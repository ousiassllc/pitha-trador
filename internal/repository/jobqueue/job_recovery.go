package jobqueue

import (
	"context"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

// ResetStuckRunning resets every job still marked running back to pending
// (docs/architecture/er.md §jobs "再起動時の回復": プロセス起動時に
// status='running'のまま残っている行（クラッシュで中断されたジョブ）を
// pendingへ戻し再実行する). It returns the number of rows reset.
func (r *JobRepository) ResetStuckRunning(ctx context.Context) (int64, error) {
	res, err := r.db.ExecContext(ctx,
		`UPDATE jobs SET status = ?, started_at = NULL WHERE status = ?`,
		JobStatusPending, JobStatusRunning,
	)
	if err != nil {
		return 0, fmt.Errorf("repository: reset stuck running jobs: %w", err)
	}
	return res.RowsAffected()
}

// FailOrphanedRunning marks every job on queue that has been running since
// before startedBefore as failed (finished at finishedAt, last_error =
// reason), and returns the jobs it closed. Such rows are orphans: a worker
// whose completion write failed (database is locked, disk error, ...)
// leaves its job status='running' and ResetStuckRunning only runs at
// startup, so without this a producer that waits for the queue to drain
// would wait forever (issue #416). Failing - not re-queueing - them avoids
// running the same job twice if its handler is merely slow; the next full
// scan re-enqueues the instrument anyway. The observer is notified of each
// closed job.
func (r *JobRepository) FailOrphanedRunning(ctx context.Context, queue string, startedBefore, finishedAt time.Time, reason string) ([]Job, error) {
	rows, err := r.db.QueryContext(ctx,
		`UPDATE jobs SET status = ?, finished_at = ?, last_error = ?
		 WHERE queue = ? AND status = ? AND started_at < ?
		 RETURNING id, queue, payload_json, status, attempts, scheduled_at, started_at, finished_at, last_error, created_at`,
		JobStatusFailed, sqlutil.FormatTime(finishedAt), reason,
		queue, JobStatusRunning, sqlutil.FormatTime(startedBefore),
	)
	if err != nil {
		return nil, fmt.Errorf("repository: fail orphaned running jobs (queue=%q): %w", queue, err)
	}
	defer func() { _ = rows.Close() }()

	var jobs []Job
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repository: fail orphaned running jobs (queue=%q): %w", queue, err)
	}
	// Close before notifying: the observer may query the database.
	_ = rows.Close()
	for _, job := range jobs {
		r.notify(ctx, job)
	}
	return jobs, nil
}
