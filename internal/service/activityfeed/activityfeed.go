// Package activityfeed aggregates the existing jobs, jev_decisions and
// kill_switch_events tables into System Activity Log's read-only feed
// (docs/requirements/functional.md §4.15, docs/architecture/overview.md
// §12): a Snapshot of per-queue depth plus a time-ordered event list, and
// an in-process event bus that pushes new events to `/ws/activity`
// subscribers. It owns no persistent state - restarting the process drops
// any undelivered bus messages and clients resume from the next Snapshot.
package activityfeed

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/jobqueue"
)

const (
	// DefaultLimit and MaxLimit are functional.md FR-ACT-3's feed size
	// bounds (a display limit only; source table retention is unaffected).
	DefaultLimit = 200
	MaxLimit     = 500

	// FailedWindow is how far back a failed job still counts toward a
	// queue's FailedRecent (functional.md FR-ACT-1 "直近failed件数").
	FailedWindow = time.Hour

	// subscriberBuffer is each subscriber's channel capacity; a subscriber
	// slower than this many pending messages misses the overflow.
	subscriberBuffer = 64

	// queueUpdateInterval is how long ObserveJob coalesces job transitions
	// before computing one set of queue-depth updates (within
	// non-functional.md §2.2's 1s live-reflection target). A 4,000-symbol
	// full scan makes thousands of transitions a minute; counting per
	// transition would put a jobs aggregation on every enqueue/claim/finish.
	queueUpdateInterval = 500 * time.Millisecond

	// queueUpdateTimeout bounds one coalesced QueueCounts call.
	queueUpdateTimeout = 5 * time.Second
)

// Queues lists the 6 job queues in pipeline order (repository job queue
// constants), the fixed row set of Snapshot.Queues.
func Queues() []string {
	return []string{
		jobqueue.JobQueueMarketData,
		jobqueue.JobQueueFeatureCalc,
		jobqueue.JobQueueJevScout,
		jobqueue.JobQueueJevTrader,
		jobqueue.JobQueueOutcomeLabeling,
		jobqueue.JobQueueAnalytics,
	}
}

// JobSource is the jobs table access Service needs
// (jobqueue.JobRepository).
type JobSource interface {
	QueueCounts(ctx context.Context, failedSince time.Time) ([]jobqueue.JobQueueCount, error)
	ListRecent(ctx context.Context, queue string, limit int) ([]jobqueue.Job, error)
}

// DecisionSource is the jev_decisions table access Service needs
// (judgement.DecisionRepository).
type DecisionSource interface {
	ListRecent(ctx context.Context, decisionType string, limit int) ([]domain.JevDecision, error)
}

// KillSwitchSource is the kill_switch_events table access Service needs
// (system.KillSwitchRepository).
type KillSwitchSource interface {
	ListRecent(ctx context.Context, limit int) ([]domain.KillSwitchEvent, error)
}

// Query filters Service.Snapshot. Zero values mean "no filter"/default.
type Query struct {
	// Limit caps Events; <= 0 means DefaultLimit, and it is clamped to
	// MaxLimit.
	Limit int
	// Queue restricts Events to job events on this jobs.queue; since only
	// job events have a queue, every other event type is excluded.
	Queue string
	// Type restricts Events to one domain.ActivityType* value.
	Type string
}

// Message is one bus notification to `/ws/activity` subscribers: exactly
// one of QueueUpdate/Event is non-nil.
type Message struct {
	QueueUpdate *QueueUpdate
	Event       *domain.ActivityEvent
}

// QueueUpdate is a queue's new depth after a job state transition.
type QueueUpdate struct {
	Queue        string
	Pending      int
	Running      int
	FailedRecent int
}

// Service builds Snapshots and fans new events out to subscribers.
type Service struct {
	jobs      JobSource
	decisions DecisionSource
	killSw    KillSwitchSource
	now       func() time.Time

	// queueUpdateInterval is queueUpdateInterval; a field so tests can
	// shorten it.
	queueUpdateInterval time.Duration

	mu   sync.Mutex
	subs map[chan Message]struct{}
	// dirtyQueues are the queues with transitions not yet reported as a
	// QueueUpdate; non-nil while a flush is scheduled (bus.go).
	dirtyQueues map[string]struct{}
}

