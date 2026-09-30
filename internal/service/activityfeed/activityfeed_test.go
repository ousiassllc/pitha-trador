package activityfeed_test

import (
	"context"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/jobqueue"
	"github.com/ousiassllc/pitha-trador/internal/service/activityfeed"
)

type fakeJobs struct {
	counts    []jobqueue.JobQueueCount
	jobs      []jobqueue.Job
	gotQueue  string
	gotLimit  int
	listCalls int
}

func (f *fakeJobs) QueueCounts(context.Context, time.Time) ([]jobqueue.JobQueueCount, error) {
	return f.counts, nil
}

func (f *fakeJobs) ListRecent(_ context.Context, queue string, limit int) ([]jobqueue.Job, error) {
	f.listCalls++
	f.gotQueue, f.gotLimit = queue, limit
	return f.jobs, nil
}

type fakeDecisions struct {
	byType map[string][]domain.JevDecision
	calls  int
}

func (f *fakeDecisions) ListRecent(_ context.Context, decisionType string, _ int) ([]domain.JevDecision, error) {
	f.calls++
	return f.byType[decisionType], nil
}

type fakeKillSwitch struct {
	events []domain.KillSwitchEvent
	calls  int
}

func (f *fakeKillSwitch) ListRecent(context.Context, int) ([]domain.KillSwitchEvent, error) {
	f.calls++
	return f.events, nil
}

var t0 = time.Date(2026, 9, 29, 1, 0, 0, 0, time.UTC)

func fixtures() (*fakeJobs, *fakeDecisions, *fakeKillSwitch) {
	started, finished := t0.Add(2*time.Minute), t0.Add(2*time.Minute+1500*time.Millisecond)
	dir, conf := domain.JevDirectionLong, 0.74
	return &fakeJobs{
			counts: []jobqueue.JobQueueCount{{Queue: jobqueue.JobQueueJevScout, Pending: 3, Running: 1, FailedSince: 2}},
			jobs: []jobqueue.Job{{
				ID: 1, Queue: jobqueue.JobQueueMarketData, PayloadJSON: `{"symbol":"7203"}`,
				Status: jobqueue.JobStatusSucceeded, Attempts: 1, CreatedAt: t0, StartedAt: &started, FinishedAt: &finished,
			}},
		}, &fakeDecisions{byType: map[string][]domain.JevDecision{
			domain.JevDecisionTypeScout:  {{Symbol: "6758", Timestamp: t0.Add(time.Minute), DecisionType: domain.JevDecisionTypeScout, QuestionVersion: "scout-v1", LatencyMs: 300}},
			domain.JevDecisionTypeTrader: {{Symbol: "7203", Timestamp: t0.Add(3 * time.Minute), DecisionType: domain.JevDecisionTypeTrader, Direction: &dir, Confidence: &conf, LatencyMs: 820}},
		}}, &fakeKillSwitch{events: []domain.KillSwitchEvent{{ID: 1, TriggeredAt: t0.Add(4 * time.Minute), Reason: domain.KillReasonDailyLossLimit}}}
}

func TestService_Snapshot_ReportsAllSixQueuesInPipelineOrder(t *testing.T) {
	jobs, decisions, kill := fixtures()
	snap, err := activityfeed.New(jobs, decisions, kill).Snapshot(context.Background(), activityfeed.Query{})
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	want := []string{"market-data", "feature-calc", "jev-scout", "jev-trader", "outcome-labeling", "analytics"}
	if len(snap.Queues) != len(want) {
		t.Fatalf("queues = %+v, want %d entries", snap.Queues, len(want))
	}
	for i, name := range want {
		if snap.Queues[i].Queue != name {
			t.Fatalf("queues[%d] = %q, want %q", i, snap.Queues[i].Queue, name)
		}
	}
	scout := snap.Queues[2]
	if scout.Pending != 3 || scout.Running != 1 || scout.FailedRecent != 2 {
		t.Fatalf("jev-scout = %+v, want pending=3 running=1 failedRecent=2", scout)
	}
	if idle := snap.Queues[5]; idle.Pending != 0 || idle.Running != 0 || idle.FailedRecent != 0 {
		t.Fatalf("analytics (no rows) = %+v, want zeroes", idle)
	}
}

