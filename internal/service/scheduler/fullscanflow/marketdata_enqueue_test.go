package fullscanflow_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/jobqueue"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/service/scheduler"
)

// EnqueueMarketData (the ranking watch list, FR-SCHED-9) enqueues exactly the
// given instruments even while the full scan is disabled (the default).
func TestScheduler_EnqueueMarketData_EnqueuesGivenInstrumentsWithFullScanDisabled(t *testing.T) {
	db := newTestDB(t)
	instruments := market.NewInstrumentRepository(db)
	jobs := jobqueue.NewJobRepository(db)
	watched := mustCreateInstrument(t, instruments, "7203", true)
	mustCreateInstrument(t, instruments, "6758", true) // not on the watch list

	s := scheduler.New(jobs, instruments, scheduler.WithFullScanDisabled())
	now := time.Date(2026, 9, 27, 9, 0, 0, 0, time.UTC)

	n, err := s.EnqueueMarketData(context.Background(), []domain.Instrument{watched}, now)
	if err != nil || n != 1 {
		t.Fatalf("EnqueueMarketData = (%d, %v), want 1", n, err)
	}
	job, err := jobs.ClaimNext(context.Background(), jobqueue.JobQueueMarketData, now)
	if err != nil {
		t.Fatalf("ClaimNext: %v", err)
	}
	if want := `"symbol":"7203"`; !strings.Contains(job.PayloadJSON, want) {
		t.Errorf("payload = %s, want %s", job.PayloadJSON, want)
	}
	if _, err := jobs.ClaimNext(context.Background(), jobqueue.JobQueueMarketData, now); !errors.Is(err, jobqueue.ErrJobNotFound) {
		t.Errorf("a second job was enqueued for a symbol outside the watch list: %v", err)
	}
}

// An empty watch list (empty/failed ranking, nothing held) enqueues nothing,
// and a still-unfinished previous cycle skips the next one.
func TestScheduler_EnqueueMarketData_EmptyListAndUnfinishedCycleEnqueueNothing(t *testing.T) {
	db := newTestDB(t)
	instruments := market.NewInstrumentRepository(db)
	jobs := jobqueue.NewJobRepository(db)
	watched := mustCreateInstrument(t, instruments, "7203", true)
	s := scheduler.New(jobs, instruments, scheduler.WithFullScanDisabled())
	now := time.Date(2026, 9, 27, 9, 0, 0, 0, time.UTC)

	if n, err := s.EnqueueMarketData(context.Background(), nil, now); err != nil || n != 0 {
		t.Fatalf("EnqueueMarketData(empty) = (%d, %v), want 0", n, err)
	}
	if n, _ := s.EnqueueMarketData(context.Background(), []domain.Instrument{watched}, now); n != 1 {
		t.Fatalf("first cycle = %d, want 1", n)
	}
	if n, err := s.EnqueueMarketData(context.Background(), []domain.Instrument{watched}, now.Add(time.Minute)); err != nil || n != 0 {
		t.Errorf("second cycle with the first unfinished = (%d, %v), want 0", n, err)
	}
}
