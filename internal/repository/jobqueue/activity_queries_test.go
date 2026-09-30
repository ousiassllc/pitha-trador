package jobqueue_test

import (
	"context"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/jobqueue"
)

func TestJobRepository_QueueCounts_AggregatesPerQueueAndWindowsFailures(t *testing.T) {
	repo := jobqueue.NewJobRepository(newTestDB(t))
	ctx := context.Background()
	now := time.Date(2026, 9, 29, 9, 0, 0, 0, time.UTC)

	// jev-scout: 2 pending, 1 running, 1 recent failure, 1 old failure.
	for range 2 {
		if _, err := repo.Enqueue(ctx, jobqueue.JobQueueJevScout, "{}", now.Add(time.Hour)); err != nil {
			t.Fatalf("Enqueue pending: %v", err)
		}
	}
	if _, err := repo.Enqueue(ctx, jobqueue.JobQueueJevScout, "{}", now.Add(-time.Minute)); err != nil {
		t.Fatalf("Enqueue running: %v", err)
	}
	if _, err := repo.ClaimNext(ctx, jobqueue.JobQueueJevScout, now); err != nil {
		t.Fatalf("ClaimNext: %v", err)
	}
	for _, finishedAt := range []time.Time{now.Add(-10 * time.Minute), now.Add(-3 * time.Hour)} {
		j, err := repo.Enqueue(ctx, jobqueue.JobQueueJevScout, "{}", now.Add(-time.Hour))
		if err != nil {
			t.Fatalf("Enqueue failing job: %v", err)
		}
		if err := repo.MarkFailed(ctx, j.ID, finishedAt, "boom"); err != nil {
			t.Fatalf("MarkFailed: %v", err)
		}
	}
	// analytics: one succeeded job counts toward nothing.
	ok, _ := repo.Enqueue(ctx, jobqueue.JobQueueAnalytics, "{}", now)
	if err := repo.MarkSucceeded(ctx, ok.ID, now); err != nil {
		t.Fatalf("MarkSucceeded: %v", err)
	}

	counts, err := repo.QueueCounts(ctx, now.Add(-time.Hour))
	if err != nil {
		t.Fatalf("QueueCounts: %v", err)
	}
	got := map[string]jobqueue.JobQueueCount{}
	for _, c := range counts {
		got[c.Queue] = c
	}
	scout := got[jobqueue.JobQueueJevScout]
	if scout.Pending != 2 || scout.Running != 1 || scout.FailedSince != 1 {
		t.Fatalf("jev-scout counts = %+v, want pending=2 running=1 failedSince=1", scout)
	}
	if a := got[jobqueue.JobQueueAnalytics]; a.Pending != 0 || a.Running != 0 || a.FailedSince != 0 {
		t.Fatalf("analytics counts = %+v, want all zero", a)
	}
}

func TestJobRepository_ListRecent_OrdersByLatestActivityAndFiltersQueue(t *testing.T) {
	repo := jobqueue.NewJobRepository(newTestDB(t))
	ctx := context.Background()
	now := time.Date(2026, 9, 29, 9, 0, 0, 0, time.UTC)

	older, _ := repo.Enqueue(ctx, jobqueue.JobQueueMarketData, "{}", now)
	if err := repo.MarkSucceeded(ctx, older.ID, now.Add(time.Minute)); err != nil {
		t.Fatalf("MarkSucceeded: %v", err)
	}
	newer, _ := repo.Enqueue(ctx, jobqueue.JobQueueFeatureCalc, "{}", now)
	if err := repo.MarkSucceeded(ctx, newer.ID, now.Add(2*time.Minute)); err != nil {
		t.Fatalf("MarkSucceeded: %v", err)
	}

	all, err := repo.ListRecent(ctx, "", 10)
	if err != nil {
		t.Fatalf("ListRecent: %v", err)
	}
	if len(all) != 2 || all[0].ID != newer.ID || all[1].ID != older.ID {
		t.Fatalf("ListRecent(all) IDs = %v, want [%d %d]", jobIDs(all), newer.ID, older.ID)
	}

	filtered, _ := repo.ListRecent(ctx, jobqueue.JobQueueMarketData, 10)
	if len(filtered) != 1 || filtered[0].ID != older.ID {
		t.Fatalf("ListRecent(market-data) IDs = %v, want [%d]", jobIDs(filtered), older.ID)
	}

	limited, _ := repo.ListRecent(ctx, "", 1)
	if len(limited) != 1 || limited[0].ID != newer.ID {
		t.Fatalf("ListRecent(limit=1) IDs = %v, want [%d]", jobIDs(limited), newer.ID)
	}
}

func jobIDs(jobs []jobqueue.Job) []int64 {
	ids := make([]int64, len(jobs))
	for i, j := range jobs {
		ids[i] = j.ID
	}
	return ids
}

func TestJobRepository_Observer_SeesEveryCommittedTransition(t *testing.T) {
	repo := jobqueue.NewJobRepository(newTestDB(t))
	ctx := context.Background()
	now := time.Date(2026, 9, 29, 9, 0, 0, 0, time.UTC)

	var seen []string
	repo.SetObserver(func(_ context.Context, j jobqueue.Job) { seen = append(seen, j.Status) })

	j, _ := repo.Enqueue(ctx, jobqueue.JobQueueJevTrader, "{}", now)
	if _, err := repo.ClaimNext(ctx, jobqueue.JobQueueJevTrader, now); err != nil {
		t.Fatalf("ClaimNext: %v", err)
	}
	if err := repo.MarkFailed(ctx, j.ID, now, "boom"); err != nil {
		t.Fatalf("MarkFailed: %v", err)
	}
	// A failed write (unknown id) must not notify.
	if err := repo.MarkSucceeded(ctx, 9999, now); err == nil {
		t.Fatalf("MarkSucceeded(unknown) = nil, want error")
	}

	want := []string{jobqueue.JobStatusPending, jobqueue.JobStatusRunning, jobqueue.JobStatusFailed}
	if len(seen) != len(want) {
		t.Fatalf("observed statuses = %v, want %v", seen, want)
	}
	for i := range want {
		if seen[i] != want[i] {
			t.Fatalf("observed statuses = %v, want %v", seen, want)
		}
	}
}