func TestService_Snapshot_MergesSourcesNewestFirst(t *testing.T) {
	jobs, decisions, kill := fixtures()
	snap, err := activityfeed.New(jobs, decisions, kill).Snapshot(context.Background(), activityfeed.Query{})
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	wantTypes := []string{domain.ActivityTypeKillSwitch, domain.ActivityTypeJevTrader, domain.ActivityTypeJob, domain.ActivityTypeJevScout}
	if len(snap.Events) != len(wantTypes) {
		t.Fatalf("events = %+v, want %d", snap.Events, len(wantTypes))
	}
	for i, want := range wantTypes {
		if snap.Events[i].Type != want {
			t.Fatalf("events[%d].Type = %q, want %q (full: %+v)", i, snap.Events[i].Type, want, snap.Events)
		}
	}

	trader := snap.Events[1]
	if trader.Symbol != "7203" || trader.Detail != "direction=LONG confidence=0.74" || trader.LatencyMs == nil || *trader.LatencyMs != 820 {
		t.Fatalf("trader event = %+v", trader)
	}
	job := snap.Events[2]
	if job.Queue != jobqueue.JobQueueMarketData || job.Symbol != "7203" || job.LatencyMs == nil || *job.LatencyMs != 1500 {
		t.Fatalf("job event = %+v, want queue/symbol from the row and latency = finished-started", job)
	}
	if kill := snap.Events[0]; kill.Detail != "reason=daily_loss_limit" || kill.Symbol != "" || kill.LatencyMs != nil {
		t.Fatalf("kill switch event = %+v", kill)
	}
}

func TestService_Snapshot_TypeFilterQueriesOnlyThatSource(t *testing.T) {
	jobs, decisions, kill := fixtures()
	svc := activityfeed.New(jobs, decisions, kill)

	snap, err := svc.Snapshot(context.Background(), activityfeed.Query{Type: domain.ActivityTypeJevScout})
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if len(snap.Events) != 1 || snap.Events[0].Type != domain.ActivityTypeJevScout || snap.Events[0].Symbol != "6758" {
		t.Fatalf("events = %+v, want only the scout decision", snap.Events)
	}
	if jobs.listCalls != 0 || kill.calls != 0 {
		t.Fatalf("job/kill switch sources queried for a jev_scout filter (jobs=%d kill=%d)", jobs.listCalls, kill.calls)
	}
}

func TestService_Snapshot_QueueFilterReturnsOnlyJobsOnThatQueue(t *testing.T) {
	jobs, decisions, kill := fixtures()
	snap, err := activityfeed.New(jobs, decisions, kill).Snapshot(context.Background(), activityfeed.Query{Queue: jobqueue.JobQueueMarketData})
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if jobs.gotQueue != jobqueue.JobQueueMarketData {
		t.Fatalf("job source queried with queue %q, want market-data", jobs.gotQueue)
	}
	if len(snap.Events) != 1 || snap.Events[0].Type != domain.ActivityTypeJob {
		t.Fatalf("events = %+v, want only the job event", snap.Events)
	}
	if decisions.calls != 0 || kill.calls != 0 {
		t.Fatalf("non-job sources queried for a queue filter (decisions=%d kill=%d)", decisions.calls, kill.calls)
	}

	// A queue filter combined with a non-job type can match nothing.
	snap, err = activityfeed.New(jobs, decisions, kill).Snapshot(context.Background(),
		activityfeed.Query{Queue: jobqueue.JobQueueMarketData, Type: domain.ActivityTypeKillSwitch})
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if len(snap.Events) != 0 {
		t.Fatalf("events = %+v, want none for queue+kill_switch", snap.Events)
	}
}

func TestService_Snapshot_LimitDefaultsAndClampsToMax(t *testing.T) {
	for _, tc := range []struct{ limit, want int }{
		{0, activityfeed.DefaultLimit},
		{-5, activityfeed.DefaultLimit},
		{10, 10},
		{100000, activityfeed.MaxLimit},
	} {
		jobs, decisions, kill := fixtures()
		if _, err := activityfeed.New(jobs, decisions, kill).Snapshot(context.Background(), activityfeed.Query{Limit: tc.limit}); err != nil {
			t.Fatalf("Snapshot(limit=%d): %v", tc.limit, err)
		}
		if jobs.gotLimit != tc.want {
			t.Fatalf("Snapshot(limit=%d) queried sources with limit %d, want %d", tc.limit, jobs.gotLimit, tc.want)
		}
	}
}

