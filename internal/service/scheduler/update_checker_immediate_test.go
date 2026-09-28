package scheduler_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository"
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
	instruments := repository.NewInstrumentRepository(db)
	jobs := repository.NewJobRepository(db)
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
