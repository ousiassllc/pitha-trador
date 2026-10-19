package jobqueue

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

// FailedJob is the part of a failed jobs row the failure log needs: which
// queue failed, when, and why. It carries neither the payload (it holds
// instrument codes and prices) nor the attempts.
type FailedJob struct {
	ID         int64
	Queue      string
	FinishedAt time.Time
	// LastError is jobs.last_error, "" when the row has none.
	LastError string
}

// FailedCursor is a position in the (finished_at, id) order ListFailedAfter
// pages through: the rows strictly after it are returned.
type FailedCursor struct {
	FinishedAt time.Time
	ID         int64
}

// FailedCursorAtTime is the cursor that precedes no row finished after t
// and follows every row finished at or before t.
func FailedCursorAtTime(t time.Time) FailedCursor {
	return FailedCursor{FinishedAt: t, ID: math.MaxInt64}
}

// Cursor returns the position of j, for paging on from it.
func (j FailedJob) Cursor() FailedCursor {
	return FailedCursor{FinishedAt: j.FinishedAt, ID: j.ID}
}

// listFailedAfterQuery is one queue's range search on
// jobs_queue_status_finished_idx (migration 000019), bounded on both sides by
// finished_at; the trailing (finished_at > ? OR id > ?) only trims the first
// finished_at of the range.
const listFailedAfterQuery = `SELECT id, queue, finished_at, COALESCE(last_error, '') FROM jobs
WHERE queue = ? AND status = ? AND finished_at >= ? AND finished_at <= ? AND (finished_at > ? OR id > ?)
ORDER BY finished_at ASC, id ASC LIMIT ?`

// ListFailedAfter returns up to limit failed jobs of all queues that
// finished strictly after cursor and at or before upTo, oldest first
// ((finished_at, id) order). Passing the last row's Cursor back pages through
// a window without skipping rows that share a finished_at; a result shorter
// than limit means the window is exhausted.
//
// It queries each queue on its own (so the (queue, status, finished_at)
// index applies) and merges: the first limit rows overall are within the
// first limit rows of their own queue.
func (r *JobRepository) ListFailedAfter(ctx context.Context, cursor FailedCursor, upTo time.Time, limit int) ([]FailedJob, error) {
	if limit <= 0 {
		return nil, nil
	}
	var out []FailedJob
	for _, queue := range AllQueues() {
		jobs, err := r.listFailedAfterQueue(ctx, queue, cursor, upTo, limit)
		if err != nil {
			return nil, err
		}
		out = append(out, jobs...)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].FinishedAt.Equal(out[j].FinishedAt) {
			return out[i].FinishedAt.Before(out[j].FinishedAt)
		}
		return out[i].ID < out[j].ID
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (r *JobRepository) listFailedAfterQueue(ctx context.Context, queue string, cursor FailedCursor, upTo time.Time, limit int) ([]FailedJob, error) {
	after := sqlutil.FormatTime(cursor.FinishedAt)
	rows, err := r.db.QueryContext(ctx, listFailedAfterQuery,
		queue, JobStatusFailed, after, sqlutil.FormatTime(upTo), after, cursor.ID, limit)
	if err != nil {
		return nil, fmt.Errorf("repository: list failed %s jobs: %w", queue, err)
	}
	defer func() { _ = rows.Close() }()

	var out []FailedJob
	for rows.Next() {
		var (
			j          FailedJob
			finishedAt string
		)
		if err := rows.Scan(&j.ID, &j.Queue, &finishedAt, &j.LastError); err != nil {
			return nil, fmt.Errorf("repository: scan failed job: %w", err)
		}
		if j.FinishedAt, err = sqlutil.ParseTime(finishedAt); err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repository: list failed %s jobs: %w", queue, err)
	}
	return out, nil
}
