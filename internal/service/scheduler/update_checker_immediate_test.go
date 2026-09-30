package scheduler_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/jobqueue"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/service/scheduler"
)

// syncUpdateChecker is a scheduler.UpdateChecker whose call count is safe
// to read from the test goroutine while Start's own goroutine writes to
// it concurrently.
type syncUpdateChecker struct {
	mu    sync.Mutex
	calls int
}

func (c *syncUpdateChecker) CheckForUpdate(ctx context.Context) error {
	c.mu.Lock()
	c.calls++
	c.mu.Unlock()
	return nil
}

func (c *syncUpdateChecker) Calls() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

// TestScheduler_Start_UpdateCheckerRunsImmediately regresses issue #71:
// updateCheckCronSpec's "@every 6h" schedules its first cron.AddFunc
// firing 6h after cron.Start() (robfig/cron/v3's ConstantDelaySchedule
// computes Next(now) = now.Add(delay), never now itself), so a
// WithUpdateChecker configured Start must trigger CheckForUpdate once
// itself, immediately, rather than leaving every process lifetime
// shorter than 6h without a single update check (functional.md's issue
// #65 update mechanism otherwise never fires for cmd/desktop's typical
// intraday restart cadence).
func TestScheduler_Start_UpdateCheckerRunsImmediately(t *testing.T) {
	db := newTestDB(t)
	instruments := market.NewInstrumentRepository(db)
	jobs := jobqueue.NewJobRepository(db)
	checker := &syncUpdateChecker{}

	s := scheduler.New(jobs, instruments, scheduler.WithUpdateChecker(checker))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// A 1h full-scan interval keeps that unrelated trigger from firing
	// during this test; only the update-check trigger is under test.
	if err := s.Start(ctx, 1*time.Hour); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Stop()

	deadline := time.Now().Add(1 * time.Second)
	for checker.Calls() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for Start to run the update checker immediately, without waiting for the @every 6h cron interval")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// blockingUpdateChecker is a scheduler.UpdateChecker whose CheckForUpdate
// signals Started and then blocks until Release is closed, so a test can
// observe "the check is in flight" and control exactly when it finishes.
type blockingUpdateChecker struct {
	Started chan struct{}
	Release chan struct{}
}

func newBlockingUpdateChecker() *blockingUpdateChecker {
	return &blockingUpdateChecker{Started: make(chan struct{}), Release: make(chan struct{})}
}

func (c *blockingUpdateChecker) CheckForUpdate(ctx context.Context) error {
	close(c.Started)
	<-c.Release
	return nil
}

// TestScheduler_Stop_WaitsForImmediateUpdateCheck regresses the immediate
// update check (issue #71) being a bare `go checkForUpdate()` untracked by
// s.wg: Stop's doc comment promises "blocking until the workers have
// exited", but a plain goroutine outside s.wg would let Stop (and thus a
// caller about to os.Exit) return while an installer download/verification
// is still in flight. The immediate check must be tracked on s.wg like
// runWorker's goroutines so Stop actually waits for it.
func TestScheduler_Stop_WaitsForImmediateUpdateCheck(t *testing.T) {
	db := newTestDB(t)
	instruments := market.NewInstrumentRepository(db)
	jobs := jobqueue.NewJobRepository(db)
	checker := newBlockingUpdateChecker()

	s := scheduler.New(jobs, instruments, scheduler.WithUpdateChecker(checker))
	if err := s.Start(context.Background(), 1*time.Hour); err != nil {
		t.Fatalf("Start: %v", err)
	}

	select {
	case <-checker.Started:
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for the immediate update check to start")
	}

	stopped := make(chan struct{})
	go func() {
		s.Stop()
		close(stopped)
	}()

	select {
	case <-stopped:
		t.Fatal("Stop returned while the immediate update check was still in flight")
	case <-time.After(100 * time.Millisecond):
	}

	close(checker.Release)

	select {
	case <-stopped:
	case <-time.After(1 * time.Second):
		t.Fatal("Stop did not return after the immediate update check finished")
	}
}
