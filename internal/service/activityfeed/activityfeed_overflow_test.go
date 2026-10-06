package activityfeed_test

import (
	"context"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/jobqueue"
	"github.com/ousiassllc/pitha-trador/internal/service/activityfeed"
)

func burstJobs(svc *activityfeed.Service, n int) {
	for i := range n {
		svc.ObserveJob(context.Background(), jobqueue.Job{
			ID: int64(i), Queue: jobqueue.JobQueueMarketData, Status: jobqueue.JobStatusPending, CreatedAt: t0,
		})
	}
}

// Issue #536: a full scan's job burst fills a slow subscriber's job quota,
// but kill switch and decision events still reach it, and the dropped job
// messages are reported with a Resync instead of vanishing silently.
func TestService_JobBurst_DoesNotCrowdOutPriorityEvents(t *testing.T) {
	jobs, decisions, kill := fixtures()
	svc := activityfeed.New(jobs, decisions, kill)
	svc.SetQueueUpdateInterval(20 * time.Millisecond)
	messages, cancel := svc.Subscribe() // not read during the burst
	defer cancel()

	burstJobs(svc, 4000)
	svc.ObserveKillSwitch(context.Background(), domain.KillSwitchEvent{TriggeredAt: t0, Reason: domain.KillReasonDailyLossLimit})
	svc.ObserveDecision(context.Background(), domain.JevDecision{Symbol: "7203", Timestamp: t0, DecisionType: domain.JevDecisionTypeTrader})

	// Let the coalescing flush run while the subscriber is still behind.
	time.Sleep(100 * time.Millisecond)

	var gotKill, gotDecision, gotResync bool
	deadline := time.After(5 * time.Second)
	for !gotKill || !gotDecision || !gotResync {
		select {
		case m, ok := <-messages:
			if !ok {
				t.Fatalf("subscriber closed (kill=%v decision=%v resync=%v)", gotKill, gotDecision, gotResync)
			}
			switch {
			case m.Resync:
				gotResync = true
			case m.Event != nil && m.Event.Type == domain.ActivityTypeKillSwitch:
				gotKill = true
			case m.Event != nil && m.Event.Type == domain.ActivityTypeJevTrader:
				gotDecision = true
			}
		case <-deadline:
			t.Fatalf("after 4000 job events: kill switch=%v decision=%v resync=%v, want all true", gotKill, gotDecision, gotResync)
		}
	}
}

// The Resync is still delivered when job traffic has stopped and the
// subscriber drains late (the flush keeps retrying while it is lagged).
func TestService_Resync_DeliveredAfterSlowSubscriberDrains(t *testing.T) {
	jobs, decisions, kill := fixtures()
	svc := activityfeed.New(jobs, decisions, kill)
	svc.SetQueueUpdateInterval(10 * time.Millisecond)
	messages, cancel := svc.Subscribe()
	defer cancel()

	burstJobs(svc, 500)
	time.Sleep(100 * time.Millisecond) // flush runs; buffer still full -> still lagged

	deadline := time.After(5 * time.Second)
	for {
		select {
		case m := <-messages:
			if m.Resync {
				return
			}
		case <-deadline:
			t.Fatal("no Resync after the subscriber drained")
		}
	}
}

// A subscriber too slow even for the priority headroom is closed (so its
// WebSocket ends and the client re-fetches) rather than losing a kill
// switch event silently.
func TestService_PriorityOverflow_ClosesSubscriber(t *testing.T) {
	jobs, decisions, kill := fixtures()
	svc := activityfeed.New(jobs, decisions, kill)
	messages, cancel := svc.Subscribe()
	defer cancel()

	for range 1000 {
		svc.ObserveKillSwitch(context.Background(), domain.KillSwitchEvent{Reason: domain.KillReasonDailyLossLimit})
	}
	n := 0
	for range messages { // ends only if the bus closed the channel
		n++
	}
	if n == 0 {
		t.Fatal("subscriber received nothing before being closed")
	}
}