// New returns a Service reading from the given sources.
func New(jobs JobSource, decisions DecisionSource, killSwitch KillSwitchSource) *Service {
	return &Service{
		jobs:      jobs,
		decisions: decisions,
		killSw:    killSwitch,
		now:       func() time.Time { return time.Now().UTC() },
		subs:      make(map[chan Message]struct{}),

		queueUpdateInterval: queueUpdateInterval,
	}
}

// Snapshot returns the current per-queue depth (all 6 queues, zeroes when
// idle) and the newest-first merged event list matching q.
func (s *Service) Snapshot(ctx context.Context, q Query) (domain.ActivitySnapshot, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = DefaultLimit
	}
	if limit > MaxLimit {
		limit = MaxLimit
	}

	asOf := s.now()
	queues, err := s.queueStatuses(ctx, asOf)
	if err != nil {
		return domain.ActivitySnapshot{}, err
	}

	events, err := s.collectEvents(ctx, q, limit)
	if err != nil {
		return domain.ActivitySnapshot{}, err
	}
	// Stable so equal timestamps keep collectEvents' job → scout → trader
	// → kill switch order.
	sort.SliceStable(events, func(i, j int) bool { return events[i].Timestamp.After(events[j].Timestamp) })
	if len(events) > limit {
		events = events[:limit]
	}

	return domain.ActivitySnapshot{Queues: queues, Events: events, AsOf: asOf}, nil
}

func (s *Service) queueStatuses(ctx context.Context, asOf time.Time) ([]domain.QueueStatus, error) {
	counts, err := s.jobs.QueueCounts(ctx, asOf.Add(-FailedWindow))
	if err != nil {
		return nil, fmt.Errorf("activityfeed: queue counts: %w", err)
	}
	byQueue := make(map[string]jobqueue.JobQueueCount, len(counts))
	for _, c := range counts {
		byQueue[c.Queue] = c
	}
	statuses := make([]domain.QueueStatus, 0, len(Queues()))
	for _, name := range Queues() {
		c := byQueue[name]
		statuses = append(statuses, domain.QueueStatus{
			Queue: name, Pending: c.Pending, Running: c.Running, FailedRecent: c.FailedSince,
		})
	}
	return statuses, nil
}

func (s *Service) collectEvents(ctx context.Context, q Query, limit int) ([]domain.ActivityEvent, error) {
	var events []domain.ActivityEvent

	if q.Type == "" || q.Type == domain.ActivityTypeJob {
		jobs, err := s.jobs.ListRecent(ctx, q.Queue, limit)
		if err != nil {
			return nil, fmt.Errorf("activityfeed: list jobs: %w", err)
		}
		for _, job := range jobs {
			events = append(events, jobEvent(job))
		}
	}

	// Queue filters jobs.queue only, so it excludes every other type.
	if q.Queue != "" {
		return events, nil
	}

	for _, kind := range []struct{ eventType, decisionType string }{
		{domain.ActivityTypeJevScout, domain.JevDecisionTypeScout},
		{domain.ActivityTypeJevTrader, domain.JevDecisionTypeTrader},
	} {
		if q.Type != "" && q.Type != kind.eventType {
			continue
		}
		decisions, err := s.decisions.ListRecent(ctx, kind.decisionType, limit)
		if err != nil {
			return nil, fmt.Errorf("activityfeed: list %s decisions: %w", kind.decisionType, err)
		}
		for _, d := range decisions {
			events = append(events, decisionEvent(d))
		}
	}

	if q.Type == "" || q.Type == domain.ActivityTypeKillSwitch {
		kills, err := s.killSw.ListRecent(ctx, limit)
		if err != nil {
			return nil, fmt.Errorf("activityfeed: list kill switch events: %w", err)
		}
		for _, ev := range kills {
			events = append(events, killSwitchEvent(ev))
		}
	}
	return events, nil
}