func TestService_Snapshot_TruncatesMergedListToLimit(t *testing.T) {
	jobs, decisions, kill := fixtures()
	snap, err := activityfeed.New(jobs, decisions, kill).Snapshot(context.Background(), activityfeed.Query{Limit: 2})
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if len(snap.Events) != 2 || snap.Events[0].Type != domain.ActivityTypeKillSwitch || snap.Events[1].Type != domain.ActivityTypeJevTrader {
		t.Fatalf("events = %+v, want the 2 newest (kill_switch, jev_trader)", snap.Events)
	}
}

func TestService_Observers_PublishToSubscribersOnly(t *testing.T) {
	jobs, decisions, kill := fixtures()
	svc := activityfeed.New(jobs, decisions, kill)

	// No subscriber: observers are no-ops (must not panic or block).
	svc.ObserveKillSwitch(context.Background(), domain.KillSwitchEvent{Reason: domain.KillReasonDailyLossLimit})

	messages, cancel := svc.Subscribe()
	defer cancel()

	svc.ObserveKillSwitch(context.Background(), domain.KillSwitchEvent{TriggeredAt: t0, Reason: domain.KillReasonDailyLossLimit})
	svc.ObserveDecision(context.Background(), domain.JevDecision{Symbol: "7203", Timestamp: t0, DecisionType: domain.JevDecisionTypeScout})
	svc.ObserveJob(context.Background(), jobqueue.Job{ID: 9, Queue: jobqueue.JobQueueJevScout, Status: jobqueue.JobStatusRunning, CreatedAt: t0})

	next := func() activityfeed.Message {
		t.Helper()
		select {
		case m := <-messages:
			return m
		default:
			t.Fatalf("expected a queued message, channel empty")
			return activityfeed.Message{}
		}
	}
	if m := next(); m.Event == nil || m.Event.Type != domain.ActivityTypeKillSwitch {
		t.Fatalf("message 1 = %+v, want kill_switch event", m)
	}
	if m := next(); m.Event == nil || m.Event.Type != domain.ActivityTypeJevScout || m.Event.Symbol != "7203" {
		t.Fatalf("message 2 = %+v, want jev_scout event for 7203", m)
	}
	if m := next(); m.Event == nil || m.Event.Type != domain.ActivityTypeJob || m.Event.Queue != jobqueue.JobQueueJevScout {
		t.Fatalf("message 3 = %+v, want job event on jev-scout", m)
	}
	m := next()
	if m.QueueUpdate == nil || m.QueueUpdate.Queue != jobqueue.JobQueueJevScout ||
		m.QueueUpdate.Pending != 3 || m.QueueUpdate.Running != 1 || m.QueueUpdate.FailedRecent != 2 {
		t.Fatalf("message 4 = %+v, want jev-scout queue update pending=3 running=1 failedRecent=2", m)
	}
}

func TestService_Publish_SlowSubscriberNeverBlocksWriter(t *testing.T) {
	jobs, decisions, kill := fixtures()
	svc := activityfeed.New(jobs, decisions, kill)
	_, cancel := svc.Subscribe() // never drained
	defer cancel()

	done := make(chan struct{})
	go func() {
		for range 1000 {
			svc.ObserveKillSwitch(context.Background(), domain.KillSwitchEvent{Reason: domain.KillReasonDailyLossLimit})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("observer blocked on a full subscriber channel")
	}
}

func TestService_Subscribe_CancelClosesChannelAndStopsDelivery(t *testing.T) {
	jobs, decisions, kill := fixtures()
	svc := activityfeed.New(jobs, decisions, kill)
	messages, cancel := svc.Subscribe()
	cancel()

	if _, ok := <-messages; ok {
		t.Fatalf("channel still open after cancel")
	}
	// Publishing after cancel must not panic (send on closed channel).
	svc.ObserveKillSwitch(context.Background(), domain.KillSwitchEvent{Reason: domain.KillReasonDailyLossLimit})
}
