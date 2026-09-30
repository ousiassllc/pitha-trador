package scheduler_test

import (
	"context"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/jobqueue"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/service/scheduler"
)

func countDue(t *testing.T, jobs *jobqueue.JobRepository, queue string, now time.Time) int {
	t.Helper()
	n := 0
	for {
		if _, err := jobs.ClaimNext(context.Background(), queue, now); err != nil {
			return n
		}
		n++
	}
}

func TestScheduler_SessionGate_SkipsFullScanAndEventEnqueueOffHours(t *testing.T) {
	db := newTestDB(t)
	instruments := market.NewInstrumentRepository(db)
	jobs := jobqueue.NewJobRepository(db)
	inst := mustCreateInstrument(t, instruments, "7203", true)

	open := false
	s := scheduler.New(jobs, instruments, scheduler.WithSessionGate(func(time.Time) bool { return open }))
	now := time.Date(2026, 9, 27, 3, 0, 0, 0, time.UTC)

	count, err := s.EnqueueFullScan(context.Background(), now)
	if err != nil || count != 0 {
		t.Fatalf("off-hours EnqueueFullScan = (%d, %v), want (0, nil)", count, err)
	}
	if err := s.EnqueueEventReevaluation(context.Background(), inst.ID, inst.Symbol, true, now); err != nil {
		t.Fatalf("off-hours EnqueueEventReevaluation: %v", err)
	}
	for _, q := range []string{jobqueue.JobQueueMarketData, jobqueue.JobQueueFeatureCalc, jobqueue.JobQueueJevScout} {
		if n := countDue(t, jobs, q, now); n != 0 {
			t.Errorf("queue %q has %d jobs off-hours, want 0", q, n)
		}
	}

	open = true
	if count, err := s.EnqueueFullScan(context.Background(), now); err != nil || count != 1 {
		t.Fatalf("in-session EnqueueFullScan = (%d, %v), want (1, nil)", count, err)
	}
	if err := s.EnqueueEventReevaluation(context.Background(), inst.ID, inst.Symbol, true, now); err != nil {
		t.Fatalf("in-session EnqueueEventReevaluation: %v", err)
	}
	if n := countDue(t, jobs, jobqueue.JobQueueMarketData, now); n != 1 {
		t.Errorf("market-data jobs in-session = %d, want 1", n)
	}
	if n := countDue(t, jobs, jobqueue.JobQueueJevScout, now); n != 1 {
		t.Errorf("jev-scout jobs in-session = %d, want 1", n)
	}
}
