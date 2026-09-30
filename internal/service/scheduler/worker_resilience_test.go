package scheduler_test

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/jobqueue"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/service/scheduler"
)

// runOnePoll starts a scheduler whose worker polls only when told to, and
// returns once exactly one poll has finished. The poll channel is unbuffered,
// so the second send is accepted only after poll #1's drain loop is done:
// everything handled by then was handled by that ONE poll, with no wall-clock
// timing involved.
func runOnePoll(t *testing.T, jobs *jobqueue.JobRepository, instruments *market.InstrumentRepository, h scheduler.Handler) {
	t.Helper()
	polls := make(chan time.Time)
	s := scheduler.New(jobs, instruments, scheduler.WithPollSignalForTest(polls))
	s.RegisterHandler(jobqueue.JobQueueMarketData, h)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); s.Stop() })
	if err := s.Start(ctx, time.Hour); err != nil {
		t.Fatalf("Start: %v", err)
	}
	polls <- time.Now()
	polls <- time.Now()
}

func TestScheduler_Start_HandlerPanicMarksJobFailedAndKeepsWorkerAlive(t *testing.T) {
	db := newTestDB(t)
	instruments := market.NewInstrumentRepository(db)
	jobs := jobqueue.NewJobRepository(db)

	now := time.Now().UTC()
	panicJob, err := jobs.Enqueue(context.Background(), jobqueue.JobQueueMarketData, `{"boom":true}`, now)
	if err != nil {
		t.Fatalf("Enqueue panic job: %v", err)
	}
	okJob, err := jobs.Enqueue(context.Background(), jobqueue.JobQueueMarketData, `{"boom":false}`, now.Add(time.Millisecond))
	if err != nil {
		t.Fatalf("Enqueue ok job: %v", err)
	}

	runOnePoll(t, jobs, instruments, func(_ context.Context, j jobqueue.Job) error {
		if j.ID == panicJob.ID {
			var payload []int
			_ = payload[len(j.PayloadJSON)] // index out of range: panics
		}
		return nil
	})
	gotPanic, err := jobs.Get(context.Background(), panicJob.ID)
	if err != nil {
		t.Fatalf("Get panic job: %v", err)
	}
	gotOK, err := jobs.Get(context.Background(), okJob.ID)
	if err != nil {
		t.Fatalf("Get ok job: %v", err)
	}
	if gotPanic.Status != jobqueue.JobStatusFailed || gotOK.Status != jobqueue.JobStatusSucceeded {
		t.Fatalf("panic job status = %q, following job status = %q; want failed and succeeded", gotPanic.Status, gotOK.Status)
	}
	if gotPanic.LastError == nil || !strings.Contains(*gotPanic.LastError, "handler panic") {
		t.Errorf("LastError = %v, want it to mention the handler panic", gotPanic.LastError)
	}
}

func TestScheduler_Start_DrainsBacklogWithinOnePoll(t *testing.T) {
	db := newTestDB(t)
	instruments := market.NewInstrumentRepository(db)
	jobs := jobqueue.NewJobRepository(db)

	const total = 40
	now := time.Now().UTC()
	for range total {
		if _, err := jobs.Enqueue(context.Background(), jobqueue.JobQueueMarketData, `{}`, now); err != nil {
			t.Fatalf("Enqueue: %v", err)
		}
	}

	var processed atomic.Int64
	runOnePoll(t, jobs, instruments, func(context.Context, jobqueue.Job) error {
		processed.Add(1)
		return nil
	})
	if got := processed.Load(); got != total {
		t.Fatalf("processed %d of %d jobs in a single poll; worker is not draining the backlog per poll", got, total)
	}
}
