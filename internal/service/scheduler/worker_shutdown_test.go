package scheduler_test

import (
	"context"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/jobqueue"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/service/scheduler"
)

// runOneJobCancellingInHandler runs a single job whose handler cancels the
// scheduler's context (simulating shutdown mid-job) and returns handlerErr.
// It returns the scheduler and the job repository once the worker stopped.
func runOneJobCancellingInHandler(t *testing.T, handlerErr error) (*scheduler.Scheduler, *jobqueue.JobRepository, jobqueue.Job) {
	t.Helper()
	db := newTestDB(t)
	instruments := market.NewInstrumentRepository(db)
	jobs := jobqueue.NewJobRepository(db)
	job, err := jobs.Enqueue(context.Background(), jobqueue.JobQueueMarketData, `{}`, time.Now().UTC())
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	polls := make(chan time.Time)
	s := scheduler.New(jobs, instruments, scheduler.WithPollSignalForTest(polls))
	ctx, cancel := context.WithCancel(context.Background())
	handled := make(chan struct{})
	s.RegisterHandler(jobqueue.JobQueueMarketData, func(context.Context, jobqueue.Job) error {
		cancel()
		close(handled)
		return handlerErr
	})
	t.Cleanup(cancel)
	if err := s.Start(ctx, time.Hour); err != nil {
		t.Fatalf("Start: %v", err)
	}
	polls <- time.Now()
	<-handled
	s.Stop() // waits for the worker to finish the in-flight job
	return s, jobs, job
}

func TestScheduler_Worker_RecordsSuccessEvenIfContextCancelledAfterHandler(t *testing.T) {
	s, jobs, job := runOneJobCancellingInHandler(t, nil)

	got, err := jobs.Get(context.Background(), job.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != jobqueue.JobStatusSucceeded {
		t.Fatalf("status = %q, want succeeded (a finished job must not stay running)", got.Status)
	}
	n, err := s.Recover(context.Background())
	if err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if n != 0 {
		t.Errorf("Recover re-queued %d jobs, want 0", n)
	}
}

func TestScheduler_Worker_LeavesJobRunningWhenHandlerInterruptedByCancel(t *testing.T) {
	s, jobs, job := runOneJobCancellingInHandler(t, context.Canceled)

	got, err := jobs.Get(context.Background(), job.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != jobqueue.JobStatusRunning {
		t.Fatalf("status = %q, want running so Recover re-runs it", got.Status)
	}
	n, err := s.Recover(context.Background())
	if err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if n != 1 {
		t.Errorf("Recover re-queued %d jobs, want 1", n)
	}
}
