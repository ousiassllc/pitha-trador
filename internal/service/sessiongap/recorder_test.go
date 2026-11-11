package sessiongap_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
	"github.com/ousiassllc/pitha-trador/internal/service/marketcalendar"
	"github.com/ousiassllc/pitha-trador/internal/service/retention"
	"github.com/ousiassllc/pitha-trador/internal/service/sessiongap"
)

func newDB(t *testing.T) *sql.DB {
	t.Helper()
	conn, err := sqlitedb.Open(filepath.Join(t.TempDir(), "pitha.db"))
	if err != nil {
		t.Fatalf("sqlitedb.Open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if _, err := conn.Exec(`INSERT INTO instruments (id, symbol, name, market) VALUES (1, '7203', 'n', 'TSE')`); err != nil {
		t.Fatalf("instrument: %v", err)
	}
	return conn
}

// snapshots stores one snapshot per given minute.
func snapshots(t *testing.T, conn *sql.DB, at ...[]time.Time) {
	t.Helper()
	for _, list := range at {
		for _, ts := range list {
			_, err := conn.Exec(`INSERT INTO market_snapshots (instrument_id, symbol, timestamp, price, bid, ask, volume,
				turnover, vwap, price_vs_vwap_bps, raw_data_json) VALUES (1, '7203', ?, 1, 1, 1, 1, 1, 1, 1, '{}')`, sqlutil.FormatTime(ts))
			if err != nil {
				t.Fatalf("snapshot: %v", err)
			}
		}
	}
}

func fullDay(date string) [][]time.Time {
	return [][]time.Time{minutes(date, "09:00", "11:30"), minutes(date, "12:30", "15:30")}
}

type row struct {
	kind               string
	codes, alerts      []string
	checks, rows, mins int
	first, last        sql.NullString
	jobsOK, jobsFailed sql.NullInt64
	longest            int
}

func readRow(t *testing.T, conn *sql.DB, date string) (row, bool) {
	t.Helper()
	var r row
	var codes, alerts string
	err := conn.QueryRow(`SELECT gap_kind, gap_codes_json, alert_codes_json, watch_checks, snapshot_rows, snapshot_minutes,
		first_snapshot_at, last_snapshot_at, longest_gap_minutes, market_data_jobs_succeeded, market_data_jobs_failed
		FROM session_diagnostics_daily WHERE session_date = ?`, date).
		Scan(&r.kind, &codes, &alerts, &r.checks, &r.rows, &r.mins, &r.first, &r.last, &r.longest, &r.jobsOK, &r.jobsFailed)
	if errors.Is(err, sql.ErrNoRows) {
		return row{}, false
	}
	if err != nil {
		t.Fatalf("read %s: %v", date, err)
	}
	if err := json.Unmarshal([]byte(codes), &r.codes); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(alerts), &r.alerts); err != nil {
		t.Fatal(err)
	}
	return r, true
}

type clock struct{ now time.Time }

func (c *clock) Now() time.Time { return c.now }

func recorder(conn *sql.DB, c *clock, alerts func() []string) *sessiongap.Recorder {
	return sessiongap.NewRecorder(sessiongap.Config{
		Store: sessiongap.NewDBStore(conn),
		Alerts: func(context.Context) ([]string, error) {
			if alerts == nil {
				return nil, nil
			}
			return alerts(), nil
		},
		Now: c.Now,
	})
}

func at(date, hm string) time.Time {
	ts, err := time.ParseInLocation("2006-01-02 15:04", date+" "+hm, jst)
	if err != nil {
		panic(err)
	}
	return ts
}

// Started on the morning after a day that began at 12:30: the start-up pass
// records that day from the snapshots as they are, an intraday gap with the
// morning missing. The previous trading day skips the holiday.
func TestStartupRecordsThePreviousTradingDay(t *testing.T) {
	conn := newDB(t)
	snapshots(t, conn, fullDay("2026-10-21")[0], fullDay("2026-10-21")[1], minutes("2026-10-22", "12:30", "15:30"))
	c := &clock{now: at("2026-10-23", "09:00")}
	if err := recorder(conn, c, nil).Check(t.Context()); err != nil {
		t.Fatalf("Check: %v", err)
	}
	r, ok := readRow(t, conn, "2026-10-22")
	if !ok || r.kind != "intraday" || !slices.Equal(r.codes, []string{sessiongap.CodeMorningMissing}) {
		t.Fatalf("10-22 = %+v (found %v), want intraday [morning_missing]", r, ok)
	}
	if r.rows != 180 || r.mins != 180 || r.longest != 150 || !r.first.Valid || !r.last.Valid {
		t.Errorf("counts = rows %d minutes %d longest %d first %v, want 180/180/150 with first and last", r.rows, r.mins, r.longest, r.first)
	}
	if today, ok := readRow(t, conn, "2026-10-23"); ok && today.kind != "in_progress" {
		t.Errorf("today was recorded before its close: %+v", today)
	}
}

