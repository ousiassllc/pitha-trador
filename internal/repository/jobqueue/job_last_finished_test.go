package jobqueue_test

import (
	"context"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/jobqueue"
)

// LastFinishedAt is the newest finish of a succeeded or failed job of the
// queue: pending/running jobs and other queues do not count, and a queue with
// nothing finished reports ok=false.
func TestJobRepository_LastFinishedAt(t *testing.T) {
	repo := jobqueue.NewJobRepository(newTestDB(t))
	ctx := context.Background()
	t0 := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)

	if _, ok, err := repo.LastFinishedAt(ctx, jobqueue.JobQueueMarketData); err != nil || ok {
		t.Fatalf("empty queue: ok=%v err=%v, want ok=false", ok, err)
	}

	// ClaimNext takes the oldest due job first: claim before enqueueing the rest.
	for range 3 {
		if _, err := repo.Enqueue(ctx, jobqueue.JobQueueMarketData, `{}`, t0); err != nil {
			t.Fatalf("Enqueue: %v", err)
		}
	}
	var ids []int64
	for range 3 {
		job, err := repo.ClaimNext(ctx, jobqueue.JobQueueMarketData, t0)
		if err != nil {
			t.Fatalf("ClaimNext: %v", err)
		}
		ids = append(ids, job.ID)
	}
	if err := repo.MarkSucceeded(ctx, ids[0], t0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := repo.MarkFailed(ctx, ids[1], t0.Add(3*time.Minute), "boom"); err != nil {
		t.Fatal(err)
	}
	// ids[2] stays running; another queue's finish is later but not counted.
	if _, err := repo.Enqueue(ctx, jobqueue.JobQueueJevScout, `{}`, t0); err != nil {
		t.Fatal(err)
	}
	other, err := repo.ClaimNext(ctx, jobqueue.JobQueueJevScout, t0)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.MarkSucceeded(ctx, other.ID, t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	got, ok, err := repo.LastFinishedAt(ctx, jobqueue.JobQueueMarketData)
	if err != nil || !ok || !got.Equal(t0.Add(3*time.Minute)) {
		t.Fatalf("LastFinishedAt = %v, %v, %v; want %v (the failed job's finish)", got, ok, err, t0.Add(3*time.Minute))
	}
}
