package scheduler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository"
)

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
		if !errors.Is(err, repository.ErrJobNotFound) {
			slog.Error("scheduler: claim job failed", "queue", queue, "error", err)
		}
		return false
	}

	if err := safeHandle(ctx, handler, job); err != nil {
		if markErr := s.jobs.MarkFailed(ctx, job.ID, time.Now().UTC(), err.Error()); markErr != nil {
			slog.Error("scheduler: mark job failed", "job_id", job.ID, "error", markErr)
		}
		return true
	}

	if err := s.jobs.MarkSucceeded(ctx, job.ID, time.Now().UTC()); err != nil {
		slog.Error("scheduler: mark job succeeded", "job_id", job.ID, "error", err)
	}
	return true
}

// safeHandle runs handler and converts a panic into an error (logging the
// stack), so one bad job is marked failed instead of crashing the whole
// process - and, since a crash would leave the job status='running' for
// Recover to reset, instead of crash-looping on restart.
func safeHandle(ctx context.Context, handler Handler, job repository.Job) (err error) {
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
