package jobqueue

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

// EnqueueBatch inserts one pending job per payload onto queue, all due at
// scheduledAt, in a single transaction: either every job is enqueued or
// none is. A full scan over ~4,000 instruments is then one commit instead of
// thousands (non-functional.md §2.3). The observer is notified of each
// job after the commit. It returns the number of jobs enqueued.
func (r *JobRepository) EnqueueBatch(ctx context.Context, queue string, payloadsJSON []string, scheduledAt time.Time) (int, error) {
	if len(payloadsJSON) == 0 {
		return 0, nil
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("repository: begin enqueue batch for queue %q: %w", queue, err)
	}
	stmt, err := tx.PrepareContext(ctx,
		`INSERT INTO jobs (queue, payload_json, status, attempts, scheduled_at, created_at)
		 VALUES (?, ?, ?, 0, ?, ?)`)
	if err != nil {
		return 0, errors.Join(fmt.Errorf("repository: prepare enqueue batch for queue %q: %w", queue, err), tx.Rollback())
	}
	defer func() { _ = stmt.Close() }()

	now := time.Now().UTC()
	scheduled, created := sqlutil.FormatTime(scheduledAt), sqlutil.FormatTime(now)
	var jobs []Job
	if r.observer != nil {
		jobs = make([]Job, 0, len(payloadsJSON))
	}
	for _, payload := range payloadsJSON {
		res, err := stmt.ExecContext(ctx, queue, payload, JobStatusPending, scheduled, created)
		if err != nil {
			return 0, errors.Join(fmt.Errorf("repository: enqueue batch job on queue %q: %w", queue, err), tx.Rollback())
		}
		if r.observer == nil {
			continue
		}
		id, err := res.LastInsertId()
		if err != nil {
			return 0, errors.Join(fmt.Errorf("repository: read job id for queue %q: %w", queue, err), tx.Rollback())
		}
		jobs = append(jobs, Job{
			ID: id, Queue: queue, PayloadJSON: payload, Status: JobStatusPending,
			ScheduledAt: scheduledAt.UTC(), CreatedAt: now,
		})
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("repository: commit enqueue batch for queue %q: %w", queue, err)
	}
	for _, job := range jobs {
		r.notify(ctx, job)
	}
	return len(payloadsJSON), nil
}
