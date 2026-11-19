// Package researchquotejob is the composition root's wiring of the retention of
// the research 時価 samples (issue #1083): the daily maintenance task that, once
// rows are older than the retention (Research.QuotesRetentionDays, default 90),
// archives and verifies those days to the local archive folder and only then
// deletes them (the #1072 archive-before-delete of market_snapshots, for
// research_quote_samples; see internal/service/snapshotarchive). Nothing it
// writes is a trading input.
//
// The task works whatever the recording switch says (rows recorded earlier
// still age out), but a Run returns maintenance.ErrSkipped before it writes
// anything when the table is empty, so a default install gains no write from
// it. It fails closed: when the archive or its verification fails, nothing is
// deleted. A Run deletes at most MaxBatchesPerRun batches (with a pause between
// them so a writer can take the lock) and returns maintenance.ErrMore while rows
// are left; the runner frees its lock between the calls.
package researchquotejob

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/researchquote"
	"github.com/ousiassllc/pitha-trador/internal/service/opsettings"
	"github.com/ousiassllc/pitha-trador/internal/service/scheduler/maintenance"
	"github.com/ousiassllc/pitha-trador/internal/service/snapshotarchive"
)

// TaskName is the maintenance task's name (its last-success date is stored
// under it), Label what the notification calls it.
const (
	TaskName = "research_quote_retention"
	Label    = "研究用の時価の保持期間の整理"
)

const (
	// BatchSize is how many rows one DELETE removes.
	BatchSize = 1000
	// MaxBatchesPerRun bounds one Run (50,000 rows, about one recorded day).
	MaxBatchesPerRun = 50
	// BatchPause is the wait between two batches (the retention purge's).
	BatchPause = 200 * time.Millisecond
)

// jst is the zone of the retention's days.
var jst = time.FixedZone("JST", 9*60*60)

// Archiver archives and verifies the days before a cutoff
// (*snapshotarchive.Archiver of research_quote_samples).
type Archiver interface {
	ArchiveBefore(ctx context.Context, cutoff time.Time) (snapshotarchive.Result, error)
}

// Store reads and deletes the samples (*researchquote.Repository).
type Store interface {
	Any(ctx context.Context) (bool, error)
	DeleteBefore(ctx context.Context, before string, maxID int64, limit int) (int64, error)
}

// Job ages out the research 時価 samples.
type Job struct {
	settings  opsettings.Store
	archiver  Archiver
	store     Store
	now       func() time.Time
	batchSize int
	maxBatch  int
	pause     time.Duration
	sleep     func(ctx context.Context, d time.Duration) error
}

// New returns the Job of db, archiving to snapshotarchive.DefaultDir(dbPath) (a
// folder on the same PC, never sent anywhere) and reading the retention from
// settings at every run.
func New(db *sql.DB, settings opsettings.Store, dbPath string) *Job {
	return newJob(settings, snapshotarchive.NewResearchQuotes(db, snapshotarchive.DefaultDir(dbPath)), researchquote.New(db))
}

func newJob(settings opsettings.Store, archiver Archiver, store Store) *Job {
	return &Job{
		settings: settings, archiver: archiver, store: store, now: time.Now,
		batchSize: BatchSize, maxBatch: MaxBatchesPerRun, pause: BatchPause, sleep: sleepContext,
	}
}

// Task returns the maintenance task for scheduler.WithMaintenanceTask.
func (j *Job) Task() maintenance.Task {
	return maintenance.Task{Name: TaskName, Label: Label, Run: j.Run}
}

// Run archives the days older than the retention and deletes their rows. It
// returns maintenance.ErrSkipped when the table is empty (nothing is written)
// and maintenance.ErrMore when the Run's batch limit left rows behind. The
// samples are by-product research data, so a panic here is returned as an error
// instead of crashing the process that also trades.
func (j *Job) Run(ctx context.Context) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("researchquotejob: panic while aging out research quotes: %v", p)
		}
	}()
	hasRows, err := j.store.Any(ctx)
	if err != nil {
		return fmt.Errorf("researchquotejob: %w", err)
	}
	if !hasRows {
		return maintenance.ErrSkipped
	}
	set, err := opsettings.LoadTachibanaSource(ctx, j.settings)
	if err != nil {
		return fmt.Errorf("researchquotejob: %w", err)
	}
	cutoff := j.cutoff(set.Research.QuotesRetentionDays)
	res, err := j.archiver.ArchiveBefore(ctx, cutoff)
	if err != nil {
		slog.ErrorContext(ctx, "researchquotejob: archive failed; nothing deleted from research_quote_samples", "error", err)
		return fmt.Errorf("researchquotejob: archive before delete (research_quote_samples left undeleted): %w", err)
	}
	deleted, more, err := j.purge(ctx, res)
	if err != nil {
		return fmt.Errorf("researchquotejob: %w", err)
	}
	slog.InfoContext(ctx, "researchquotejob: expired research quotes archived and deleted",
		"archived_days", len(res.Days), "deleted_rows", deleted, "delete_before", res.DeleteBefore, "more", more)
	if more {
		return maintenance.ErrMore
	}
	return nil
}

// cutoff is the first day kept, as the UTC midnight of its date: the samples of
// a trading day are stamped within 00:00-06:30 UTC, the same date as its JST
// date, so the JST date of (now - retention days) is also the UTC date to
// archive up to (exclusive).
func (j *Job) cutoff(retentionDays int) time.Time {
	y, m, d := j.now().In(jst).AddDate(0, 0, -retentionDays).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// purge deletes the verified rows (the dates before res.DeleteBefore, ids up to
// res.MaxID) in batches with a pause between; more is true when it stopped at
// the Run's batch limit with rows left.
func (j *Job) purge(ctx context.Context, res snapshotarchive.Result) (deleted int64, more bool, err error) {
	for batch := 0; ; batch++ {
		if batch == j.maxBatch {
			return deleted, true, nil
		}
		n, err := j.store.DeleteBefore(ctx, res.DeleteBefore, res.MaxID, j.batchSize)
		deleted += n
		if err != nil {
			return deleted, false, err
		}
		if n < int64(j.batchSize) {
			return deleted, false, nil
		}
		if err := j.sleep(ctx, j.pause); err != nil {
			return deleted, false, err
		}
	}
}

func sleepContext(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
