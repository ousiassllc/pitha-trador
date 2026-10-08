package jobqueue

import (
	"context"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

// Notes recorded in jobs.last_error by a non-failing handler outcome.
// last_error is only an operator-facing note on these rows: their status is
// pending (deferred) or succeeded (skipped), never failed, so they do not
// count toward FailedRecent (issue #710).
const (
	deferredNotePrefix = "deferred: "
	skippedNotePrefix  = "skipped: "
)

// DeferredError is returned by a job handler that cannot finish yet but
// expects to later (e.g. market data still landing): the scheduler puts the
// job back to pending at RetryAt instead of marking it failed (issue #710).
type DeferredError struct {
	RetryAt time.Time
	Reason  string
}

func (e *DeferredError) Error() string {
	return fmt.Sprintf("job deferred until %s: %s", e.RetryAt.Format(time.RFC3339), e.Reason)
}

// Defer builds the DeferredError a handler returns to be retried at retryAt.
func Defer(retryAt time.Time, reason string) error {
	return &DeferredError{RetryAt: retryAt, Reason: reason}
}

// SkippedError is returned by a job handler that decided its work can never
// be done and has recorded that terminally itself (e.g. an
// unlabelable-horizon marker): the scheduler ends the job as succeeded with
// the reason noted, instead of failed (issue #710).
type SkippedError struct {
	Reason string
}

func (e *SkippedError) Error() string { return "job skipped: " + e.Reason }

// Skip builds the SkippedError a handler returns after terminally skipping.
func Skip(reason string) error {
	return &SkippedError{Reason: reason}
}

// Reschedule puts a running job back to pending, due at scheduledAt, and
// records reason in last_error as a "deferred:" note (attempts is kept, so
// retries stay visible). It returns ErrJobNotFound if the job is not
// running. The observer is notified of the new state.
func (r *JobRepository) Reschedule(ctx context.Context, id int64, scheduledAt time.Time, reason string) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE jobs SET status = ?, scheduled_at = ?, started_at = NULL, finished_at = NULL, last_error = ?
		 WHERE id = ? AND status = ?`,
		JobStatusPending, sqlutil.FormatTime(scheduledAt), deferredNotePrefix+reason, id, JobStatusRunning,
	)
	if err != nil {
		return fmt.Errorf("repository: reschedule job %d: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("repository: reschedule job %d: %w", id, err)
	}
	if n == 0 {
		return ErrJobNotFound
	}
	if r.observer != nil {
		if job, err := r.Get(ctx, id); err == nil {
			r.notify(ctx, job)
		}
	}
	return nil
}

// MarkSkipped ends the job as succeeded (not failed) with a "skipped:" note
// in last_error, so operators can tell a terminal skip from a plain success.
// It returns ErrJobNotFound if no row with that ID exists.
func (r *JobRepository) MarkSkipped(ctx context.Context, id int64, finishedAt time.Time, reason string) error {
	note := skippedNotePrefix + reason
	return r.setFinished(ctx, id, JobStatusSucceeded, finishedAt, &note)
}
