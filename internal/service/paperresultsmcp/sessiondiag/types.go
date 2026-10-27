package sessiondiag

import "github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"

// MaxDays is the longest span session_diagnostics reads: the last this-many
// JST days of the period (issue #850). A longer period (the default is 30
// days) is cut to its end and Window.Truncated says so. It bounds the per-day
// statements (one row count and, within the succeeded-job retention, one probe
// join per day); 14 days is the implementer's choice, from measurement on the
// 267,637-job synthetic database: 14 days read in about 0.64 s cold and 0.23 s
// warm, where the 31 days it was first set to took 2.2 s and 0.5 s (a quarter
// of the 3 s call budget just in the cold case on a fast host).
const MaxDays = 14

// Observation codes of a trading day, fixed strings (never free text). They
// restate what the counts show; they are leads for the cause, not a verdict.
const (
	// ObsNoSnapshots: no market_snapshots row on the day (health_today's
	// missing_session).
	ObsNoSnapshots = "no_snapshots"
	// ObsNoMarketDataJobs: no market-data job finished on the day (neither
	// succeeded nor failed): nothing was enqueued or the app was not running.
	ObsNoMarketDataJobs = "no_market_data_jobs"
	// ObsAllMarketDataFailed: market-data jobs finished, all of them failed.
	ObsAllMarketDataFailed = "all_market_data_jobs_failed"
	// ObsSucceededWithoutSnapshot: some market-data job succeeded but wrote no
	// snapshot (a board fetch deferred by the broker's rate limit returns nil).
	ObsSucceededWithoutSnapshot = "succeeded_without_snapshot"
	// ObsWatchListEmpty: no watch list was decided for the day, or it is empty,
	// with the 立花 adapter (the only writer of watch_lists) or a broker
	// selection that could not be read.
	ObsWatchListEmpty = "watch_list_empty"
	// ObsWatchListNotApplicable: the same days with kabu selected. kabu keeps
	// its watch list in memory and never writes watch_lists, so the empty table
	// says nothing about the runtime watch list (issue #914).
	ObsWatchListNotApplicable = "watch_list_not_applicable"
	// ObsOtherQueuesIdle: no job of any other queue finished on the day,
	// which is what a stopped app looks like.
	ObsOtherQueuesIdle = "other_queues_idle"
)

// Window is the span session_diagnostics covers: JST dates, To
// exclusive. Truncated is true when the requested period was longer than
// MaxDays and was cut to its last MaxDays days.
type Window struct {
	From          string `json:"from"`
	To            string `json:"to"`
	RequestedFrom string `json:"requested_from"`
	MaxDays       int    `json:"max_days"`
	Truncated     bool   `json:"truncated"`
}

// MarketDataJobDay counts the market-data jobs finished on one JST day. A
// field is null when the retention (Diagnostics.Retention) has already
// deleted the jobs it counts, so 0 never means "deleted".
type MarketDataJobDay struct {
	Succeeded *int `json:"succeeded"`
	Failed    *int `json:"failed"`
	// SucceededWithoutSnapshot are succeeded jobs with no market_snapshots row
	// for their instrument between their start and finish.
	SucceededWithoutSnapshot *int `json:"succeeded_without_snapshot"`
}

// WatchListDay is the day's watch list (watch_lists / watch_list_entries). The
// table is written by the 立花 adapter only (see ObsWatchListNotApplicable).
type WatchListDay struct {
	Present bool    `json:"present"`
	Source  *string `json:"source"`
	Entries int     `json:"entries"`
}

// Day is the diagnosis of one TSE trading day (JST).
type Day struct {
	Date string `json:"date"`
	// SnapshotRows is the market_snapshots row count; null once the snapshot
	// retention has deleted the day.
	SnapshotRows  *int             `json:"snapshot_rows"`
	MarketDataJob MarketDataJobDay `json:"market_data_jobs"`
	// OtherQueueJobsFinished counts the jobs of the other queues that finished
	// that day: the app's activity apart from market-data (null once the
	// succeeded-job retention has deleted them).
	OtherQueueJobsFinished *int `json:"other_queue_jobs_finished"`
	// WatchList is null only when it could not be read in time (Incomplete).
	WatchList    *WatchListDay `json:"watch_list"`
	Observations []string      `json:"observations"`
}

// Retention are the retention windows that make a count null.
type Retention struct {
	SnapshotDays     int `json:"snapshot_days"`
	SucceededJobDays int `json:"succeeded_job_days"`
	FailedJobDays    int `json:"failed_job_days"`
}

// Diagnostics is session_diagnostics' data.
type Diagnostics struct {
	Window Window `json:"window"`
	// Days are the trading days of the window, oldest first.
	Days []Day `json:"days"`
	// BrokerProvider is the stored broker selection (runtime_settings
	// broker.provider; "kabu" when none is stored, "tachibana", or
	// ProviderUnknown when it could not be read). The app reads it once at
	// start, so after a change it names the broker of the next start.
	BrokerProvider string `json:"broker_provider"`
	// Current is the kabu login / app state now (from the health alerts and
	// the detector's last check); it says nothing about the days above.
	// Current is null only when it could not be read in time (Incomplete).
	Current *query.HealthMarketData `json:"current"`
	// Unavailable lists what the database does not keep per day, so its
	// absence from Days is not a finding.
	Unavailable []string  `json:"unavailable"`
	Retention   Retention `json:"retention"`
	Note        string    `json:"note"`
	// TimedOut is true when the soft time budget (package budget) ran out: the
	// items listed in Incomplete were not read, and the fields they feed are
	// null (a null is then "not read", not "deleted by the retention").
	// Incomplete names them: "current", "market_data_jobs", "other_queue_jobs",
	// "watch_lists", "snapshot_rows:<JST date>" and
	// "succeeded_without_snapshot:<JST date>" and "broker_provider". Both are empty/false when
	// everything was read.
	TimedOut   bool     `json:"timed_out"`
	Incomplete []string `json:"incomplete"`
}

const sessionDiagnosticsNote = "Counts come from the database only. The kabu login state and app starts of past days are not stored " +
	"(health_alerts holds the alerts active now, health_alert_checks only the last check): see current, and read " +
	"other_queue_jobs_finished and market_data_jobs as indirect signs of whether the app ran. A null count was deleted by the retention, unless timed_out is true and incomplete names it."

// jstDay is the JST date of a stored finished_at, as an SQL expression.
const jstDay = "substr(datetime(finished_at, '+9 hours'), 1, 10)"
