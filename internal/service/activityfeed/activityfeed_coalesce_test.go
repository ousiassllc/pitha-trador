package activityfeed_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/jobqueue"
	"github.com/ousiassllc/pitha-trador/internal/service/activityfeed"
)

// A burst of transitions (a full scan's enqueue loop) costs one QueueCounts
// call, not one per transition, and each touched queue gets one update with
// the current depth.
func TestService_ObserveJob_CoalescesQueueUpdates(t *testing.T) {
	jobs, decisions, kill := fixtures()
	jobs.counts = []jobqueue.JobQueueCount{
		{Queue: jobqueue.JobQueueMarketData, Pending: 40, Running: 1},
		{Queue: jobqueue.JobQueueJevScout, Pending: 3, Running: 1, FailedSince: 2},
	}
	svc := activityfeed.New(jobs, decisions, kill)
	svc.SetQueueUpdateInterval(50 * time.Millisecond)
	messages, cancel := svc.Subscribe()
	defer cancel()

	for i := range 500 {
		queue := jobqueue.JobQueueMarketData
		if i%2 == 1 {
			queue = jobqueue.JobQueueJevScout
		}
		svc.ObserveJob(context.Background(), jobqueue.Job{ID: int64(i), Queue: queue, Status: jobqueue.JobStatusPending, CreatedAt: t0})
	}

	updates := map[string]activityfeed.QueueUpdate{}
	timeout := time.After(5 * time.Second)
	for len(updates) < 2 {
		select {
		case m := <-messages:
			if m.QueueUpdate != nil {
				updates[m.QueueUpdate.Queue] = *m.QueueUpdate
			}
		case <-timeout:
			t.Fatalf("queue updates = %+v, want one per touched queue", updates)
		}
	}
	if got := updates[jobqueue.JobQueueMarketData]; got.Pending != 40 || got.Running != 1 || got.FailedRecent != 0 {
		t.Fatalf("market-data update = %+v, want pending=40 running=1 failedRecent=0", got)
	}
	if got := updates[jobqueue.JobQueueJevScout]; got.Pending != 3 || got.Running != 1 || got.FailedRecent != 2 {
		t.Fatalf("jev-scout update = %+v, want pending=3 running=1 failedRecent=2", got)
	}
	if n := jobs.countCalls.Load(); n != 1 {
		t.Fatalf("QueueCounts calls = %d for 500 transitions, want 1", n)
	}
}

// failFirstCounts makes the first QueueCounts call fail, as a transient
// "database is locked" would.
type failFirstCounts struct {
	*fakeJobs
	calls atomic.Int32
}

func (f *failFirstCounts) QueueCounts(ctx context.Context, since time.Time) ([]jobqueue.JobQueueCount, error) {
	if f.calls.Add(1) == 1 {
		return nil, errors.New("database is locked")
	}
	return f.fakeJobs.QueueCounts(ctx, since)
}

// A failed QueueCounts must not drop the dirty queue: its update is
// delivered by the next flush (issue #420).
func TestService_ObserveJob_RetriesQueueUpdateAfterQueueCountsFailure(t *testing.T) {
	jobs, decisions, kill := fixtures()
	flaky := &failFirstCounts{fakeJobs: jobs}
	svc := activityfeed.New(flaky, decisions, kill)
	svc.SetQueueUpdateInterval(10 * time.Millisecond)
	messages, cancel := svc.Subscribe()
	defer cancel()

	svc.ObserveJob(context.Background(), jobqueue.Job{ID: 1, Queue: jobqueue.JobQueueJevScout, Status: jobqueue.JobStatusPending, CreatedAt: t0})

	timeout := time.After(5 * time.Second)
	for {
		select {
		case m := <-messages:
			if m.QueueUpdate == nil {
				continue
			}
			if got := m.QueueUpdate; got.Queue != jobqueue.JobQueueJevScout || got.Pending != 3 || got.Running != 1 || got.FailedRecent != 2 {
				t.Fatalf("queue update = %+v, want jev-scout pending=3 running=1 failedRecent=2", got)
			}
			if n := flaky.calls.Load(); n != 2 {
				t.Fatalf("QueueCounts calls = %d, want 2 (one failure, one retry)", n)
			}
			return
		case <-timeout:
			t.Fatalf("no queue update after QueueCounts failure (QueueCounts calls = %d)", flaky.calls.Load())
		}
	}
}
