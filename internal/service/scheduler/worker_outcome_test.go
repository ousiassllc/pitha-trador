package scheduler_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/jobqueue"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
)

// A handler returning jobqueue.Defer must leave the job pending (retried
// later), never failed (issue #710).
func TestScheduler_Start_DeferredJobGoesBackToPendingNotFailed(t *testing.T) {
	db := newTestDB(t)
	instruments := market.NewInstrumentRepository(db)
	jobs := jobqueue.NewJobRepository(db)
	job, err := jobs.Enqueue(context.Background(), jobqueue.JobQueueMarketData, `{}`, time.Now().UTC())
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	retryAt := time.Now().UTC().Add(time.Hour)
	calls := 0
	runOnePoll(t, jobs, instruments, func(context.Context, jobqueue.Job) error {
		calls++
		return jobqueue.Defer(retryAt, "data not landed")
	})

	got, err := jobs.Get(context.Background(), job.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != jobqueue.JobStatusPending || !got.ScheduledAt.Equal(retryAt) {
		t.Fatalf("job = %+v, want pending scheduled at %v", got, retryAt)
	}
	if got.LastError == nil || !strings.HasPrefix(*got.LastError, "deferred: ") {
		t.Fatalf("LastError = %v, want a deferred: note", got.LastError)
	}
	if calls != 1 {
		t.Fatalf("handler calls = %d, want 1 (not due again before retryAt)", calls)
	}
}

// A handler returning jobqueue.Skip must end the job succeeded with a
// skipped note, never failed (issue #710).
func TestScheduler_Start_SkippedJobSucceedsWithNoteNotFailed(t *testing.T) {
	db := newTestDB(t)
	instruments := market.NewInstrumentRepository(db)
	jobs := jobqueue.NewJobRepository(db)
	job, err := jobs.Enqueue(context.Background(), jobqueue.JobQueueMarketData, `{}`, time.Now().UTC())
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	runOnePoll(t, jobs, instruments, func(context.Context, jobqueue.Job) error {
		return jobqueue.Skip("lunch break")
	})

	got, err := jobs.Get(context.Background(), job.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != jobqueue.JobStatusSucceeded {
		t.Fatalf("status = %q, want succeeded", got.Status)
	}
	if got.LastError == nil || *got.LastError != "skipped: lunch break" {
		t.Fatalf("LastError = %v, want %q", got.LastError, "skipped: lunch break")
	}
}
