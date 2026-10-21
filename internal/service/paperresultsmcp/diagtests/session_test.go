package diagtests

import (
	"database/sql"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/sessiondiag"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

var jst = time.FixedZone("JST", 9*3600)

func at(day, h, m int) time.Time { return time.Date(2026, 10, day, h, m, 0, 0, jst) }

// diagNow is the clock of the scenario of issue #850: Wednesday 2026-10-21
// 17:45 JST, when health_today reported missing_session for 10-14..10-16.
var diagNow = at(21, 17, 45)

const diagPayload = `{"instrument_id":1,"symbol":"7203"}`

func diagJob(t *testing.T, w *sql.DB, queue, status string, started, finished time.Time, lastError any) {
	t.Helper()
	testkit.MustExec(t, w, `INSERT INTO jobs (queue, payload_json, status, scheduled_at, started_at, finished_at, last_error)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, queue, diagPayload, status, testkit.StoredAt(t, started), testkit.StoredAt(t, started),
		testkit.StoredAt(t, finished), lastError)
}

func diagSnapshot(t *testing.T, w *sql.DB, ts time.Time) {
	t.Helper()
	testkit.MustExec(t, w, `INSERT INTO market_snapshots (instrument_id, symbol, timestamp, price, volume, turnover, vwap,
		price_vs_vwap_bps, raw_data_json) VALUES (1, '7203', ?, 1000, 1, 1, 1, 1, '{}')`, testkit.StoredAt(t, ts))
}

func period(from, to string) query.Range {
	f, _ := time.ParseInLocation(time.DateOnly, from, jst)
	e, _ := time.ParseInLocation(time.DateOnly, to, jst)
	return query.Range{From: f, To: e}
}

// diagDB is the database of the missing 10-14..10-16 scenario, one cause per day:
//
//	10-13 healthy: 2 snapshots, 2 market-data jobs that wrote them, a watch list of 3
//	10-14 the app was off: no jobs, no snapshot, no watch list
//	10-15 every market-data job failed (rate limit): no snapshot, a watch list of 5
//	10-16 jobs "succeeded" but wrote nothing (deferred board fetch), an empty watch list
//	10-19 one job wrote its row, one succeeded without one
func diagDB(t *testing.T) *sql.DB {
	t.Helper()
	_, w := testkit.NewMigratedDB(t)
	testkit.MustExec(t, w, `INSERT INTO instruments (id, symbol, name, market) VALUES (1, '7203', 'n', 'TSE')`)
	testkit.DetectorChecked(t, w, diagNow.Add(-time.Minute))
	testkit.HealthAlert(t, w, query.AlertKabuNotLoggedIn, diagNow.Add(-2*time.Hour), diagNow.Add(-time.Minute))

	for _, h := range []int{9, 10} {
		start := at(13, h, 0)
		diagJob(t, w, "market-data", "succeeded", start, start.Add(time.Second), nil)
		diagSnapshot(t, w, start.Add(500*time.Millisecond))
	}
	const rate = `marketdatajob: fetch board for "7203": marketdata: kabu station api error (http 429, code 4001006): SENTINEL-NAME`
	for _, h := range []int{9, 10, 11} {
		start := at(15, h, 0)
		diagJob(t, w, "market-data", "failed", start, start.Add(time.Second), rate)
	}
	diagJob(t, w, "analytics", "succeeded", at(15, 15, 40), at(15, 15, 41), nil)
	for _, h := range []int{9, 10} {
		start := at(16, h, 0)
		diagJob(t, w, "market-data", "succeeded", start, start.Add(time.Second), nil)
	}
	start := at(19, 9, 0)
	diagJob(t, w, "market-data", "succeeded", start, start.Add(time.Second), nil)
	diagSnapshot(t, w, start.Add(500*time.Millisecond))
	start = at(19, 10, 0)
	diagJob(t, w, "market-data", "succeeded", start, start.Add(time.Second), nil)
	diagJob(t, w, "jev-scout", "succeeded", at(19, 11, 0), at(19, 11, 1), nil)

	for date, n := range map[string]int{"2026-10-13": 3, "2026-10-15": 5, "2026-10-16": 0, "2026-10-19": 3} {
		source := "daily_screen"
		if n == 0 {
			source = "fixed_fallback"
		}
		testkit.MustExec(t, w, `INSERT INTO watch_lists (list_date, source, decided_at) VALUES (?, ?, ?)`,
			date, source, testkit.StoredAt(t, diagNow))
		for p := 0; p < n; p++ {
			testkit.MustExec(t, w, `INSERT INTO watch_list_entries (list_date, position, symbol, origin) VALUES (?, ?, ?, 'screen')`,
				date, p, fmt.Sprintf("%04d", 7000+p))
		}
	}
	return w
}

// The missing days of issue #850 are diagnosed one cause each from the
// database alone.
func TestMissingDays(t *testing.T) {
	w := diagDB(t)
	d, err := sessiondiag.Read(t.Context(), w, period("2026-10-13", "2026-10-22"), diagNow)
	if err != nil {
		t.Fatal(err)
	}
	days := map[string]sessiondiag.Day{}
	var dates []string
	for _, day := range d.Days {
		days[day.Date] = day
		dates = append(dates, day.Date)
	}
	// Trading days only (10-17 and 10-18 are a weekend).
	if want := []string{"2026-10-13", "2026-10-14", "2026-10-15", "2026-10-16", "2026-10-19", "2026-10-20", "2026-10-21"}; !slices.Equal(dates, want) {
		t.Fatalf("days = %v, want %v", dates, want)
	}
	num := func(p *int) string {
		if p == nil {
			return "null"
		}
		return fmt.Sprint(*p)
	}
	for _, c := range []struct {
		date string
		// snapshots, succeeded, failed, succeeded_without_snapshot, other queues
		want string
		obs  []string
		list int
	}{
		// 10-13 and 10-14 are before the 7-day cut-off of finished succeeded
		// jobs (10-14 17:45): their succeeded counts are deleted data, not 0.
		{"2026-10-13", "2/null/0/null/null", []string{}, 3},
		{"2026-10-14", "0/null/0/null/null", []string{sessiondiag.ObsNoSnapshots, sessiondiag.ObsWatchListEmpty}, 0},
		{"2026-10-15", "0/0/3/0/1", []string{sessiondiag.ObsNoSnapshots, sessiondiag.ObsAllMarketDataFailed}, 5},
		{"2026-10-16", "0/2/0/2/0", []string{sessiondiag.ObsNoSnapshots, sessiondiag.ObsSucceededWithoutSnapshot, sessiondiag.ObsWatchListEmpty, sessiondiag.ObsOtherQueuesIdle}, 0},
		{"2026-10-19", "1/2/0/1/1", []string{sessiondiag.ObsSucceededWithoutSnapshot}, 3},
		{"2026-10-20", "0/0/0/0/0", []string{sessiondiag.ObsNoSnapshots, sessiondiag.ObsNoMarketDataJobs, sessiondiag.ObsWatchListEmpty, sessiondiag.ObsOtherQueuesIdle}, 0},
	} {
		day := days[c.date]
		got := fmt.Sprintf("%s/%s/%s/%s/%s", num(day.SnapshotRows), num(day.MarketDataJob.Succeeded), num(day.MarketDataJob.Failed),
			num(day.MarketDataJob.SucceededWithoutSnapshot), num(day.OtherQueueJobsFinished))
		if got != c.want || !slices.Equal(day.Observations, c.obs) || day.WatchList.Entries != c.list {
			t.Errorf("%s = %s %v list=%d, want %s %v list=%d", c.date, got, day.Observations, day.WatchList.Entries, c.want, c.obs, c.list)
		}
	}
	if l := days["2026-10-16"].WatchList; !l.Present || l.Source == nil || *l.Source != "fixed_fallback" || l.Entries != 0 {
		t.Errorf("10-16 watch list = %+v, want a present empty fixed_fallback list", l)
	}
	if l := days["2026-10-14"].WatchList; l.Present || l.Source != nil {
		t.Errorf("10-14 watch list = %+v, want none", l)
	}
	// The kabu login state and app start of a past day are not stored: only
	// the present is, and the response says what is missing.
	// 17:45 JST is outside the watched session, so the detector reports the
	// kabu login as not_watched (and the alert it left is not active).
	if d.Current.KabuLogin != query.StatusNotWatched || !d.Current.AppRunning {
		t.Errorf("current = %+v, want kabu not_watched and the app running", d.Current)
	}
	if want := []string{"kabu_login_by_day", "app_start_by_day"}; !slices.Equal(d.Unavailable, want) {
		t.Errorf("unavailable = %v, want %v", d.Unavailable, want)
	}
	if d.Window.From != "2026-10-13" || d.Window.To != "2026-10-22" || d.Window.Truncated || d.Retention.SucceededJobDays != 7 {
		t.Errorf("window/retention = %+v %+v", d.Window, d.Retention)
	}
}

// A 366-day period is read only for its last MaxDays days.
func TestLongPeriodIsCut(t *testing.T) {
	d, err := sessiondiag.Read(t.Context(), diagDB(t), period("2025-10-22", "2026-10-22"), diagNow)
	if err != nil {
		t.Fatal(err)
	}
	if !d.Window.Truncated || d.Window.From != "2026-09-21" || d.Window.To != "2026-10-22" || d.Window.MaxDays != sessiondiag.MaxDays {
		t.Fatalf("window = %+v, want the last %d days marked truncated", d.Window, sessiondiag.MaxDays)
	}
	if first := d.Days[0].Date; first < "2026-09-21" {
		t.Errorf("first day = %s, want no day before the cut at 2026-09-21", first)
	}
}

// During the session the current kabu state comes from the detector's active
// alert, with when it began, in the same call.
func TestCurrentKabuLogin(t *testing.T) {
	now := at(21, 10, 0)
	_, w := testkit.NewMigratedDB(t)
	testkit.DetectorChecked(t, w, now.Add(-time.Minute))
	testkit.HealthAlert(t, w, query.AlertKabuNotLoggedIn, now.Add(-time.Hour), now.Add(-time.Minute))
	d, err := sessiondiag.Read(t.Context(), w, period("2026-10-21", "2026-10-22"), now)
	if err != nil {
		t.Fatal(err)
	}
	if d.Current.KabuLogin != query.KabuLoginNotLoggedIn || !d.Current.AppRunning || len(d.Current.Alerts) != 1 {
		t.Errorf("current = %+v, want kabu not_logged_in, app running, one alert", d.Current)
	}
}