func TestStartupSkipsTheHolidayAndRecordsAWholeDayGap(t *testing.T) {
	conn := newDB(t)
	snapshots(t, conn, fullDay("2026-10-08")[0], fullDay("2026-10-08")[1]) // history exists; 10-09 (Fri) has nothing
	// Tuesday 10-13: the days before are Sports Day 10-12 (holiday), the weekend and Friday 10-09.
	c := &clock{now: at("2026-10-13", "09:30")}
	if err := recorder(conn, c, nil).Check(t.Context()); err != nil {
		t.Fatalf("Check: %v", err)
	}
	if r, ok := readRow(t, conn, "2026-10-09"); !ok || r.kind != "whole_day" || r.rows != 0 || r.first.Valid {
		t.Fatalf("10-09 = %+v (found %v), want whole_day with no snapshot", r, ok)
	}
	for _, holiday := range []string{"2026-10-10", "2026-10-11", "2026-10-12"} {
		if _, ok := readRow(t, conn, holiday); ok {
			t.Errorf("%s (a non-trading day) was recorded", holiday)
		}
	}
	// Culture Day: Wednesday 11-04 looks back to Monday 11-02.
	c.now = at("2026-11-04", "08:00")
	if err := recorder(conn, c, nil).Check(t.Context()); err != nil {
		t.Fatalf("Check: %v", err)
	}
	if _, ok := readRow(t, conn, "2026-11-03"); ok {
		t.Error("2026-11-03 (Culture Day) was recorded")
	}
	if r, ok := readRow(t, conn, "2026-11-02"); !ok || r.kind != "whole_day" {
		t.Errorf("11-02 = %+v (found %v), want whole_day", r, ok)
	}
}

// The first start ever: no snapshot has been stored before, so the day cannot
// be judged and is not a gap (#814).
func TestNoHistoryIsRecordedAsInsufficientHistory(t *testing.T) {
	conn := newDB(t)
	c := &clock{now: at("2026-10-23", "09:00")}
	if err := recorder(conn, c, nil).Check(t.Context()); err != nil {
		t.Fatalf("Check: %v", err)
	}
	if r, ok := readRow(t, conn, "2026-10-22"); !ok || r.kind != "insufficient_history" {
		t.Fatalf("10-22 = %+v (found %v), want insufficient_history", r, ok)
	}
}

// Watched through a session the Recorder accumulates the warning codes it saw
// and the in-session checks, and keeps them when the day is recorded after the
// close. The first verdict stands: later passes do not rewrite it.
func TestSessionIsWatchedAndTheDayRecordedOnceAfterTheClose(t *testing.T) {
	conn := newDB(t)
	date := "2026-10-22"
	snapshots(t, conn, fullDay("2026-10-21")[0], fullDay("2026-10-21")[1])
	active := []string{"kabu_not_logged_in"}
	c := &clock{now: at(date, "08:50")} // inside the kabu window, before the open: alerts counted, no session check
	rec := recorder(conn, c, func() []string { return active })
	check := func(hm string) {
		t.Helper()
		c.now = at(date, hm)
		if err := rec.Check(t.Context()); err != nil {
			t.Fatalf("Check %s: %v", hm, err)
		}
	}
	check("08:50")
	active = []string{"market_data_idle"}
	check("10:00")
	check("10:01")
	check("12:00") // lunch break: in the kabu window, not in session
	if r, ok := readRow(t, conn, date); !ok || r.kind != "in_progress" || r.checks != 2 ||
		!slices.Equal(r.alerts, []string{"kabu_not_logged_in", "market_data_idle"}) {
		t.Fatalf("in progress = %+v (found %v), want 2 session checks and both alert codes", r, ok)
	}

	snapshots(t, conn, minutes(date, "12:30", "15:30"))
	check("15:32") // before the settle delay: not yet recorded
	if r, _ := readRow(t, conn, date); r.kind != "in_progress" {
		t.Fatalf("recorded before the settle delay: %+v", r)
	}
	check("15:36")
	r, _ := readRow(t, conn, date)
	if r.kind != "intraday" || !slices.Equal(r.codes, []string{sessiongap.CodeMorningMissing}) || r.checks != 2 ||
		!slices.Equal(r.alerts, []string{"kabu_not_logged_in", "market_data_idle"}) {
		t.Fatalf("recorded = %+v, want intraday [morning_missing] keeping checks and alerts", r)
	}

	// A later pass (and a restart) must not change the first verdict.
	snapshots(t, conn, minutes(date, "09:00", "11:30"))
	check("16:00")
	c.now = at("2026-10-23", "09:00")
	if err := recorder(conn, c, nil).Check(t.Context()); err != nil {
		t.Fatal(err)
	}
	if again, _ := readRow(t, conn, date); again.kind != "intraday" {
		t.Errorf("verdict rewritten to %+v", again)
	}
}

