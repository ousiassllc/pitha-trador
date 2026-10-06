package activityfeed_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/activityfeed"
)

// Issue #273: News Ingest failures show up in the Activity Feed as news_feed
// events, in the snapshot (type filter included) and on the live bus.
func TestService_ObserveNewsFeedError_AppearsInSnapshotAndBus(t *testing.T) {
	jobs, decisions, kill := fixtures()
	svc := activityfeed.New(jobs, decisions, kill)
	messages, cancel := svc.Subscribe()
	defer cancel()

	svc.ObserveNewsFeedError("7203", errors.New("newsfeed: yanoshin returned status 503 for \"7203\""))

	select {
	case msg := <-messages:
		if msg.Event == nil || msg.Event.Type != domain.ActivityTypeNewsFeed || msg.Event.Symbol != "7203" || !strings.Contains(msg.Event.Detail, "status 503") {
			t.Fatalf("bus message = %+v, want a news_feed event with the error", msg)
		}
	default:
		t.Fatal("no bus message for the news feed error")
	}

	snap, err := svc.Snapshot(context.Background(), activityfeed.Query{Type: domain.ActivityTypeNewsFeed})
	if err != nil || len(snap.Events) != 1 || snap.Events[0].Type != domain.ActivityTypeNewsFeed {
		t.Fatalf("news_feed snapshot = %+v, %v, want the recorded error only", snap.Events, err)
	}
	if all, err := svc.Snapshot(context.Background(), activityfeed.Query{}); err != nil || !hasType(all.Events, domain.ActivityTypeNewsFeed) {
		t.Errorf("unfiltered snapshot lacks the news_feed event: %+v, %v", all.Events, err)
	}
	if other, err := svc.Snapshot(context.Background(), activityfeed.Query{Type: domain.ActivityTypeJob}); err != nil || hasType(other.Events, domain.ActivityTypeNewsFeed) {
		t.Errorf("job-filtered snapshot contains a news_feed event: %+v, %v", other.Events, err)
	}
}

func TestService_ObserveNewsFeedError_KeepsOnlyTheMostRecentErrors(t *testing.T) {
	jobs, decisions, kill := fixtures()
	svc := activityfeed.New(jobs, decisions, kill)
	for range 120 {
		svc.ObserveNewsFeedError("7203", errors.New("down"))
	}
	snap, err := svc.Snapshot(context.Background(), activityfeed.Query{Type: domain.ActivityTypeNewsFeed, Limit: 200})
	if err != nil || len(snap.Events) != 50 {
		t.Fatalf("events = %d (%v), want the 50 most recent", len(snap.Events), err)
	}
}

func hasType(events []domain.ActivityEvent, eventType string) bool {
	for _, ev := range events {
		if ev.Type == eventType {
			return true
		}
	}
	return false
}
