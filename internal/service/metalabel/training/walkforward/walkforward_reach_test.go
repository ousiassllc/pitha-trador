package walkforward_test

import (
	"errors"
	"testing"
	"time"

	repo "github.com/ousiassllc/pitha-trador/internal/repository/metalabel"
	"github.com/ousiassllc/pitha-trador/internal/service/metalabel/training/walkforward"
)

// dailyRows returns perDay rows on each of the given number of JST trading days
// ending with the day of wfNow (10/14 JST, 18:00 now): the rows of a day are
// spread over 09:00-15:00 JST; every label window is 15 minutes long.
func dailyRows(days, perDay int) []walkforward.Row {
	jst := time.FixedZone("JST", 9*3600)
	var rows []walkforward.Row
	for d := days - 1; d >= 0; d-- {
		open := time.Date(2026, 10, 14, 9, 0, 0, 0, jst).AddDate(0, 0, -d)
		for i := range perDay {
			at := open.Add(time.Duration(i) * 6 * time.Hour / time.Duration(perDay))
			rows = append(rows, walkforward.Row{DecidedAt: at, LabelEnd: at.Add(15 * time.Minute), Direction: "LONG", Vector: []float64{1}})
		}
	}
	return rows
}

// reachConfig holds out at least 7 JST days and 740 rows, as far back as 21
// days, the shape of the default (issue #862).
func reachConfig() walkforward.WalkForwardConfig {
	return walkforward.WalkForwardConfig{
		OOSMinRows: 740, OOSMinDays: 7, OOSMaxWindow: 21 * 24 * time.Hour,
		CalibrationFraction: 0.25, Embargo: 30 * time.Minute, MinFit: 100, MinCalibration: 30,
	}
}

// Issue #862: the window reaches back until it holds the days AND the rows the
// bar asks for, whichever is further, and takes whole days.
func TestSplitWalkForward_OOSWindowReachesBackToHoldTheWantedDaysAndRows(t *testing.T) {
	cfg := reachConfig()
	// 120 rows a day: 7 days are 840 rows, more than 740, so the days decide.
	s, err := walkforward.SplitWalkForward(dailyRows(12, 120), wfNow, time.Time{}, cfg)
	if err != nil {
		t.Fatalf("120 rows a day: %v", err)
	}
	if len(s.OOS) != 7*120 {
		t.Errorf("120 rows a day: OOS rows = %d, want the 7 newest days (840)", len(s.OOS))
	}
	// 60 rows a day: 7 days are 420 rows, 740 rows need 13 days: the rows decide.
	s, err = walkforward.SplitWalkForward(dailyRows(20, 60), wfNow, time.Time{}, cfg)
	if err != nil {
		t.Fatalf("60 rows a day: %v", err)
	}
	if len(s.OOS) != 740 {
		t.Errorf("60 rows a day: OOS rows = %d, want exactly the newest 740", len(s.OOS))
	}
	// The days decide on whole days: the 7 newest JST days open at 09:00 JST.
	s, err = walkforward.SplitWalkForward(dailyRows(12, 120), wfNow, time.Time{}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	jst := time.FixedZone("JST", 9*3600)
	if got := s.OOS[0].DecidedAt.In(jst); got.Hour() != 9 || got.Minute() != 0 {
		t.Errorf("OOS opens at %v JST, want the first row of a day (09:00)", got)
	}
}

// The window never reaches back further than OOSMaxWindow, and never before the
// LLM cutoff even when the bar would want more rows (the rule of walkforward.go
// kept by issue #862).
func TestSplitWalkForward_WideOOSWindowIsCappedByTheMaxWindowAndTheCutoff(t *testing.T) {
	cfg := reachConfig()
	rows := dailyRows(40, 20) // 800 rows on 40 days: 740 rows would need 37 days

	s, err := walkforward.SplitWalkForward(rows, wfNow, time.Time{}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if want := wfNow.Add(-cfg.OOSMaxWindow); !s.OOSStart.Equal(want) {
		t.Errorf("OOSStart = %v, want now - OOSMaxWindow %v", s.OOSStart, want)
	}

	cutoff := wfNow.Add(-5 * 24 * time.Hour)
	s, err = walkforward.SplitWalkForward(rows, wfNow, cutoff, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !s.OOSStart.Equal(cutoff) {
		t.Errorf("OOSStart = %v, want the LLM cutoff %v", s.OOSStart, cutoff)
	}
	for _, r := range s.OOS {
		if r.DecidedAt.Before(cutoff) {
			t.Fatalf("OOS holds a decision from %v, before the cutoff %v", r.DecidedAt, cutoff)
		}
	}
}

// Issue #862: a skip carries both sides of the split, the rows before the OOS
// window against what fit and calibration need, and the rows in it against what
// the window reaches back to hold.
func TestSplitWalkForward_InsufficientDataCarriesTheOOSCounts(t *testing.T) {
	cfg := reachConfig()
	// Two days of 50 rows: the 7-day window swallows everything, nothing is left to train on.
	_, err := walkforward.SplitWalkForward(dailyRows(2, 50), wfNow, time.Time{}, cfg)
	var e *walkforward.InsufficientDataError
	if !errors.As(err, &e) {
		t.Fatalf("error = %v, want *InsufficientDataError", err)
	}
	if e.Code != repo.SkipTooFewRowsBeforeOOS || e.Usable != 0 || e.Required != 130 || !intIs(e.OOSRows, 100) || !intIs(e.RequiredOOSRows, 740) {
		t.Errorf("skip = %+v, want too_few_rows_before_oos 0 of 130 before, 100 of 740 in the window", e)
	}
}

// intIs reports whether p is recorded and equals want.
func intIs(p *int, want int) bool { return p != nil && *p == want }
