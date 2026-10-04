package jobqueue_test

import (
	"context"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/jobqueue"
)

func TestJobRepository_FailOrphanedRunning_OnlyOldRunningOnQueue(t *testing.T) {
	repo := jobqueue.NewJobRepository(newTestDB(t))
	ctx := context.Background()
	base := time.Date(2026, 9, 27, 9, 0, 0, 0, time.UTC)

	enqueueClaim := func(queue string, startedAt time.Time) jobqueue.Job {
		t.Helper()
		if _, err := repo.Enqueue(ctx, queue, "{}", base.Add(-time.Hour)); err != nil {
			t.Fatalf("Enqueue(%s): %v", queue, err)
		}
		job, err := repo.ClaimNext(ctx, queue, startedAt)
		if err != nil {
			t.Fatalf("ClaimNext(%s): %v", queue, err)
		}
		return job
	}
	oldMD := enqueueClaim(jobqueue.JobQueueMarketData, base.Add(-30*time.Minute))
	freshMD := enqueueClaim(jobqueue.JobQueueMarketData, base.Add(-time.Minute))
	oldScout := enqueueClaim(jobqueue.JobQueueJevScout, base.Add(-30*time.Minute))
	pending, err := repo.Enqueue(ctx, jobqueue.JobQueueMarketData, "{}", base)
	if err != nil {
		t.Fatalf("Enqueue pending: %v", err)
	}

	var notified []int64
	repo.SetObserver(func(_ context.Context, job jobqueue.Job) { notified = append(notified, job.ID) })

	closed, err := repo.FailOrphanedRunning(ctx, jobqueue.JobQueueMarketData, base.Add(-10*time.Minute), base, "orphaned")
	if err != nil {
		t.Fatalf("FailOrphanedRunning: %v", err)
	}
	if len(closed) != 1 || closed[0].ID != oldMD.ID || closed[0].Status != jobqueue.JobStatusFailed {
		t.Fatalf("closed = %+v, want only old market-data job %d failed", closed, oldMD.ID)
	}
	if len(notified) != 1 || notified[0] != oldMD.ID {
		t.Errorf("observer notified %v, want [%d]", notified, oldMD.ID)
	}

	for id, want := range map[int64]string{
		oldMD.ID:    jobqueue.JobStatusFailed,
		freshMD.ID:  jobqueue.JobStatusRunning,
		oldScout.ID: jobqueue.JobStatusRunning,
		pending.ID:  jobqueue.JobStatusPending,
	} {
		got, err := repo.Get(ctx, id)
		if err != nil {
			t.Fatalf("Get(%d): %v", id, err)
		}
		if got.Status != want {
			t.Errorf("job %d status = %q, want %q", id, got.Status, want)
		}
	}
}
