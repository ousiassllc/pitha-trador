package jobqueue_test

import (
	"context"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/jobqueue"
)

// scheduled_at is stored as fixed-width text, so a whole-second schedule is
// due for a `now` later within the same second. With the old variable-width
// RFC3339Nano text "…:05Z" > "…:05.5Z" hid such a job for up to a second
// (issue #430).
func TestJobRepository_ClaimNext_WholeSecondJobIsDueWithinSameSecond(t *testing.T) {
	repo := jobqueue.NewJobRepository(newTestDB(t))
	ctx := context.Background()
	scheduledAt := time.Date(2026, 10, 5, 1, 0, 5, 0, time.UTC)

	job, err := repo.Enqueue(ctx, jobqueue.JobQueueFeatureCalc, "{}", scheduledAt)
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	claimed, err := repo.ClaimNext(ctx, jobqueue.JobQueueFeatureCalc, scheduledAt.Add(500*time.Millisecond))
	if err != nil {
		t.Fatalf("ClaimNext within the scheduled second: %v", err)
	}
	if claimed.ID != job.ID {
		t.Fatalf("ClaimNext() claimed job %d, want %d", claimed.ID, job.ID)
	}
}

// Within one second jobs must be claimed in scheduled_at order regardless of
// how many fractional digits their timestamps happen to need.
func TestJobRepository_ClaimNext_OrdersByScheduledAtWithinSameSecond(t *testing.T) {
	repo := jobqueue.NewJobRepository(newTestDB(t))
	ctx := context.Background()
	base := time.Date(2026, 10, 5, 1, 0, 5, 0, time.UTC)

	var ids [3]int64
	// Enqueue in an order different from the scheduled order.
	for i, offset := range []time.Duration{510 * time.Millisecond, 0, 500 * time.Millisecond} {
		job, err := repo.Enqueue(ctx, jobqueue.JobQueueFeatureCalc, "{}", base.Add(offset))
		if err != nil {
			t.Fatalf("Enqueue(%v): %v", offset, err)
		}
		ids[i] = job.ID
	}

	for _, want := range []int64{ids[1], ids[2], ids[0]} {
		claimed, err := repo.ClaimNext(ctx, jobqueue.JobQueueFeatureCalc, base.Add(time.Second))
		if err != nil {
			t.Fatalf("ClaimNext: %v", err)
		}
		if claimed.ID != want {
			t.Fatalf("ClaimNext() claimed job %d, want %d (scheduled_at order)", claimed.ID, want)
		}
	}
}
