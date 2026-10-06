package jobqueue

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

// Job queue names (docs/architecture/er.md §jobs, overview.md §2 "Job
// Queue / Scheduler"). internal/service/scheduler enqueues and drains
// jobs on these queues in this pipeline order:
// market-data → jev-scout → jev-trader (feature-calc is a compat no-op
// queue the full scan never feeds; FR-SCHED-1), with
// outcome-labeling/analytics running asynchronously afterward. Risk
// check and Paper execution are not queues: they run synchronously
// inside the jev-trader job (FR-SCHED-1).
const (
	JobQueueMarketData      = "market-data"
	JobQueueFeatureCalc     = "feature-calc"
	JobQueueJevScout        = "jev-scout"
	JobQueueJevTrader       = "jev-trader"
	JobQueueOutcomeLabeling = "outcome-labeling"
	JobQueueAnalytics       = "analytics"
)

// Job statuses (docs/architecture/er.md §jobs status CHECK constraint).
const (
	JobStatusPending   = "pending"
	JobStatusRunning   = "running"
	JobStatusSucceeded = "succeeded"
	JobStatusFailed    = "failed"
)

// ErrJobNotFound is returned by JobRepository methods when no matching
// jobs row exists, and by ClaimNext when no job is currently due.
var ErrJobNotFound = errors.New("repository: job not found")

// Job is a single row of the jobs table: the self-hosted worker queue that
// stands in for River (Postgres-only) on SQLite
// (docs/architecture/er.md §jobs).
type Job struct {
	ID          int64
	Queue       string
	PayloadJSON string
	Status      string
	Attempts    int
	ScheduledAt time.Time
	StartedAt   *time.Time
	FinishedAt  *time.Time
	LastError   *string
	CreatedAt   time.Time
}

// JobObserver is notified after every committed jobs state transition
// (Enqueue, ClaimNext, MarkSucceeded, MarkFailed) with the row's new
// state (docs/architecture/overview.md §12). It runs synchronously on
// the writer's goroutine after the write has committed and cannot fail
// the write, so it must return quickly.
type JobObserver func(ctx context.Context, job Job)

// JobRepository provides CRUD access to the jobs table for
// internal/service/scheduler's worker pool.
type JobRepository struct {
	db       *sql.DB
	observer JobObserver
}

// SetObserver registers fn to be called after every committed job state
// transition, replacing any previous observer. It is not safe to call
// concurrently with other JobRepository methods; wire it once during
// composition, before any worker starts.
func (r *JobRepository) SetObserver(fn JobObserver) { r.observer = fn }

func (r *JobRepository) notify(ctx context.Context, job Job) {
	if r.observer != nil {
		r.observer(ctx, job)
	}
}

// NewJobRepository returns a JobRepository backed by db.
func NewJobRepository(db *sql.DB) *JobRepository {
	return &JobRepository{db: db}
}

