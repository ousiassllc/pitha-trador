package jobqueue

import (
	"context"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

// JobQueueCount is one queue's aggregate row counts, for
// internal/service/activityfeed (functional.md §4.15 FR-ACT-1).
type JobQueueCount struct {
	Queue   string
	Pending int
	Running int
	// FailedSince counts failed jobs whose finished_at is at or after the
	// failedSince argument of QueueCounts.
	FailedSince int
}

// QueueCounts returns one row per queue that has at least one pending,
// running, or failed-since-failedSince job. Queues with no such jobs are
// absent from the result.
func (r *JobRepository) QueueCounts(ctx context.Context, failedSince time.Time) ([]JobQueueCount, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT queue,
			COALESCE(SUM(status = ?), 0),
			COALESCE(SUM(status = ?), 0),
			COALESCE(SUM(status = ? AND finished_at >= ?), 0)
		 FROM jobs GROUP BY queue`,
		JobStatusPending, JobStatusRunning, JobStatusFailed, sqlutil.FormatTime(failedSince),
	)
	if err != nil {
		return nil, fmt.Errorf("repository: count jobs per queue: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []JobQueueCount
	for rows.Next() {
		var c JobQueueCount
		if err := rows.Scan(&c.Queue, &c.Pending, &c.Running, &c.FailedSince); err != nil {
			return nil, fmt.Errorf("repository: scan job queue count: %w", err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repository: count jobs per queue: %w", err)
	}
	return out, nil
}

// ListRecent returns up to limit jobs, most recently active first (by the
// latest of finished_at, started_at, created_at that is set), optionally
// restricted to queue ("" = every queue).
func (r *JobRepository) ListRecent(ctx context.Context, queue string, limit int) ([]Job, error) {
	rows, err := r.db.QueryContext(ctx,
		jobSelectColumns+` FROM jobs WHERE (? = '' OR queue = ?)
		 ORDER BY COALESCE(finished_at, started_at, created_at) DESC, id DESC
		 LIMIT ?`,
		queue, queue, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("repository: list recent jobs (queue=%q): %w", queue, err)
	}
	defer func() { _ = rows.Close() }()

	var out []Job
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, job)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repository: list recent jobs (queue=%q): %w", queue, err)
	}
	return out, nil
}

// ListOpenOrFinishedSince returns the jobs on queue that are still pending
// or running, plus those that finished (succeeded or failed) at or after
// finishedSince - the working set a producer needs to avoid enqueueing a
// duplicate of an unprocessed job or re-enqueueing too soon after a
// completed one (issue #388).
func (r *JobRepository) ListOpenOrFinishedSince(ctx context.Context, queue string, finishedSince time.Time) ([]Job, error) {
	rows, err := r.db.QueryContext(ctx,
		jobSelectColumns+` FROM jobs WHERE queue = ?
		 AND (status IN (?, ?) OR finished_at >= ?)
		 ORDER BY id ASC`,
		queue, JobStatusPending, JobStatusRunning, sqlutil.FormatTime(finishedSince),
	)
	if err != nil {
		return nil, fmt.Errorf("repository: list open or recently finished jobs (queue=%q): %w", queue, err)
	}
	defer func() { _ = rows.Close() }()

	var out []Job
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, job)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repository: list open or recently finished jobs (queue=%q): %w", queue, err)
	}
	return out, nil
}
