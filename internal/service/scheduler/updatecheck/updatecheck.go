// Package updatecheck runs internal/service/scheduler's update check
// (issue #65's GitHub Releases self-update) so that a failed check, or a
// newer release held back by the safety gate, is retried with backoff
// instead of waiting for the next @every-6h cron tick (issue #240).
//
// It lives beside the scheduler rather than in it so the retry policy stays
// testable without a database and the scheduler directory stays within
// linterly's line budget (issue #134).
package updatecheck

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/safego"
)

// Default retry delays: the delay doubles from DefaultInitial up to
// DefaultMax, so a transient failure (network not up yet right after an
// auto-launch, GitHub API rate limit) is retried within a minute while a
// persistent one (a broken release, a long-held position) costs at most
// one request per hour.
const (
	DefaultInitial = 1 * time.Minute
	DefaultMax     = 1 * time.Hour
)

// Checker is scheduler.UpdateChecker (internal/service/updater's periodic
// entrypoint), redeclared here to keep this package free of a scheduler
// import.
type Checker interface {
	CheckForUpdate(ctx context.Context) error
}

// PendingReporter is optionally implemented by a Checker whose last
// successful CheckForUpdate found a newer release it could not install yet
// (internal/service/updater's safety gate holding it back). That outcome
// returns a nil error, so without this the Runner could not tell it from
// "already up to date".
type PendingReporter interface {
	UpdatePending() bool
}

// permanentError is optionally implemented (found with errors.As) by a
// check failure that retrying cannot fix, such as a broken release or a
// checksum mismatch (internal/service/updater's kinded errors). Retrying
// those with backoff would only repeat the request, and for a rejected
// asset re-download the whole installer, every hour (issue #259); the next
// @every-6h cron tick still looks again in case a fixed release was
// published.
type permanentError interface {
	Permanent() bool
}

// Runner runs a Checker once and keeps retrying with exponential backoff
// while the check fails with a transient error or its result is held back.
type Runner struct {
	checker Checker
	initial time.Duration
	max     time.Duration
	running atomic.Bool
}

// New returns a Runner; a non-positive initial or max falls back to
// DefaultInitial / DefaultMax.
func New(checker Checker, initial, maxDelay time.Duration) *Runner {
	if initial <= 0 {
		initial = DefaultInitial
	}
	if maxDelay <= 0 {
		maxDelay = DefaultMax
	}
	return &Runner{checker: checker, initial: initial, max: maxDelay}
}

// Start runs the check (and its retries) on a goroutine tracked on wg, so
// the caller's wg.Wait blocks until it has returned instead of reporting
// done while an installer download/verification is still in flight. It does
// nothing while an earlier run is still retrying: that run already covers
// this trigger.
func (r *Runner) Start(ctx context.Context, wg *sync.WaitGroup) {
	if !r.running.CompareAndSwap(false, true) {
		return
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer r.running.Store(false)
		defer safego.Recover("update check")
		r.run(ctx)
	}()
}

func (r *Runner) run(ctx context.Context) {
	delay := r.initial
	for r.needsRetry(ctx) {
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		delay = min(delay*2, r.max)
	}
}

// needsRetry runs one check and reports whether it should be retried soon.
func (r *Runner) needsRetry(ctx context.Context) bool {
	if err := r.checker.CheckForUpdate(ctx); err != nil {
		if ctx.Err() != nil {
			return false
		}
		var perm permanentError
		if errors.As(err, &perm) && perm.Permanent() {
			slog.Error("scheduler: update check failed, not retrying until the next scheduled check", "error", err)
			return false
		}
		slog.Error("scheduler: update check failed, retrying with backoff", "error", err)
		return true
	}
	if p, ok := r.checker.(PendingReporter); ok && p.UpdatePending() {
		slog.Info("scheduler: newer release is held back, retrying with backoff")
		return true
	}
	return false
}
