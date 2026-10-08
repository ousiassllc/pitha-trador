package activityfeed_test

import (
	"context"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/activityfeed"
)

// Issue #727: broker adapter notices show up in the Activity Feed as
// broker_notice events, in the snapshot (type filter included) and on the bus.
func TestService_ObserveBrokerNotice_AppearsInSnapshotAndBus(t *testing.T) {
	jobs, decisions, kill := fixtures()
	svc := activityfeed.New(jobs, decisions, kill)
	messages, cancel := svc.Subscribe()
	defer cancel()

	svc.ObserveBrokerNotice("立花証券 e支店APIへのログインが8:30までに成功していません。")

	select {
	case msg := <-messages:
		if msg.Event == nil || msg.Event.Type != domain.ActivityTypeBrokerNotice || !strings.Contains(msg.Event.Detail, "8:30") {
			t.Fatalf("bus message = %+v, want a broker_notice event with the text", msg)
		}
	default:
		t.Fatal("no bus message for the broker notice")
	}

	snap, err := svc.Snapshot(context.Background(), activityfeed.Query{Type: domain.ActivityTypeBrokerNotice})
	if err != nil || len(snap.Events) != 1 || snap.Events[0].Type != domain.ActivityTypeBrokerNotice {
		t.Fatalf("broker_notice snapshot = %+v, %v", snap.Events, err)
	}
	if all, err := svc.Snapshot(context.Background(), activityfeed.Query{}); err != nil || !hasType(all.Events, domain.ActivityTypeBrokerNotice) {
		t.Errorf("unfiltered snapshot lacks the broker_notice event: %+v, %v", all.Events, err)
	}
	if other, err := svc.Snapshot(context.Background(), activityfeed.Query{Type: domain.ActivityTypeNewsFeed}); err != nil || hasType(other.Events, domain.ActivityTypeBrokerNotice) {
		t.Errorf("news_feed-filtered snapshot contains a broker_notice event: %+v, %v", other.Events, err)
	}
}

func TestService_ObserveBrokerNotice_KeepsOnlyTheMostRecent(t *testing.T) {
	jobs, decisions, kill := fixtures()
	svc := activityfeed.New(jobs, decisions, kill)
	for range 80 {
		svc.ObserveBrokerNotice("notice")
	}
	snap, err := svc.Snapshot(context.Background(), activityfeed.Query{Type: domain.ActivityTypeBrokerNotice, Limit: 200})
	if err != nil || len(snap.Events) != 50 {
		t.Fatalf("events = %d (%v), want the 50 most recent", len(snap.Events), err)
	}
}
