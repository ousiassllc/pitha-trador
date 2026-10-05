package jobqueue_test

import (
	"context"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/jobqueue"
)

// ListOpenOrFinishedSince returns pending/running jobs of the queue and
// those finished at or after the cutoff; older finished jobs and other
// queues' jobs are excluded.
func TestJobRepository_ListOpenOrFinishedSince(t *testing.T) {
	repo := jobqueue.NewJobRepository(newTestDB(t))
	ctx := context.Background()
	now := time.Date(2026, 10, 4, 9, 0, 0, 0, time.UTC)
	since := now.Add(-time.Minute)

	// ClaimNext takes the oldest due job first, so claim the four jobs
	// that need a non-pending state before enqueueing the pending one.
	for _, payload := range []string{"running", "old-done", "new-done", "new-failed"} {
		if _, err := repo.Enqueue(ctx, jobqueue.JobQueueJevScout, `{"n":"`+payload+`"}`, now); err != nil {
			t.Fatalf("Enqueue %s: %v", payload, err)
		}
	}
	claimed := make(map[string]jobqueue.Job)
	for _, payload := range []string{"running", "old-done", "new-done", "new-failed"} {
		job, err := repo.ClaimNext(ctx, jobqueue.JobQueueJevScout, now)
		if err != nil {
			t.Fatalf("ClaimNext %s: %v", payload, err)
		}
		claimed[payload] = job
	}
	if err := repo.MarkSucceeded(ctx, claimed["old-done"].ID, since.Add(-time.Second)); err != nil {
		t.Fatalf("MarkSucceeded old-done: %v", err)
	}
	if err := repo.MarkSucceeded(ctx, claimed["new-done"].ID, since); err != nil {
		t.Fatalf("MarkSucceeded new-done: %v", err)
	}
	if err := repo.MarkFailed(ctx, claimed["new-failed"].ID, now, "boom"); err != nil {
		t.Fatalf("MarkFailed new-failed: %v", err)
	}
	if _, err := repo.Enqueue(ctx, jobqueue.JobQueueJevScout, `{"n":"pending"}`, now); err != nil {
		t.Fatalf("Enqueue pending: %v", err)
	}
	if _, err := repo.Enqueue(ctx, jobqueue.JobQueueJevTrader, `{"n":"other-queue"}`, now); err != nil {
		t.Fatalf("Enqueue other-queue: %v", err)
	}

	got, err := repo.ListOpenOrFinishedSince(ctx, jobqueue.JobQueueJevScout, since)
	if err != nil {
		t.Fatalf("ListOpenOrFinishedSince: %v", err)
	}
	var payloads []string
	for _, job := range got {
		payloads = append(payloads, job.PayloadJSON)
	}
	want := []string{`{"n":"running"}`, `{"n":"new-done"}`, `{"n":"new-failed"}`, `{"n":"pending"}`}
	if len(payloads) != len(want) {
		t.Fatalf("payloads = %v, want %v", payloads, want)
	}
	for i := range want {
		if payloads[i] != want[i] {
			t.Fatalf("payloads = %v, want %v", payloads, want)
		}
	}
}
