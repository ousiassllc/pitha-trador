package diagtests

import (
	"slices"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/sessiondiag"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/sessionrecord"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
	"github.com/ousiassllc/pitha-trador/internal/service/retention"
)

// Issue #1054: the daily record of a day outlives the succeeded-job retention,
// so collection_status (a 30-day period, far beyond the 7 days of jobs) and
// session_diagnostics still show the gap kind, the codes and the warning codes
// of the time after the retention has purged the jobs.
func TestDailyRecordOutlivesTheJobRetention(t *testing.T) {
	_, w := testkit.NewMigratedDB(t)
	now := at(30, 12, 0)
	// 10-09 (Friday) and 10-22: the days of the report, recorded at the time
	// with the alert codes that were seen; the jobs have long been purged.
	testkit.MustExec(t, w, `INSERT INTO session_diagnostics_daily (session_date, gap_kind, gap_codes_json, alert_codes_json, watch_checks, recorded_at)
		VALUES ('2026-10-09', 'whole_day', '[]', '[]', 0, ?)`, testkit.StoredAt(t, at(9, 15, 40)))
	testkit.MustExec(t, w, `INSERT INTO session_diagnostics_daily (session_date, gap_kind, gap_codes_json, gaps_json, alert_codes_json,
		watch_checks, snapshot_rows, snapshot_minutes, first_snapshot_at, last_snapshot_at, longest_gap_minutes,
		market_data_jobs_succeeded, market_data_jobs_failed, recorded_at)
		VALUES ('2026-10-22', 'intraday', '["morning_missing"]', '[{"code":"morning_missing","from":"2026-10-22T00:00:00Z","to":"2026-10-22T02:30:00Z","minutes":150}]',
		'["kabu_unreachable"]', 180, 180, 180, ?, ?, 150, 180, 2, ?)`,
		testkit.StoredAt(t, at(22, 12, 30)), testkit.StoredAt(t, at(22, 15, 29)), testkit.StoredAt(t, at(22, 15, 36)))
	testkit.MustExec(t, w, `INSERT INTO session_diagnostics_daily (session_date, gap_kind, recorded_at) VALUES ('2026-10-29', 'in_progress', ?)`,
		testkit.StoredAt(t, at(29, 10, 0)))
	testkit.MustExec(t, w, `INSERT INTO session_diagnostics_daily (session_date, gap_kind, recorded_at) VALUES ('2026-10-02', 'insufficient_history', ?)`,
		testkit.StoredAt(t, at(2, 15, 40)))

	// A succeeded job older than the retention window is deleted by the purge.
	old := testkit.StoredAt(t, now.AddDate(0, 0, -(retention.DefaultSucceededJobDays+2)))
	testkit.MustExec(t, w, `INSERT INTO jobs (queue, payload_json, status, scheduled_at, finished_at, created_at) VALUES ('market-data', '{}', 'succeeded', ?, ?, ?)`, old, old, old)
	if err := retention.New(w, retention.Policy{}).Purge(t.Context()); err != nil {
		t.Fatalf("Purge: %v", err)
	}
	var jobs int
	if err := w.QueryRow(`SELECT COUNT(*) FROM jobs`).Scan(&jobs); err != nil || jobs != 0 {
		t.Fatalf("jobs after the purge = %d (%v), want 0", jobs, err)
	}

	c, err := query.CollectionStatus(t.Context(), w, period("2026-10-01", "2026-10-30"), now)
	if err != nil {
		t.Fatalf("CollectionStatus: %v", err)
	}
	g := c.SessionGaps
	if !slices.Equal(g.WholeDayDates, []string{"2026-10-09"}) || !slices.Equal(g.IntradayDates, []string{"2026-10-22"}) ||
		!slices.Equal(g.InsufficientHistoryDates, []string{"2026-10-02"}) {
		t.Fatalf("session_gaps = whole %v, intraday %v, insufficient %v; want 10-09 / 10-22 / 10-02", g.WholeDayDates, g.IntradayDates, g.InsufficientHistoryDates)
	}
	if len(g.Days) != 3 {
		t.Fatalf("days = %d, want 3 (the in-progress row is not a verdict)", len(g.Days))
	}
	var intraday sessionrecord.Diagnostic
	for _, d := range g.Days {
		if d.Date == "2026-10-22" {
			intraday = d
		}
	}
	if !slices.Equal(intraday.Codes, []string{"morning_missing"}) || !slices.Equal(intraday.AlertCodes, []string{"kabu_unreachable"}) ||
		intraday.WatchChecks != 180 || intraday.LongestGapMinutes != 150 || len(intraday.Gaps) != 1 || intraday.Gaps[0].Minutes != 150 ||
		intraday.JobsSucceeded == nil || *intraday.JobsSucceeded != 180 {
		t.Errorf("record of 10-22 = %+v", intraday)
	}

	d, err := sessiondiag.Read(t.Context(), w, period("2026-10-20", "2026-10-30"), now)
	if err != nil {
		t.Fatalf("session_diagnostics: %v", err)
	}
	var found *sessiondiag.Day
	for i := range d.Days {
		if d.Days[i].Date == "2026-10-22" {
			found = &d.Days[i]
		}
	}
	if found == nil || found.Recorded == nil || found.Recorded.GapKind != "intraday" || !slices.Contains(found.Observations, sessiondiag.ObsIntradayGap) {
		t.Fatalf("diagnostics of 10-22 = %+v, want the record and the intraday_gap observation", found)
	}
	if found.MarketDataJob.Succeeded != nil {
		t.Errorf("the purged succeeded count is %d, want null (the record holds it instead)", *found.MarketDataJob.Succeeded)
	}
	for _, day := range d.Days {
		if day.Date == "2026-10-29" && day.Recorded != nil {
			t.Errorf("an in-progress row was returned as a record: %+v", day.Recorded)
		}
	}
}

// Whatever the retention does to snapshots and jobs, the record carries no
// price: only counts, times and fixed codes.
func TestDailyRecordColumnsHoldNoPrices(t *testing.T) {
	_, w := testkit.NewMigratedDB(t)
	rows, err := w.QueryContext(t.Context(), `SELECT name FROM pragma_table_info('session_diagnostics_daily')`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		for _, banned := range []string{"price", "bid", "ask", "vwap", "turnover", "raw"} {
			if strings.Contains(name, banned) {
				t.Errorf("column %q looks like market data", name)
			}
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}
