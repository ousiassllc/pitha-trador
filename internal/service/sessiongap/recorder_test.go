package sessiongap_test

import (
	"slices"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/sessiongap"
)

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
