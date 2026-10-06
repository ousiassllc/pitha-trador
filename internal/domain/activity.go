package domain

import "time"

// Activity event types (docs/api/endpoints.md §5 `GET /api/v1/activity`
// `type` query, functional.md §4.15 FR-ACT-2).
const (
	ActivityTypeJob        = "job"
	ActivityTypeJevScout   = "jev_scout"
	ActivityTypeJevTrader  = "jev_trader"
	ActivityTypeKillSwitch = "kill_switch"
	// ActivityTypeNewsFeed is a News Ingest failure (feed fetch or Luna
	// classification, issue #273). Unlike the others it is kept in memory
	// only (no table).
	ActivityTypeNewsFeed = "news_feed"
)

// ActivityEvent is one entry of System Activity Log's merged feed, derived
// from a jobs state transition, a jev_decisions row, a kill_switch_events
// row, or (in memory only) a News Ingest failure. It is never persisted
// itself (functional.md §4.15: no new tables).
type ActivityEvent struct {
	// Type is one of the ActivityType* constants.
	Type      string
	Timestamp time.Time
	// Queue is the jobs.queue of an ActivityTypeJob event, "" otherwise.
	Queue string
	// Symbol is the target instrument's symbol, "" when the event has none
	// (e.g. kill switch events, non-per-symbol jobs).
	Symbol string
	Detail string
	// LatencyMs is how long the underlying call/job took, nil when unknown
	// or not applicable.
	LatencyMs *int
}

// QueueStatus is one jobs queue's current depth (functional.md §4.15
// FR-ACT-1).
type QueueStatus struct {
	Queue   string
	Pending int
	Running int
	// FailedRecent counts jobs that failed within the activity feed's
	// recent-failure window (internal/service/activityfeed.FailedWindow).
	FailedRecent int
}

// ActivitySnapshot is the point-in-time state `GET /api/v1/activity`
// returns and System Activity Log's SSR page renders.
type ActivitySnapshot struct {
	// Queues has one entry for each of the 8 job queues, in pipeline order.
	Queues []QueueStatus
	// Events is newest first.
	Events []ActivityEvent
	AsOf   time.Time
}
