// Package sessiondiag is session_diagnostics (issue #850): per trading day, why
// market_snapshots may be missing. It only reads, through the query.Q of a
// read-only transaction.
package sessiondiag

import (
	"context"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/marketcalendar"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
	"github.com/ousiassllc/pitha-trador/internal/service/retention"
)

// MaxDays is the longest span session_diagnostics reads:
// the last this-many JST days of the period (issue #850). The default 30-day
// period fits; a 366-day one is cut to its end. It bounds the per-day
// statements (one row count and, within the succeeded-job retention, one
// probe join per day).
const MaxDays = 31

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
	// ObsWatchListEmpty: no watch list was decided for the day, or it is empty.
	ObsWatchListEmpty = "watch_list_empty"
	// ObsOtherQueuesIdle: no job of any other queue finished on the day,
	// which is what a stopped app looks like.
	ObsOtherQueuesIdle = "other_queues_idle"
)

// Window is the span session_diagnostics covers: JST dates, To
// exclusive. Truncated is true when the requested period was longer than
// MaxDays and was cut to its last MaxDays days.
type Window struct {
	From      string `json:"from"`
	To        string `json:"to"`
	MaxDays   int    `json:"max_days"`
	Truncated bool   `json:"truncated"`
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

// WatchListDay is the day's watch list (watch_lists / watch_list_entries).
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
	OtherQueueJobsFinished *int         `json:"other_queue_jobs_finished"`
	WatchList              WatchListDay `json:"watch_list"`
	Observations           []string     `json:"observations"`
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
	// Current is the kabu login / app state now (from the health alerts and
	// the detector's last check); it says nothing about the days above.
	Current query.HealthMarketData `json:"current"`
	// Unavailable lists what the database does not keep per day, so its
	// absence from Days is not a finding.
	Unavailable []string  `json:"unavailable"`
	Retention   Retention `json:"retention"`
	Note        string    `json:"note"`
}

const sessionDiagnosticsNote = "Counts come from the database only. The kabu login state and app starts of past days are not stored " +
	"(health_alerts holds the alerts active now, health_alert_checks only the last check): see current, and read " +
	"other_queue_jobs_finished and market_data_jobs as indirect signs of whether the app ran. A null count was deleted by the retention."

// jstDay is the JST date of a stored finished_at, as an SQL expression.
const jstDay = "substr(datetime(finished_at, '+9 hours'), 1, 10)"

// Read reads the diagnosis of every trading day in the last
// MaxDays days of r, at now. It only reads.
func Read(ctx context.Context, q query.Q, r query.Range, now time.Time) (Diagnostics, error) {
	from, window := diagnosticsFrom(r)
	out := Diagnostics{
		Window:      window,
		Days:        []Day{},
		Unavailable: []string{"kabu_login_by_day", "app_start_by_day"},
		Retention: Retention{
			SnapshotDays:     retention.DefaultSnapshotDays,
			SucceededJobDays: retention.DefaultSucceededJobDays,
			FailedJobDays:    retention.DefaultFailedJobDays,
		},
		Note: sessionDiagnosticsNote,
	}
	var err error
	if out.Current, err = query.MarketDataState(ctx, q, now); err != nil {
		return Diagnostics{}, err
	}

	fromS, toS := bounds(from, r.To)
	succeeded, failed, err := marketDataJobsByDay(ctx, q, fromS, toS)
	if err != nil {
		return Diagnostics{}, err
	}
	other, err := otherQueueJobsByDay(ctx, q, fromS, toS)
	if err != nil {
		return Diagnostics{}, err
	}
	lists, err := watchListsByDay(ctx, q, from.In(query.JST).Format(time.DateOnly), r.To.In(query.JST).Format(time.DateOnly))
	if err != nil {
		return Diagnostics{}, err
	}

	for day := from.In(query.JST); day.Before(r.To); day = day.AddDate(0, 0, 1) {
		if !marketcalendar.TSE.IsTradingDay(day) {
			continue
		}
		d, err := sessionDay(ctx, q, day, now, succeeded, failed, other, lists)
		if err != nil {
			return Diagnostics{}, err
		}
		out.Days = append(out.Days, d)
	}
	return out, nil
}

// diagnosticsFrom cuts r to its last MaxDays days.
func diagnosticsFrom(r query.Range) (time.Time, Window) {
	from, w := r.From, Window{MaxDays: MaxDays}
	if limit := r.To.AddDate(0, 0, -MaxDays); from.Before(limit) {
		from, w.Truncated = limit, true
	}
	w.From, w.To = from.In(query.JST).Format(time.DateOnly), r.To.In(query.JST).Format(time.DateOnly)
	return from, w
}

// sessionDay builds one day. A job count is known only when the day began
// after the cut-off of its retention: the purge deletes by finished_at, so an
// older day may have lost some of its rows.
func sessionDay(ctx context.Context, q query.Q, day, now time.Time,
	succeeded, failed, other map[string]int, lists map[string]WatchListDay) (Day, error) {
	date := day.Format(time.DateOnly)
	kept := func(days int) bool { return !day.Before(now.AddDate(0, 0, -days)) }
	count := func(m map[string]int, days int) *int {
		if !kept(days) {
			return nil
		}
		n := m[date]
		return &n
	}
	d := Day{
		Date: date,
		MarketDataJob: MarketDataJobDay{
			Succeeded: count(succeeded, retention.DefaultSucceededJobDays),
			Failed:    count(failed, retention.DefaultFailedJobDays),
		},
		OtherQueueJobsFinished: count(other, retention.DefaultSucceededJobDays),
		WatchList:              lists[date],
		Observations:           []string{},
	}
	if kept(retention.DefaultSnapshotDays) {
		var n int
		from, to := bounds(day, day.AddDate(0, 0, 1))
		err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM market_snapshots WHERE timestamp >= ? AND timestamp < ?`, from, to).Scan(&n)
		if err != nil {
			return Day{}, fmt.Errorf("query: snapshot rows of %s: %w", date, err)
		}
		d.SnapshotRows = &n
	}
	if d.MarketDataJob.Succeeded != nil {
		n, err := succeededWithoutSnapshot(ctx, q, day)
		if err != nil {
			return Day{}, err
		}
		d.MarketDataJob.SucceededWithoutSnapshot = &n
	}
	d.Observations = observations(d)
	return d, nil
}

func observations(d Day) []string {
	obs := []string{}
	if d.SnapshotRows != nil && *d.SnapshotRows == 0 {
		obs = append(obs, ObsNoSnapshots)
	}
	j := d.MarketDataJob
	if j.Succeeded != nil && j.Failed != nil {
		switch {
		case *j.Succeeded == 0 && *j.Failed == 0:
			obs = append(obs, ObsNoMarketDataJobs)
		case *j.Succeeded == 0:
			obs = append(obs, ObsAllMarketDataFailed)
		}
	}
	if j.SucceededWithoutSnapshot != nil && *j.SucceededWithoutSnapshot > 0 {
		obs = append(obs, ObsSucceededWithoutSnapshot)
	}
	if d.WatchList.Entries == 0 {
		obs = append(obs, ObsWatchListEmpty)
	}
	if d.OtherQueueJobsFinished != nil && *d.OtherQueueJobsFinished == 0 {
		obs = append(obs, ObsOtherQueuesIdle)
	}
	return obs
}
