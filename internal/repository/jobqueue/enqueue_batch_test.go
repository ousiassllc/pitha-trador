package jobqueue_test

import (
	"context"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/jobqueue"
)

func TestJobRepository_EnqueueBatch_InsertsEveryPayloadAndNotifiesObserver(t *testing.T) {
	repo := jobqueue.NewJobRepository(newTestDB(t))
	ctx := context.Background()
	due := time.Date(2026, 9, 29, 9, 0, 0, 0, time.UTC)

	var seen []jobqueue.Job
	repo.SetObserver(func(_ context.Context, j jobqueue.Job) { seen = append(seen, j) })

	n, err := repo.EnqueueBatch(ctx, jobqueue.JobQueueMarketData, []string{`{"a":1}`, `{"a":2}`, `{"a":3}`}, due)
	if err != nil || n != 3 {
		t.Fatalf("EnqueueBatch = (%d, %v), want (3, nil)", n, err)
	}
	if len(seen) != 3 || seen[0].PayloadJSON != `{"a":1}` || seen[2].PayloadJSON != `{"a":3}` {
		t.Fatalf("observed jobs = %+v, want the 3 payloads in order", seen)
	}
	for _, j := range seen {
		if j.Status != jobqueue.JobStatusPending || !j.ScheduledAt.Equal(due) || j.Queue != jobqueue.JobQueueMarketData {
			t.Fatalf("observed job = %+v, want pending market-data job due at %v", j, due)
		}
		got, err := repo.Get(ctx, j.ID)
		if err != nil || got.PayloadJSON != j.PayloadJSON {
			t.Fatalf("Get(%d) = (%+v, %v), want the observed job's row", j.ID, got, err)
		}
	}

	// Claimable in insertion order.
	first, err := repo.ClaimNext(ctx, jobqueue.JobQueueMarketData, due)
	if err != nil || first.PayloadJSON != `{"a":1}` {
		t.Fatalf("ClaimNext = (%+v, %v), want the first batched job", first, err)
	}

	if n, err := repo.EnqueueBatch(ctx, jobqueue.JobQueueMarketData, nil, due); err != nil || n != 0 {
		t.Fatalf("EnqueueBatch(empty) = (%d, %v), want (0, nil)", n, err)
	}
}
