// Package retention purges expired rows from the high-frequency tables
// that would otherwise grow without bound (docs/requirements/non-functional.md
// §3 "データ保持", docs/architecture/er.md §jobs / §market_snapshots):
//
//	jobs             succeeded rows after 7 days, failed rows after 30 days
//	market_snapshots rows (and their market_snapshot_vectors entry) after 90 days
//
// Only finished jobs (status succeeded/failed) are ever deleted; pending and
// running rows are live queue state. Audit tables (kill_switch_events,
// kill_switch_resolutions, jev_decisions, orders, ...) are deliberately not
// touched: kill_switch_* are append-only by trigger (#102) and the rest are
// the audit trail (FR-ACT).
//
// Rows are deleted in bounded batches so one pass never holds the SQLite
// write lock for long while the workers are inserting. Deleted pages are
// reused by later inserts, so the database file stops growing but does not
// shrink; the daily backup (VACUUM INTO) is compact.
//
// internal/service/scheduler.Scheduler runs Purge once a day through
// scheduler.WithDataPurger.
package retention

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/jobqueue"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

// Default retention windows, in days.
const (
	DefaultSucceededJobDays = 7
	DefaultFailedJobDays    = 30
	DefaultSnapshotDays     = 90
)

// defaultBatchSize bounds how many rows one DELETE statement/transaction
// removes.
const defaultBatchSize = 1000

// Policy is the retention window per table, in days. A zero or negative
// field falls back to its Default* value.
type Policy struct {
	SucceededJobDays int
	FailedJobDays    int
	SnapshotDays     int
}

// Service purges expired rows from one database.
type Service struct {
	db        *sql.DB
	policy    Policy
	batchSize int
	now       func() time.Time
}

// New returns a Service purging db according to policy.
func New(db *sql.DB, policy Policy) *Service {
	if policy.SucceededJobDays <= 0 {
		policy.SucceededJobDays = DefaultSucceededJobDays
	}
	if policy.FailedJobDays <= 0 {
		policy.FailedJobDays = DefaultFailedJobDays
	}
	if policy.SnapshotDays <= 0 {
		policy.SnapshotDays = DefaultSnapshotDays
	}
	return &Service{db: db, policy: policy, batchSize: defaultBatchSize, now: time.Now}
}

// Purge runs one pass over every table. A failure on one table does not
// stop the others; all failures are returned joined.
func (s *Service) Purge(ctx context.Context) error {
	now := s.now().UTC()
	cutoff := func(days int) string {
		return sqlutil.FormatTime(now.AddDate(0, 0, -days))
	}

	var errs []error
	for _, step := range []struct {
		name string
		run  func() (int64, error)
	}{
		{"succeeded jobs", func() (int64, error) {
			return s.purgeJobs(ctx, jobqueue.JobStatusSucceeded, cutoff(s.policy.SucceededJobDays))
		}},
		{"failed jobs", func() (int64, error) {
			return s.purgeJobs(ctx, jobqueue.JobStatusFailed, cutoff(s.policy.FailedJobDays))
		}},
		{"market snapshots", func() (int64, error) {
			return s.purgeSnapshots(ctx, cutoff(s.policy.SnapshotDays))
		}},
	} {
		n, err := step.run()
		if err != nil {
			errs = append(errs, fmt.Errorf("retention: purge %s: %w", step.name, err))
		}
		slog.Info("retention: purged", "table", step.name, "deleted", n)
	}
	return errors.Join(errs...)
}

// purgeJobs deletes jobs with the given terminal status finished before
// cutoff (an RFC3339 UTC string, as stored), returning how many were
// deleted.
func (s *Service) purgeJobs(ctx context.Context, status, cutoff string) (int64, error) {
	var total int64
	for {
		res, err := s.db.ExecContext(ctx,
			`DELETE FROM jobs WHERE id IN (
				SELECT id FROM jobs WHERE status = ? AND finished_at < ? LIMIT ?)`,
			status, cutoff, s.batchSize)
		if err != nil {
			return total, err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return total, err
		}
		total += n
		if n < int64(s.batchSize) {
			return total, nil
		}
	}
}

// purgeSnapshots deletes market_snapshots timestamped before cutoff along
// with their market_snapshot_vectors rows (a vec0 virtual table has no
// foreign key, so the application keeps the two in step, er.md
// §ベクトルインデックス), returning how many snapshots were deleted.
func (s *Service) purgeSnapshots(ctx context.Context, cutoff string) (int64, error) {
	var total int64
	for {
		n, err := s.purgeSnapshotBatch(ctx, cutoff)
		total += n
		if err != nil || n < int64(s.batchSize) {
			return total, err
		}
	}
}

func (s *Service) purgeSnapshotBatch(ctx context.Context, cutoff string) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	rows, err := tx.QueryContext(ctx,
		`SELECT id FROM market_snapshots WHERE timestamp < ? ORDER BY id LIMIT ?`, cutoff, s.batchSize)
	if err != nil {
		return 0, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}

	for _, id := range ids {
		if _, err := tx.ExecContext(ctx, `DELETE FROM market_snapshot_vectors WHERE snapshot_id = ?`, id); err != nil {
			return 0, err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM market_snapshots WHERE id = ?`, id); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return int64(len(ids)), nil
}
