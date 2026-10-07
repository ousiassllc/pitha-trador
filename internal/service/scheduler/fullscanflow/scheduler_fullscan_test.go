package fullscanflow_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/jobqueue"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/service/scheduler"
)

func TestScheduler_EnqueueFullScan_OnlyActiveInstruments(t *testing.T) {
	db := newTestDB(t)
	instruments := market.NewInstrumentRepository(db)
	jobs := jobqueue.NewJobRepository(db)

	mustCreateInstrument(t, instruments, "7203", true)
	mustCreateInstrument(t, instruments, "9433", true)
	mustCreateInstrument(t, instruments, "1301", false)

	s := scheduler.New(jobs, instruments)
	now := time.Date(2026, 9, 27, 9, 0, 0, 0, time.UTC)

	count, err := s.EnqueueFullScan(context.Background(), now)
	if err != nil {
		t.Fatalf("EnqueueFullScan: %v", err)
	}
	if count != 2 {
		t.Fatalf("EnqueueFullScan count = %d, want 2 (inactive instrument excluded)", count)
	}

	claimed := 0
	for {
		if _, err := jobs.ClaimNext(context.Background(), jobqueue.JobQueueMarketData, now); err != nil {
			if errors.Is(err, jobqueue.ErrJobNotFound) {
				break
			}
			t.Fatalf("ClaimNext(market-data): %v", err)
		}
		claimed++
	}
	if claimed != 2 {
		t.Errorf("market-data had %d due jobs, want 2 (one per active instrument)", claimed)
	}
	// The no-op feature-calc queue is no longer fed (non-functional.md §2.3).
	if _, err := jobs.ClaimNext(context.Background(), jobqueue.JobQueueFeatureCalc, now); !errors.Is(err, jobqueue.ErrJobNotFound) {
		t.Errorf("ClaimNext(feature-calc) err = %v, want ErrJobNotFound (full scan must not enqueue feature-calc)", err)
	}
}

func TestScheduler_EnqueueFullScan_SkipsWhilePreviousCycleUnfinished(t *testing.T) {
	db := newTestDB(t)
	instruments := market.NewInstrumentRepository(db)
	jobs := jobqueue.NewJobRepository(db)
	ctx := context.Background()

	mustCreateInstrument(t, instruments, "7203", true)
	mustCreateInstrument(t, instruments, "9433", true)

	s := scheduler.New(jobs, instruments)
	now := time.Date(2026, 9, 27, 9, 0, 0, 0, time.UTC)

	if count, err := s.EnqueueFullScan(ctx, now); err != nil || count != 2 {
		t.Fatalf("first EnqueueFullScan = (%d, %v), want (2, nil)", count, err)
	}

	// Previous cycle fully pending: the next cycle must enqueue nothing.
	next := now.Add(time.Minute)
	if count, err := s.EnqueueFullScan(ctx, next); err != nil || count != 0 {
		t.Fatalf("EnqueueFullScan with pending jobs = (%d, %v), want (0, nil)", count, err)
	}

	// One job still running (the rest done) also counts as unfinished.
	claimed, err := jobs.ClaimNext(ctx, jobqueue.JobQueueMarketData, next)
	if err != nil {
		t.Fatalf("ClaimNext: %v", err)
	}
	other, err := jobs.ClaimNext(ctx, jobqueue.JobQueueMarketData, next)
	if err != nil {
		t.Fatalf("ClaimNext second: %v", err)
	}
	if err := jobs.MarkSucceeded(ctx, other.ID, next); err != nil {
		t.Fatalf("MarkSucceeded: %v", err)
	}
	if count, err := s.EnqueueFullScan(ctx, next); err != nil || count != 0 {
		t.Fatalf("EnqueueFullScan with running job = (%d, %v), want (0, nil)", count, err)
	}
	if got := countJobs(t, jobs, jobqueue.JobQueueMarketData, next); got != 2 {
		t.Fatalf("market-data unfinished/total jobs = %d, want 2 (no new jobs enqueued)", got)
	}

	// Once the previous cycle has completed, the scan resumes.
	if err := jobs.MarkSucceeded(ctx, claimed.ID, next); err != nil {
		t.Fatalf("MarkSucceeded: %v", err)
	}
	if count, err := s.EnqueueFullScan(ctx, next); err != nil || count != 2 {
		t.Fatalf("EnqueueFullScan after completion = (%d, %v), want (2, nil)", count, err)
	}
}

// A worker whose completion write failed leaves its market-data row
// running; once it is older than the orphan threshold it must not block
// the full scan forever (issue #416).
func TestScheduler_EnqueueFullScan_RecoversOrphanedRunningJob(t *testing.T) {
	db := newTestDB(t)
	instruments := market.NewInstrumentRepository(db)
	jobs := jobqueue.NewJobRepository(db)
	ctx := context.Background()

	mustCreateInstrument(t, instruments, "7203", true)
	mustCreateInstrument(t, instruments, "9433", true)

	s := scheduler.New(jobs, instruments)
	now := time.Date(2026, 9, 27, 9, 0, 0, 0, time.UTC)

	if count, err := s.EnqueueFullScan(ctx, now); err != nil || count != 2 {
		t.Fatalf("first EnqueueFullScan = (%d, %v), want (2, nil)", count, err)
	}
	orphan, err := jobs.ClaimNext(ctx, jobqueue.JobQueueMarketData, now)
	if err != nil {
		t.Fatalf("ClaimNext: %v", err)
	}
	other, err := jobs.ClaimNext(ctx, jobqueue.JobQueueMarketData, now)
	if err != nil {
		t.Fatalf("ClaimNext second: %v", err)
	}
	if err := jobs.MarkSucceeded(ctx, other.ID, now); err != nil {
		t.Fatalf("MarkSucceeded: %v", err)
	}

	// Within the threshold the running row still counts as unfinished.
	if count, err := s.EnqueueFullScan(ctx, now.Add(5*time.Minute)); err != nil || count != 0 {
		t.Fatalf("EnqueueFullScan with fresh running job = (%d, %v), want (0, nil)", count, err)
	}

	// Past the threshold it is an orphan: the scan resumes and the row is closed.
	later := now.Add(11 * time.Minute)
	if count, err := s.EnqueueFullScan(ctx, later); err != nil || count != 2 {
		t.Fatalf("EnqueueFullScan with orphaned running job = (%d, %v), want (2, nil)", count, err)
	}
	got, err := jobs.Get(ctx, orphan.ID)
	if err != nil {
		t.Fatalf("Get orphan: %v", err)
	}
	if got.Status != jobqueue.JobStatusFailed || got.FinishedAt == nil || got.LastError == nil {
		t.Fatalf("orphan job = %+v, want failed with finished_at and last_error", got)
	}
}

func countJobs(t *testing.T, jobs *jobqueue.JobRepository, queue string, since time.Time) int {
	t.Helper()
	list, err := jobs.ListOpenOrFinishedSince(context.Background(), queue, since.Add(-time.Hour))
	if err != nil {
		t.Fatalf("ListOpenOrFinishedSince: %v", err)
	}
	return len(list)
}