// A day the app left in progress (it stopped before the close) is closed at the
// next start from the snapshots that were stored, not skipped.
func TestDayLeftInProgressIsRecordedAtTheNextStart(t *testing.T) {
	conn := newDB(t)
	snapshots(t, conn, fullDay("2026-10-19")[0], fullDay("2026-10-19")[1], minutes("2026-10-20", "09:00", "11:30"))
	c := &clock{now: at("2026-10-20", "10:00")}
	if err := recorder(conn, c, func() []string { return []string{"kabu_unreachable"} }).Check(t.Context()); err != nil {
		t.Fatal(err)
	}
	// The app was off from here; it starts again two business days later.
	c.now = at("2026-10-22", "09:00")
	if err := recorder(conn, c, nil).Check(t.Context()); err != nil {
		t.Fatal(err)
	}
	r, ok := readRow(t, conn, "2026-10-20")
	if !ok || r.kind != "intraday" || !slices.Equal(r.codes, []string{sessiongap.CodeAfternoonMissing}) || !slices.Equal(r.alerts, []string{"kabu_unreachable"}) {
		t.Fatalf("10-20 = %+v (found %v), want intraday [afternoon_missing] keeping the alert seen", r, ok)
	}
	if r, ok := readRow(t, conn, "2026-10-21"); !ok || r.kind != "whole_day" {
		t.Errorf("10-21 = %+v (found %v), want whole_day (the previous trading day)", r, ok)
	}
}

// The record outlives the retention purge of succeeded jobs (7 days): the
// purge deletes the jobs the counts came from and leaves the daily record.
func TestRecordSurvivesTheJobRetentionPurge(t *testing.T) {
	conn := newDB(t)
	now := time.Now().In(jst)
	// The day to record: the previous trading day of now, with a succeeded job finishing on it.
	date := ""
	for d := now.AddDate(0, 0, -1); ; d = d.AddDate(0, 0, -1) {
		if marketcalendar.TSE.IsTradingDay(d) {
			date = d.Format(time.DateOnly)
			break
		}
	}
	// Some history (inside the snapshot retention), then an afternoon-only day.
	snapshots(t, conn, []time.Time{now.AddDate(0, 0, -30)}, minutes(date, "12:30", "15:30"))
	finished := sqlutil.FormatTime(at(date, "13:00"))
	if _, err := conn.Exec(`INSERT INTO jobs (queue, payload_json, status, scheduled_at, finished_at, created_at)
		VALUES ('market-data', '{}', 'succeeded', ?, ?, ?)`, finished, finished, finished); err != nil {
		t.Fatal(err)
	}
	if err := recorder(conn, &clock{now: now}, nil).Check(t.Context()); err != nil {
		t.Fatal(err)
	}
	r, ok := readRow(t, conn, date)
	if !ok || r.kind != "intraday" || !r.jobsOK.Valid || r.jobsOK.Int64 != 1 || r.jobsFailed.Int64 != 0 {
		t.Fatalf("record = %+v (found %v), want intraday with 1 succeeded job counted", r, ok)
	}

	// Age everything past the succeeded-job retention and purge.
	old := sqlutil.FormatTime(now.AddDate(0, 0, -(retention.DefaultSucceededJobDays + 3)))
	if _, err := conn.Exec(`UPDATE jobs SET finished_at = ?`, old); err != nil {
		t.Fatal(err)
	}
	if err := retention.New(conn, retention.Policy{}).Purge(t.Context()); err != nil {
		t.Fatalf("Purge: %v", err)
	}
	var jobs int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM jobs`).Scan(&jobs); err != nil || jobs != 0 {
		t.Fatalf("jobs after purge = %d (%v), want 0: the purge must have deleted them", jobs, err)
	}
	after, ok := readRow(t, conn, date)
	if !ok || after.kind != "intraday" || !after.jobsOK.Valid || after.jobsOK.Int64 != 1 {
		t.Errorf("record after the purge = %+v (found %v), want it unchanged", after, ok)
	}
}

// A day older than the job retention is recorded without job counts: the
// purged jobs would read as zero otherwise.
func TestJobCountsAreOmittedForADayOlderThanTheJobRetention(t *testing.T) {
	conn := newDB(t)
	snapshots(t, conn, fullDay("2026-10-01")[0], fullDay("2026-10-01")[1])
	// Left in progress on 10-02 and recorded at a start 20 days later.
	if _, err := conn.Exec(`INSERT INTO session_diagnostics_daily (session_date, gap_kind, recorded_at) VALUES ('2026-10-02', 'in_progress', ?)`,
		sqlutil.FormatTime(at("2026-10-02", "10:00"))); err != nil {
		t.Fatal(err)
	}
	c := &clock{now: at("2026-10-22", "09:00")}
	if err := recorder(conn, c, nil).Check(t.Context()); err != nil {
		t.Fatal(err)
	}
	r, ok := readRow(t, conn, "2026-10-02")
	if !ok || r.kind == "in_progress" || r.jobsOK.Valid || r.jobsFailed.Valid {
		t.Errorf("10-02 = %+v (found %v), want recorded with null job counts", r, ok)
	}
}
