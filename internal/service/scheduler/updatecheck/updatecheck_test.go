package updatecheck_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/scheduler/updatecheck"
)

// scriptedChecker returns errs[i] (nil past the end) on its i-th
// CheckForUpdate call.
type scriptedChecker struct {
	mu    sync.Mutex
	errs  []error
	calls int
}

func (c *scriptedChecker) CheckForUpdate(context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	if c.calls <= len(c.errs) {
		return c.errs[c.calls-1]
	}
	return nil
}

func (c *scriptedChecker) Calls() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

// pendingChecker also reports pending[i] after its i-th call.
type pendingChecker struct {
	scriptedChecker
	pending []bool
}

func (c *pendingChecker) UpdatePending() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls >= 1 && c.calls <= len(c.pending) && c.pending[c.calls-1]
}

// runToCompletion starts the runner and waits for its goroutine to finish.
func runToCompletion(t *testing.T, checker updatecheck.Checker) {
	t.Helper()
	var wg sync.WaitGroup
	updatecheck.New(checker, time.Millisecond, 4*time.Millisecond).Start(context.Background(), &wg)
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("runner did not stop retrying")
	}
}

// TestRunner_RetriesFailedCheckUntilItSucceeds regresses issue #240: a
// startup check that failed was only retried by the next @every 6h cron
// tick.
func TestRunner_RetriesFailedCheckUntilItSucceeds(t *testing.T) {
	checker := &scriptedChecker{errs: []error{errors.New("network unreachable"), errors.New("rate limited")}}
	runToCompletion(t, checker)
	if got := checker.Calls(); got != 3 {
		t.Fatalf("calls = %d, want 3 (2 failures then 1 success)", got)
	}
}

// permanentError is an error a retry cannot fix (updater's broken release,
// rejected checksum, refused lookup).
type permanentError struct{ error }

func (permanentError) Permanent() bool { return true }

// TestRunner_DoesNotRetryPermanentFailure regresses issue #259: a broken
// release or a checksum mismatch was retried every backoff step (re-
// downloading the whole installer each time) although it cannot fix itself;
// only the next @every-6h cron tick should look again.
func TestRunner_DoesNotRetryPermanentFailure(t *testing.T) {
	wrapped := fmt.Errorf("updater: download/verify v0.2.0: %w", permanentError{errors.New("checksum mismatch")})
	checker := &scriptedChecker{errs: []error{wrapped, wrapped, wrapped}}
	runToCompletion(t, checker)
	if got := checker.Calls(); got != 1 {
		t.Fatalf("calls = %d, want 1: a permanent failure must not be retried with backoff", got)
	}
}

// TestRunner_RetriesUpdateHeldBySafetyGate regresses issue #240: a newer
// release held by the safety gate returns a nil error, so it too was only
// retried 6h later.
func TestRunner_RetriesUpdateHeldBySafetyGate(t *testing.T) {
	checker := &pendingChecker{pending: []bool{true, true, false}}
	runToCompletion(t, checker)
	if got := checker.Calls(); got != 3 {
		t.Fatalf("calls = %d, want 3 (held twice, then released)", got)
	}
}

// TestRunner_DoesNotRetryUpToDateCheck pins that only failed/held checks
// are retried.
func TestRunner_DoesNotRetryUpToDateCheck(t *testing.T) {
	checker := &pendingChecker{}
	runToCompletion(t, checker)
	if got := checker.Calls(); got != 1 {
		t.Fatalf("calls = %d, want 1", got)
	}
}

// TestRunner_StopsRetryingWhenContextCancelled pins that Stop-style
// cancellation ends the retry loop (the scheduler waits on it in Stop).
func TestRunner_StopsRetryingWhenContextCancelled(t *testing.T) {
	checker := &scriptedChecker{errs: []error{errors.New("boom"), errors.New("boom"), errors.New("boom")}}
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	updatecheck.New(checker, time.Hour, time.Hour).Start(ctx, &wg)
	for checker.Calls() == 0 {
		time.Sleep(time.Millisecond)
	}
	cancel()
	wg.Wait()
	if got := checker.Calls(); got != 1 {
		t.Fatalf("calls = %d, want 1: cancellation must interrupt the 1h backoff wait", got)
	}
}

// TestRunner_StartWhileRetryingIsSkipped pins that a cron tick arriving
// while an earlier run is still retrying does not start a second check.
func TestRunner_StartWhileRetryingIsSkipped(t *testing.T) {
	checker := &scriptedChecker{errs: []error{errors.New("boom")}}
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	r := updatecheck.New(checker, time.Hour, time.Hour)
	r.Start(ctx, &wg)
	for checker.Calls() == 0 {
		time.Sleep(time.Millisecond)
	}
	r.Start(ctx, &wg)
	cancel()
	wg.Wait()
	if got := checker.Calls(); got != 1 {
		t.Fatalf("calls = %d, want 1: the second Start must be skipped while the first is retrying", got)
	}
}
