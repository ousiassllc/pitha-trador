package jobqueue

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"time"
)

// allQueues is every queue ListRecent covers when no queue is given.
var allQueues = []string{
	JobQueueMarketData, JobQueueFeatureCalc, JobQueueJevScout,
	JobQueueJevTrader, JobQueueOutcomeLabeling, JobQueueAnalytics,
}

// AllQueues returns every queue name (the JobQueue* constants).
func AllQueues() []string { return slices.Clone(allQueues) }

// listRecentSQL builds ListRecent's query for queues: per queue, one arm for
// the open rows (jobs_queue_status_scheduled_idx; pending/running rows are
// few) and one limit-bounded arm per finished status, read newest-first
// straight off jobs_queue_status_finished_idx (the equality on queue and
// status, then finished_at DESC, id DESC, is that index's backward scan).
// The arms are merged in the caller. Ordering everything in SQL by
// COALESCE(finished_at, started_at, created_at) instead cannot use any
// index and reads, then sorts, every retained row (issue #419).
func listRecentSQL(queues []string, limit int) (string, []any) {
	var (
		arms []string
		args []any
	)
	for _, queue := range queues {
		arms = append(arms, jobSelectColumns+` FROM jobs WHERE queue = ? AND status IN (?, ?)`)
		args = append(args, queue, JobStatusPending, JobStatusRunning)
		for _, status := range []string{JobStatusSucceeded, JobStatusFailed} {
			arms = append(arms, `SELECT * FROM (`+jobSelectColumns+
				` FROM jobs WHERE queue = ? AND status = ? ORDER BY finished_at DESC, id DESC LIMIT ?)`)
			args = append(args, queue, status, limit)
		}
	}
	return strings.Join(arms, "\nUNION ALL "), args
}

// lastActivity is the instant ListRecent orders a job by: the latest
// lifecycle timestamp that is set.
func lastActivity(j Job) time.Time {
	switch {
	case j.FinishedAt != nil:
		return *j.FinishedAt
	case j.StartedAt != nil:
		return *j.StartedAt
	default:
		return j.CreatedAt
	}
}

// ListRecent returns up to limit jobs, most recently active first (by the
// latest of finished_at, started_at, created_at that is set, then id
// descending), optionally restricted to queue ("" = every queue). Its cost
// is bounded by limit and the number of open rows, not by how many finished
// rows are retained (issue #419).
func (r *JobRepository) ListRecent(ctx context.Context, queue string, limit int) ([]Job, error) {
	if limit <= 0 {
		return nil, nil
	}
	queues := allQueues
	if queue != "" {
		queues = []string{queue}
	}
	query, args := listRecentSQL(queues, limit)
	rows, err := r.db.QueryContext(ctx, query, args...)
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

	slices.SortFunc(out, func(a, b Job) int {
		if c := lastActivity(b).Compare(lastActivity(a)); c != 0 {
			return c
		}
		return cmp.Compare(b.ID, a.ID)
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