// Enqueue inserts a new pending job onto queue, due to run at scheduledAt.
func (r *JobRepository) Enqueue(ctx context.Context, queue, payloadJSON string, scheduledAt time.Time) (Job, error) {
	now := time.Now().UTC()
	res, err := r.db.ExecContext(ctx,
		`INSERT INTO jobs (queue, payload_json, status, attempts, scheduled_at, created_at)
		 VALUES (?, ?, ?, 0, ?, ?)`,
		queue, payloadJSON, JobStatusPending, sqlutil.FormatTime(scheduledAt), sqlutil.FormatTime(now),
	)
	if err != nil {
		return Job{}, fmt.Errorf("repository: enqueue job on queue %q: %w", queue, err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return Job{}, fmt.Errorf("repository: read job id for queue %q: %w", queue, err)
	}

	job := Job{
		ID:          id,
		Queue:       queue,
		PayloadJSON: payloadJSON,
		Status:      JobStatusPending,
		ScheduledAt: scheduledAt.UTC(),
		CreatedAt:   now,
	}
	r.notify(ctx, job)
	return job, nil
}

// Get returns the job with the given id, or ErrJobNotFound.
func (r *JobRepository) Get(ctx context.Context, id int64) (Job, error) {
	row := r.db.QueryRowContext(ctx, jobSelectColumns+` FROM jobs WHERE id = ?`, id)
	return scanJob(row)
}

// ClaimNext atomically selects the oldest pending job on queue whose
// scheduled_at is due (<= now), marks it running (incrementing attempts
// and setting started_at), and returns it. It returns ErrJobNotFound if no
// job on queue is currently due.
//
// It deliberately avoids a transaction: with the DSN's _txlock=immediate
// every BeginTx would take the database write lock, and the scheduler
// polls every queue every 200ms even when idle (issue #541). The oldest
// due job is found with a plain read (no write lock, so an empty queue
// costs nothing), then claimed with an UPDATE guarded by status =
// 'pending'. That UPDATE is atomic: when another worker claimed the same
// job in between, it affects no row and the next oldest candidate is
// tried, so a job is never handed out twice.
func (r *JobRepository) ClaimNext(ctx context.Context, queue string, now time.Time) (Job, error) {
	startedAt := now.UTC()
	for {
		job, err := r.oldestDue(ctx, queue, now)
		if err != nil {
			return Job{}, err
		}

		res, err := r.db.ExecContext(ctx,
			`UPDATE jobs SET status = ?, attempts = attempts + 1, started_at = ? WHERE id = ? AND status = ?`,
			JobStatusRunning, sqlutil.FormatTime(startedAt), job.ID, JobStatusPending,
		)
		if err != nil {
			return Job{}, fmt.Errorf("repository: claim job %d: %w", job.ID, err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return Job{}, fmt.Errorf("repository: claim job %d: %w", job.ID, err)
		}
		if n == 0 {
			continue // lost the race for this job; look at the next one
		}

		job.Status = JobStatusRunning
		job.Attempts++
		job.StartedAt = &startedAt
		r.notify(ctx, job)
		return job, nil
	}
}

// oldestDue returns the oldest pending job on queue due at now, or
// ErrJobNotFound.
func (r *JobRepository) oldestDue(ctx context.Context, queue string, now time.Time) (Job, error) {
	return scanJob(r.db.QueryRowContext(ctx,
		jobSelectColumns+` FROM jobs
		 WHERE queue = ? AND status = ? AND scheduled_at <= ?
		 ORDER BY scheduled_at ASC
		 LIMIT 1`,
		queue, JobStatusPending, sqlutil.FormatTime(now),
	))
}

// MarkSucceeded marks the job as succeeded with the given finish time. It
// returns ErrJobNotFound if no row with that ID exists.
func (r *JobRepository) MarkSucceeded(ctx context.Context, id int64, finishedAt time.Time) error {
	return r.setFinished(ctx, id, JobStatusSucceeded, finishedAt, nil)
}

// MarkFailed marks the job as failed, recording lastErr and the finish
// time. It returns ErrJobNotFound if no row with that ID exists.
func (r *JobRepository) MarkFailed(ctx context.Context, id int64, finishedAt time.Time, lastErr string) error {
	return r.setFinished(ctx, id, JobStatusFailed, finishedAt, &lastErr)
}

func (r *JobRepository) setFinished(ctx context.Context, id int64, status string, finishedAt time.Time, lastErr *string) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE jobs SET status = ?, finished_at = ?, last_error = ? WHERE id = ?`,
		status, sqlutil.FormatTime(finishedAt), sqlutil.NullableString(lastErr), id,
	)
	if err != nil {
		return fmt.Errorf("repository: mark job %d %s: %w", id, status, err)
	}

	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("repository: mark job %d %s: %w", id, status, err)
	}
	if n == 0 {
		return ErrJobNotFound
	}
	if r.observer != nil {
		// The observer wants the row's full new state, which the UPDATE
		// above does not return; a failed re-read only skips the
		// notification, the write itself already succeeded.
		if job, err := r.Get(ctx, id); err == nil {
			r.notify(ctx, job)
		}
	}
	return nil
}

const jobSelectColumns = `
SELECT id, queue, payload_json, status, attempts, scheduled_at, started_at, finished_at, last_error, created_at`

func scanJob(row sqlutil.RowScanner) (Job, error) {
	var (
		job         Job
		scheduledAt string
		startedAt   sql.NullString
		finishedAt  sql.NullString
		lastError   sql.NullString
		createdAt   string
	)

	err := row.Scan(
		&job.ID, &job.Queue, &job.PayloadJSON, &job.Status, &job.Attempts,
		&scheduledAt, &startedAt, &finishedAt, &lastError, &createdAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Job{}, ErrJobNotFound
	}
	if err != nil {
		return Job{}, fmt.Errorf("repository: scan job: %w", err)
	}

	job.ScheduledAt, err = sqlutil.ParseTime(scheduledAt)
	if err != nil {
		return Job{}, err
	}
	job.CreatedAt, err = sqlutil.ParseTime(createdAt)
	if err != nil {
		return Job{}, err
	}
	if job.StartedAt, err = sqlutil.ParseNullableTime(startedAt); err != nil {
		return Job{}, err
	}
	if job.FinishedAt, err = sqlutil.ParseNullableTime(finishedAt); err != nil {
		return Job{}, err
	}
	if lastError.Valid {
		job.LastError = &lastError.String
	}

	return job, nil
}
