package jobqueue_test

import (
	"context"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/jobqueue"
)

// failedAt enqueues a job and ends it with status at finishedAt.
func failedAt(t *testing.T, repo *jobqueue.JobRepository, queue string, finishedAt time.Time, failed bool, lastErr string) int64 {
	t.Helper()
	ctx := context.Background()
	job, err := repo.Enqueue(ctx, queue, `{"secret":"payload"}`, finishedAt)
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if failed {
		err = repo.MarkFailed(ctx, job.ID, finishedAt, lastErr)
	} else {
		err = repo.MarkSucceeded(ctx, job.ID, finishedAt)
	}
	if err != nil {
		t.Fatalf("finish job %d: %v", job.ID, err)
	}
	return job.ID
}

func TestJobRepository_ListFailedAfter_WindowOrderAndPaging(t *testing.T) {
	repo := jobqueue.NewJobRepository(newTestDB(t))
	ctx := context.Background()
	t0 := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)

	before := failedAt(t, repo, jobqueue.JobQueueMarketData, t0, true, "before window")
	// Two rows share a finished_at; the page boundary must fall between them.
	tieA := failedAt(t, repo, jobqueue.JobQueueJevScout, t0.Add(time.Second), true, "tie a")
	tieB := failedAt(t, repo, jobqueue.JobQueueMarketData, t0.Add(time.Second), true, "tie b")
	failedAt(t, repo, jobqueue.JobQueueMarketData, t0.Add(2*time.Second), false, "") // succeeded: never listed
	last := failedAt(t, repo, jobqueue.JobQueueJevTrader, t0.Add(3*time.Second), true, "last")
	failedAt(t, repo, jobqueue.JobQueueMarketData, t0.Add(10*time.Second), true, "after window")

	upTo := t0.Add(3 * time.Second)
	got, err := repo.ListFailedAfter(ctx, jobqueue.FailedCursorAtTime(t0), upTo, 10)
	if err != nil {
		t.Fatalf("ListFailedAfter: %v", err)
	}
	ids := make([]int64, len(got))
	for i, j := range got {
		ids[i] = j.ID
	}
	want := []int64{tieA, tieB, last}
	if len(ids) != len(want) || ids[0] != want[0] || ids[1] != want[1] || ids[2] != want[2] {
		t.Fatalf("ids = %v (before=%d), want %v: strictly after the cursor time, up to upTo inclusive, failed only, oldest first", ids, before, want)
	}
	if got[0].Queue != jobqueue.JobQueueJevScout || got[0].LastError != "tie a" || !got[0].FinishedAt.Equal(t0.Add(time.Second)) {
		t.Errorf("first row = %+v, want queue/last_error/finished_at of the stored row", got[0])
	}

	// Paging one row at a time visits every row exactly once, including both
	// rows of the shared finished_at.
	var paged []int64
	cursor := jobqueue.FailedCursorAtTime(t0)
	for range 10 {
		page, err := repo.ListFailedAfter(ctx, cursor, upTo, 1)
		if err != nil {
			t.Fatalf("page: %v", err)
		}
		if len(page) == 0 {
			break
		}
		paged = append(paged, page[0].ID)
		cursor = page[0].Cursor()
	}
	if len(paged) != len(want) || paged[0] != want[0] || paged[1] != want[1] || paged[2] != want[2] {
		t.Fatalf("paged ids = %v, want %v", paged, want)
	}
}

func TestJobRepository_ListFailedAfter_NonPositiveLimit(t *testing.T) {
	repo := jobqueue.NewJobRepository(newTestDB(t))
	got, err := repo.ListFailedAfter(context.Background(), jobqueue.FailedCursorAtTime(time.Time{}), time.Now(), 0)
	if err != nil || got != nil {
		t.Fatalf("ListFailedAfter(limit 0) = %v, %v; want nil, nil", got, err)
	}
}
