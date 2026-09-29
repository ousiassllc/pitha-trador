package scheduler_test

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository"
	"github.com/ousiassllc/pitha-trador/internal/service/scheduler"
)

func TestScheduler_Start_HandlerPanicMarksJobFailedAndKeepsWorkerAlive(t *testing.T) {
	db := newTestDB(t)
	instruments := repository.NewInstrumentRepository(db)
	jobs := repository.NewJobRepository(db)

	now := time.Now().UTC()
	panicJob, err := jobs.Enqueue(context.Background(), repository.JobQueueMarketData, `{"boom":true}`, now)
	if err != nil {
		t.Fatalf("Enqueue panic job: %v", err)
	}
	okJob, err := jobs.Enqueue(context.Background(), repository.JobQueueMarketData, `{"boom":false}`, now.Add(time.Millisecond))
	if err != nil {
		t.Fatalf("Enqueue ok job: %v", err)
	}

	s := scheduler.New(jobs, instruments, scheduler.WithPollInterval(5*time.Millisecond))
	s.RegisterHandler(repository.JobQueueMarketData, func(_ context.Context, j repository.Job) error {
		if j.ID == panicJob.ID {
			var payload []int
			_ = payload[len(j.PayloadJSON)] // index out of range: panics
		}
		return nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := s.Start(ctx, time.Hour); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Stop()

	deadline := time.Now().Add(2 * time.Second)
	for {
		gotPanic, err := jobs.Get(context.Background(), panicJob.ID)
		if err != nil {
			t.Fatalf("Get panic job: %v", err)
		}
		gotOK, err := jobs.Get(context.Background(), okJob.ID)
		if err != nil {
			t.Fatalf("Get ok job: %v", err)
		}
		if gotPanic.Status == repository.JobStatusFailed && gotOK.Status == repository.JobStatusSucceeded {
			if gotPanic.LastError == nil || !strings.Contains(*gotPanic.LastError, "handler panic") {
				t.Errorf("LastError = %v, want it to mention the handler panic", gotPanic.LastError)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("panic job status = %q, following job status = %q; want failed and succeeded", gotPanic.Status, gotOK.Status)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func TestScheduler_Start_DrainsBacklogWithinOnePoll(t *testing.T) {
	db := newTestDB(t)
	instruments := repository.NewInstrumentRepository(db)
	jobs := repository.NewJobRepository(db)

	const total = 40
	now := time.Now().UTC()
	for range total {
		if _, err := jobs.Enqueue(context.Background(), repository.JobQueueMarketData, `{}`, now); err != nil {
			t.Fatalf("Enqueue: %v", err)
		}
	}

	var processed atomic.Int64
	s := scheduler.New(jobs, instruments, scheduler.WithPollInterval(50*time.Millisecond))
	s.RegisterHandler(repository.JobQueueMarketData, func(context.Context, repository.Job) error {
		processed.Add(1)
		return nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := s.Start(ctx, time.Hour); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Stop()

	// Handling one job per 50ms tick would take 2s for 40 jobs; draining
	// finishes them all within the first tick.
	deadline := time.Now().Add(1 * time.Second)
	for processed.Load() < total {
		if time.Now().After(deadline) {
			t.Fatalf("processed %d of %d jobs within 1s; worker is not draining the backlog per poll", processed.Load(), total)
		}
		time.Sleep(2 * time.Millisecond)
	}
}
