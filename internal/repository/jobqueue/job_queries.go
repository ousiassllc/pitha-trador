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

// queueCountsQuery counts per queue (one VALUES placeholder per JobQueue*
// constant) with three correlated subqueries, each a range lookup on the
// (queue, status, ...) indexes (jobs_queue_status_scheduled_idx,
// jobs_queue_status_finished_idx from migration 000019). Aggregating over
// jobs directly would read every retained finished row (issue #392).
const queueCountsQuery = `WITH queues(queue) AS (VALUES (?), (?), (?), (?), (?), (?))
SELECT queue,
	(SELECT COUNT(*) FROM jobs j WHERE j.queue = queues.queue AND j.status = ?),
	(SELECT COUNT(*) FROM jobs j WHERE j.queue = queues.queue AND j.status = ?),
	(SELECT COUNT(*) FROM jobs j WHERE j.queue = queues.queue AND j.status = ? AND j.finished_at >= ?)
FROM queues`

// QueueCounts returns one row per queue (JobQueue* constants) that has at
// least one pending, running, or failed-since-failedSince job. Queues with
// no such jobs are absent from the result.
func (r *JobRepository) QueueCounts(ctx context.Context, failedSince time.Time) ([]JobQueueCount, error) {
	rows, err := r.db.QueryContext(ctx, queueCountsQuery,
		JobQueueMarketData, JobQueueFeatureCalc, JobQueueJevScout, JobQueueJevTrader, JobQueueOutcomeLabeling, JobQueueAnalytics,
		JobStatusPending, JobStatusRunning, JobStatusFailed, sqlutil.FormatTime(failedSince))
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
		if c.Pending+c.Running+c.FailedSince > 0 {
			out = append(out, c)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repository: count jobs per queue: %w", err)
	}
	return out, nil
}

// countOpenQuery is an index-only count over the (queue, status, ...) indexes.
const countOpenQuery = `SELECT COUNT(*) FROM jobs WHERE queue = ? AND status IN (?, ?)`

// CountOpen returns how many jobs on queue are still pending or running.
// It touches only those rows, never the retained finished ones, so a
// producer can call it every cycle (issue #394).
func (r *JobRepository) CountOpen(ctx context.Context, queue string) (int, error) {
	var n int
	if err := r.db.QueryRowContext(ctx, countOpenQuery, queue, JobStatusPending, JobStatusRunning).Scan(&n); err != nil {
		return 0, fmt.Errorf("repository: count open jobs (queue=%q): %w", queue, err)
	}
	return n, nil
}

// listOpenOrFinishedSinceQuery is two index range searches (open rows via
// jobs_queue_status_scheduled_idx, recently finished ones via
// jobs_queue_status_finished_idx) rather than one "status IN (...) OR
// finished_at >= ?" predicate, which SQLite answers by reading every row of
// the queue (issue #395).
const listOpenOrFinishedSinceQuery = jobSelectColumns + ` FROM jobs WHERE queue = ? AND status IN (?, ?)
UNION ALL` + jobSelectColumns + ` FROM jobs WHERE queue = ? AND status IN (?, ?) AND finished_at >= ?
ORDER BY id ASC`

// ListOpenOrFinishedSince returns the jobs on queue that are still pending
// or running, plus those that finished (succeeded or failed) at or after
// finishedSince - the working set a producer needs to avoid enqueueing a
// duplicate of an unprocessed job or re-enqueueing too soon after a
// completed one (issue #388).
func (r *JobRepository) ListOpenOrFinishedSince(ctx context.Context, queue string, finishedSince time.Time) ([]Job, error) {
	rows, err := r.db.QueryContext(ctx, listOpenOrFinishedSinceQuery,
		queue, JobStatusPending, JobStatusRunning,
		queue, JobStatusSucceeded, JobStatusFailed, sqlutil.FormatTime(finishedSince),
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
