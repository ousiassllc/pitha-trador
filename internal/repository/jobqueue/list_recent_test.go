package jobqueue_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/jobqueue"
)

// ListRecent merges open rows (ranked by started_at / created_at) and
// finished rows (ranked by finished_at) from several queues, newest first,
// and trims to limit.
func TestJobRepository_ListRecent_MergesOpenAndFinishedAcrossQueues(t *testing.T) {
	repo := jobqueue.NewJobRepository(newTestDB(t))
	ctx := context.Background()
	base := time.Date(2026, 9, 29, 9, 0, 0, 0, time.UTC)

	finished := func(queue string, at time.Time, fail bool) int64 {
		t.Helper()
		j, err := repo.Enqueue(ctx, queue, "{}", base.Add(-time.Hour))
		if err != nil {
			t.Fatalf("Enqueue: %v", err)
		}
		if fail {
			err = repo.MarkFailed(ctx, j.ID, at, "boom")
		} else {
			err = repo.MarkSucceeded(ctx, j.ID, at)
		}
		if err != nil {
			t.Fatalf("mark finished: %v", err)
		}
		return j.ID
	}
	oldest := finished(jobqueue.JobQueueMarketData, base.Add(1*time.Minute), false)
	failed := finished(jobqueue.JobQueueJevScout, base.Add(4*time.Minute), true)
	ok := finished(jobqueue.JobQueueJevScout, base.Add(2*time.Minute), false)
	tradeOK := finished(jobqueue.JobQueueJevTrader, base.Add(5*time.Minute), false)

	if _, err := repo.Enqueue(ctx, jobqueue.JobQueueMarketData, "{}", base.Add(-time.Minute)); err != nil {
		t.Fatalf("Enqueue running: %v", err)
	}
	running, err := repo.ClaimNext(ctx, jobqueue.JobQueueMarketData, base.Add(3*time.Minute))
	if err != nil {
		t.Fatalf("ClaimNext: %v", err)
	}

	all, err := repo.ListRecent(ctx, "", 10)
	if err != nil {
		t.Fatalf("ListRecent: %v", err)
	}
	if want := []int64{tradeOK, failed, running.ID, ok, oldest}; !slices.Equal(jobIDs(all), want) {
		t.Fatalf("ListRecent(all) IDs = %v, want %v", jobIDs(all), want)
	}

	top2, err := repo.ListRecent(ctx, "", 2)
	if err != nil {
		t.Fatalf("ListRecent(limit=2): %v", err)
	}
	if want := []int64{tradeOK, failed}; !slices.Equal(jobIDs(top2), want) {
		t.Fatalf("ListRecent(limit=2) IDs = %v, want %v", jobIDs(top2), want)
	}

	scout, err := repo.ListRecent(ctx, jobqueue.JobQueueJevScout, 1)
	if err != nil {
		t.Fatalf("ListRecent(jev-scout): %v", err)
	}
	if want := []int64{failed}; !slices.Equal(jobIDs(scout), want) {
		t.Fatalf("ListRecent(jev-scout, 1) IDs = %v, want %v", jobIDs(scout), want)
	}
}
