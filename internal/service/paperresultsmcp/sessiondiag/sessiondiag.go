// Package sessiondiag is session_diagnostics (issue #850): per trading day, why
// market_snapshots may be missing. It only reads, through the query.Q of a
// read-only transaction, and returns the part it has read when its soft time
// budget (package budget) runs out.
package sessiondiag

import (
	"context"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/marketcalendar"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/budget"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
	"github.com/ousiassllc/pitha-trador/internal/service/retention"
)

// reader is one Read: the statements it has run so far. A map is nil when its
// statement did not finish in time.
type reader struct {
	ctx, soft context.Context
	q         query.Q
	now       time.Time
	out       *Diagnostics

	succeeded, failed, other map[string]int
	lists                    map[string]WatchListDay
	// provider is the stored broker selection (BrokerProvider).
	provider string
}

// try reports whether the statement named item succeeded (err == nil). When
// err is the soft budget running out it records item as incomplete and
// reports false with no error; any other err is returned.
func (r *reader) try(item string, err error) (bool, error) {
	if err == nil {
		return true, nil
	}
	if !budget.Spent(r.ctx, r.soft, err) {
		return false, err
	}
	r.out.TimedOut = true
	r.out.Incomplete = append(r.out.Incomplete, item)
	return false, nil
}

// Read reads the diagnosis of every trading day in the last MaxDays days of r,
// at now. It only reads. The days are read newest first, so when the soft time
// budget runs out the oldest are the ones left incomplete (Diagnostics.TimedOut).
func Read(ctx context.Context, q query.Q, per query.Range, now time.Time) (Diagnostics, error) {
	from, window := diagnosticsFrom(per)
	out := Diagnostics{
		Window:      window,
		Days:        []Day{},
		Unavailable: []string{"kabu_login_by_day", "app_start_by_day"},
		Retention: Retention{
			SnapshotDays:     retention.DefaultSnapshotDays,
			SucceededJobDays: retention.DefaultSucceededJobDays,
			FailedJobDays:    retention.DefaultFailedJobDays,
		},
		Note:       sessionDiagnosticsNote,
		Incomplete: []string{},
	}
	soft, cancel := budget.Soft(ctx)
	defer cancel()
	r := &reader{ctx: ctx, soft: soft, q: q, now: now, out: &out}

	cur, err := query.MarketDataState(soft, q, now)
	if ok, err := r.try("current", err); err != nil {
		return Diagnostics{}, err
	} else if ok {
		out.Current = &cur
	}

	fromS, toS := bounds(from, per.To)
	var err1 error
	r.succeeded, r.failed, err1 = marketDataJobsByDay(soft, q, fromS, toS)
	if _, err := r.try("market_data_jobs", err1); err != nil {
		return Diagnostics{}, err
	}
	r.other, err1 = otherQueueJobsByDay(soft, q, fromS, toS)
	if _, err := r.try("other_queue_jobs", err1); err != nil {
		return Diagnostics{}, err
	}
	r.lists, err1 = watchListsByDay(soft, q, from.In(query.JST).Format(time.DateOnly), per.To.In(query.JST).Format(time.DateOnly))
	if _, err := r.try("watch_lists", err1); err != nil {
		return Diagnostics{}, err
	}

	r.provider = r.readProvider()
	out.BrokerProvider = r.provider

	var days []time.Time
	for day := from.In(query.JST); day.Before(per.To); day = day.AddDate(0, 0, 1) {
		if marketcalendar.TSE.IsTradingDay(day) {
			days = append(days, day)
		}
	}
	out.Days = make([]Day, len(days))
	for i := len(days) - 1; i >= 0; i-- {
		if out.Days[i], err = r.sessionDay(days[i]); err != nil {
			return Diagnostics{}, err
		}
	}
	return out, nil
}

// diagnosticsFrom cuts per to its last MaxDays days.
func diagnosticsFrom(per query.Range) (time.Time, Window) {
	from, w := per.From, Window{MaxDays: MaxDays, RequestedFrom: per.From.In(query.JST).Format(time.DateOnly)}
	if limit := per.To.AddDate(0, 0, -MaxDays); from.Before(limit) {
		from, w.Truncated = limit, true
	}
	w.From, w.To = from.In(query.JST).Format(time.DateOnly), per.To.In(query.JST).Format(time.DateOnly)
	return from, w
}

// sessionDay builds one day. A job count is known only when the day began
// after the cut-off of its retention (the purge deletes by finished_at, so an
// older day may have lost some of its rows) and its statement finished.
func (r *reader) sessionDay(day time.Time) (Day, error) {
	date := day.Format(time.DateOnly)
	kept := func(days int) bool { return !day.Before(r.now.AddDate(0, 0, -days)) }
	count := func(m map[string]int, days int) *int {
		if m == nil || !kept(days) {
			return nil
		}
		n := m[date]
		return &n
	}
	d := Day{
		Date: date,
		MarketDataJob: MarketDataJobDay{
			Succeeded: count(r.succeeded, retention.DefaultSucceededJobDays),
			Failed:    count(r.failed, retention.DefaultFailedJobDays),
		},
		OtherQueueJobsFinished: count(r.other, retention.DefaultSucceededJobDays),
		Observations:           []string{},
	}
	if r.lists != nil {
		l := r.lists[date]
		d.WatchList = &l
	}
	if kept(retention.DefaultSnapshotDays) {
		var n int
		from, to := bounds(day, day.AddDate(0, 0, 1))
		err := r.q.QueryRowContext(r.soft, `SELECT COUNT(*) FROM market_snapshots WHERE timestamp >= ? AND timestamp < ?`, from, to).Scan(&n)
		if ok, err := r.try("snapshot_rows:"+date, err); err != nil {
			return Day{}, fmt.Errorf("query: snapshot rows of %s: %w", date, err)
		} else if ok {
			d.SnapshotRows = &n
		}
	}
	if d.MarketDataJob.Succeeded != nil {
		n, err := succeededWithoutSnapshot(r.soft, r.q, day)
		if ok, err := r.try("succeeded_without_snapshot:"+date, err); err != nil {
			return Day{}, err
		} else if ok {
			d.MarketDataJob.SucceededWithoutSnapshot = &n
		}
	}
	d.Observations = observations(d, r.provider)
	return d, nil
}

func observations(d Day, provider string) []string {
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
	if o := watchListObservation(provider, d.WatchList); o != "" {
		obs = append(obs, o)
	}
	if d.OtherQueueJobsFinished != nil && *d.OtherQueueJobsFinished == 0 {
		obs = append(obs, ObsOtherQueuesIdle)
	}
	return obs
}
