package bootstrap

import (
	"context"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/jobqueue"
	"github.com/ousiassllc/pitha-trador/internal/service/activityfeed"
)

// TestBuildServices_ActivityFeedAggregatesRealRepositoriesAndReceivesLiveWrites
// guards the composition wiring of functional.md §4.15: Services.Activity
// must read the same repositories the pipeline writes, and each
// repository's write must reach `/ws/activity` subscribers.
func TestBuildServices_ActivityFeedAggregatesRealRepositoriesAndReceivesLiveWrites(t *testing.T) {
	svc := newTestServices(t)
	inst := mustCreateInstrument(t, svc, "7203")
	ctx := context.Background()

	messages, cancel := svc.Activity.Subscribe()
	defer cancel()

	if _, err := svc.Jobs.Enqueue(ctx, jobqueue.JobQueueJevScout, `{"symbol":"7203"}`, time.Now().UTC().Add(time.Hour)); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if _, err := svc.Decisions.Insert(ctx, domain.JevDecision{
		InstrumentID: inst.ID, Symbol: inst.Symbol, Timestamp: time.Now().UTC(), DecisionType: domain.JevDecisionTypeScout,
		StateHash: "h", StateJSON: "{}", QuestionVersion: "v1", ResponseJSON: "{}", ModelID: "m",
	}); err != nil {
		t.Fatalf("Insert decision: %v", err)
	}
	if _, err := svc.KillSwitch.Insert(ctx, domain.KillSwitchEvent{Reason: domain.KillReasonDailyLossLimit}); err != nil {
		t.Fatalf("Insert kill switch event: %v", err)
	}

	// Enqueue → job event + queue update; decision → jev_scout event;
	// kill switch → kill_switch event. Events are published synchronously;
	// the queue update is coalesced and arrives asynchronously after
	// activityfeed's queue-update interval, so wait for it.
	var events []string
	var update *activityfeed.QueueUpdate
	for range 4 {
		select {
		case m := <-messages:
			if m.QueueUpdate != nil {
				update = m.QueueUpdate
			} else {
				events = append(events, m.Event.Type)
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("expected 4 bus messages, got events=%v update=%v", events, update)
		}
	}
	if len(events) != 3 || events[0] != domain.ActivityTypeJob || events[1] != domain.ActivityTypeJevScout || events[2] != domain.ActivityTypeKillSwitch {
		t.Fatalf("bus events = %v, want [job jev_scout kill_switch]", events)
	}
	if update == nil || update.Queue != jobqueue.JobQueueJevScout || update.Pending != 1 {
		t.Fatalf("queue update = %+v, want jev-scout pending=1", update)
	}

	snap, err := svc.Activity.Snapshot(ctx, activityfeed.Query{})
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if got := snap.Queues[2]; got.Queue != jobqueue.JobQueueJevScout || got.Pending != 1 {
		t.Fatalf("snapshot jev-scout = %+v, want pending=1", got)
	}
	seen := map[string]bool{}
	for _, e := range snap.Events {
		seen[e.Type] = true
	}
	for _, want := range []string{domain.ActivityTypeJob, domain.ActivityTypeJevScout, domain.ActivityTypeKillSwitch} {
		if !seen[want] {
			t.Fatalf("snapshot events %+v missing type %q", snap.Events, want)
		}
	}
}
