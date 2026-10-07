package scheduler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/jobqueue"
	"github.com/ousiassllc/pitha-trador/internal/textutil"
)

// maxLastErrorBytes bounds jobs.last_error: a handler error can embed an
// external API's response body, and the column is read back by the
// Activity/audit screens.
const maxLastErrorBytes = 1024

// recordTimeout bounds the completion write made after a handler returns.
const recordTimeout = 5 * time.Second

func (s *Scheduler) runWorker(ctx context.Context, queue string) {
	defer s.wg.Done()

	s.mu.Lock()
	handler := s.handlers[queue]
	s.mu.Unlock()

	polls := s.pollSignal
	if polls == nil {
		t := time.NewTicker(s.pollInterval)
		defer t.Stop()
		polls = t.C
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-polls:
			// Drain the backlog: waiting for the next tick between jobs
			// would cap throughput at 1 job per pollInterval per queue.
			for ctx.Err() == nil && s.processNext(ctx, queue, handler) {
			}
		}
	}
}

// processNext claims and runs one due job on queue. It reports whether a
// job was processed, so that runWorker can keep draining a backlog; a
// failed claim (including nothing due) reports false so a broken
// database cannot spin the worker.
func (s *Scheduler) processNext(ctx context.Context, queue string, handler Handler) bool {
	job, err := s.jobs.ClaimNext(ctx, queue, time.Now().UTC())
	if err != nil {
		if !errors.Is(err, jobqueue.ErrJobNotFound) {
			slog.Error("scheduler: claim job failed", "queue", queue, "error", err)
		}
		return false
	}

	if err := safeHandle(ctx, handler, job); err != nil {
		if ctx.Err() != nil && errors.Is(err, context.Canceled) {
			// Interrupted by shutdown: leave the job status='running' so
			// Recover re-queues it on the next start.
			slog.Info("scheduler: job interrupted by shutdown", "queue", queue, "job_id", job.ID)
			return true
		}
		var deferred *jobqueue.DeferredError
		var skipped *jobqueue.SkippedError
		switch {
		case errors.As(err, &deferred):
			slog.Info("scheduler: job deferred (not failed), will retry",
				"queue", queue, "job_id", job.ID, "retry_at", deferred.RetryAt, "reason", deferred.Reason)
			s.record(ctx, "reschedule deferred job", job.ID, func(c context.Context) error {
				return s.jobs.Reschedule(c, job.ID, deferred.RetryAt.UTC(), textutil.Truncate(deferred.Reason, maxLastErrorBytes))
			})
			return true
		case errors.As(err, &skipped):
			slog.Info("scheduler: job skipped (not failed), permanently nothing to do",
				"queue", queue, "job_id", job.ID, "reason", skipped.Reason)
			s.record(ctx, "mark job skipped", job.ID, func(c context.Context) error {
				return s.jobs.MarkSkipped(c, job.ID, time.Now().UTC(), textutil.Truncate(skipped.Reason, maxLastErrorBytes))
			})
			return true
		}
		s.record(ctx, "mark job failed", job.ID, func(c context.Context) error {
			return s.jobs.MarkFailed(c, job.ID, time.Now().UTC(), textutil.Truncate(err.Error(), maxLastErrorBytes))
		})
		return true
	}

	s.record(ctx, "mark job succeeded", job.ID, func(c context.Context) error {
		return s.jobs.MarkSucceeded(c, job.ID, time.Now().UTC())
	})
	return true
}

// record persists a job's final status. It detaches from ctx's
// cancellation (bounded by recordTimeout) so that a handler that already
// finished is recorded even when shutdown cancelled ctx in the meantime;
// otherwise the job would stay 'running' and Recover would re-run it.
func (s *Scheduler) record(ctx context.Context, what string, jobID int64, fn func(context.Context) error) {
	rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), recordTimeout)
	defer cancel()
	if err := fn(rctx); err != nil {
		slog.Error("scheduler: "+what, "job_id", jobID, "error", err)
	}
}

// safeHandle runs handler and converts a panic into an error (logging the
// stack), so one bad job is marked failed instead of crashing the whole
// process - and, since a crash would leave the job status='running' for
// Recover to reset, instead of crash-looping on restart.
func safeHandle(ctx context.Context, handler Handler, job jobqueue.Job) (err error) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("scheduler: job handler panicked",
				"queue", job.Queue, "job_id", job.ID, "panic", r, "stack", string(debug.Stack()))
			err = fmt.Errorf("handler panic: %v", r)
		}
	}()
	return handler(ctx, job)
}

// cronSlogLogger adapts robfig/cron's Logger to slog, for cron.Recover.
type cronSlogLogger struct{}

func (cronSlogLogger) Info(msg string, keysAndValues ...any) {
	slog.Info("scheduler: cron: "+msg, keysAndValues...)
}

func (cronSlogLogger) Error(err error, msg string, keysAndValues ...any) {
	slog.Error("scheduler: cron: "+msg, append([]any{"error", err}, keysAndValues...)...)
}
