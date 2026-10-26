package metalabeltests

import (
	"database/sql"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/metalabeldiag"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

// seedEvery seeds n learnable rows one every step, the last one at end (ids from
// firstID), and returns the times they were decided at.
func seedEvery(t *testing.T, w *sql.DB, n, firstID int, end time.Time, step time.Duration) []time.Time {
	t.Helper()
	rows := learnable(n, firstID, end, "jev-1.13.0")
	at := make([]time.Time, n)
	for i, d := range rows {
		d.at = end.Add(-time.Duration(n-1-i) * step)
		at[i] = d.at
		insertDecision(t, w, d)
	}
	return at
}

// Issue #913: the shape of the daily skip: every usable row sits after the
// LLM cutoff, so the OOS window opens at the cutoff itself, nothing is left to
// learn from, and the tool says so: oos_start_reason is cutoff_floor, the
// shortfall is what the bar and the fit/calibration minimum still lack, and the
// retraining's own skip agrees on the count.
func TestReadinessSaysTheCutoffHoldsTheOOSWindowOpen(t *testing.T) {
	now := testkit.FixedNow() // 2026-10-18T03:00Z
	path, w := readinessDB(t)
	testkit.Setting(t, w, "metalabel.llm_cutoff", `"2026-10-14"`)
	const rows = 527 // the usable rows of the real skip, all after the cutoff
	at := seedEvery(t, w, rows, 1, now.Add(-30*time.Minute), 10*time.Minute)
	if !at[0].After(time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("fixture: the first row %v is not after the cutoff", at[0])
	}

	skip := runNightly(t, w, now).skips
	d := callReadiness(t, newTestServer(t, path, now), fullLookback)

	if len(skip) != 1 || d.Split.TrainSkipCode == nil || *d.Split.TrainSkipCode != skip[0].ReasonCode || d.Split.Train.Rows != skip[0].UsableRows {
		t.Fatalf("train skip = %v / %d rows, the nightly retraining recorded %+v", d.Split.TrainSkipCode, d.Split.Train.Rows, skip)
	}
	if d.Features.UsableRows != rows || d.Split.Train.Rows != 0 {
		t.Errorf("all usable rows = %d (want %d), rows before the OOS window = %d (want 0)", d.Features.UsableRows, rows, d.Split.Train.Rows)
	}
	if d.Split.OOSStartReason != metalabeldiag.OOSStartCutoffFloor || d.Split.OOSFloorSource != metalabeldiag.OOSStartCutoffFloor ||
		d.Split.OOSStart != "2026-10-14T00:00:00Z" || d.Split.OOSFloor != d.Split.OOSStart {
		t.Errorf("oos start = %s (%s), floor = %s (%s), want the cutoff 2026-10-14T00:00:00Z as cutoff_floor", d.Split.OOSStart, d.Split.OOSStartReason, d.Split.OOSFloor, d.Split.OOSFloorSource)
	}
	s := d.Split.Shortfall
	if s.OOSWindowRows != rows || s.OOSRowsMissing != oosBarRows()-rows || s.TrainRowsMissing != fitAndCalibration() || s.PurgedRows != 0 {
		t.Errorf("shortfall = %+v, want %d rows in the window, %d short of the bar, %d short for fit and calibration, nothing purged", s, rows, oosBarRows()-rows, fitAndCalibration())
	}
	if want := oosBarRows() + fitAndCalibration() - rows; s.AdditionalRowsNeeded != want {
		t.Errorf("additional rows needed = %d, want OOS minimum + fit/calibration minimum - usable rows = %d", s.AdditionalRowsNeeded, want)
	}

	// Rows a day: counted by hand on the five newest trading days before the
	// as-of day (Sun 10/18): Fri 10/16, Thu 15, Wed 14, Tue 13, and Fri 10/9
	// (Mon 10/12 is Sports Day); the weekend rows are not in it.
	days := map[string]bool{"2026-10-16": true, "2026-10-15": true, "2026-10-14": true, "2026-10-13": true, "2026-10-09": true}
	counted := 0
	for _, a := range at {
		if days[a.In(query.JST).Format(time.DateOnly)] {
			counted++
		}
	}
	if want := float64(counted) / 5; s.RateTradingDays != 5 || s.RowsPerTradingDay != want || counted == 0 {
		t.Errorf("rows per trading day = %v over %d days, want %v (%d rows on %v)", s.RowsPerTradingDay, s.RateTradingDays, want, counted, days)
	}
	if want := int((float64(s.AdditionalRowsNeeded) + s.RowsPerTradingDay - 1e-9) / s.RowsPerTradingDay); s.EstimatedTradingDays == nil || *s.EstimatedTradingDays != want {
		t.Errorf("estimated trading days = %v, want %d (%d rows at %v a day, rounded up)", s.EstimatedTradingDays, want, s.AdditionalRowsNeeded, s.RowsPerTradingDay)
	}
	// 10/14 09:00 JST to 10/18: three trading days (14, 15, 16) to hold the bar.
	if bar := oosBarRows(); s.OOSWindowTradingDays != 3 || s.OOSMinRowsPerTradingDay == nil || *s.OOSMinRowsPerTradingDay != (bar+2)/3 ||
		s.RateReachesOOSBar == nil || *s.RateReachesOOSBar != (s.RowsPerTradingDay >= float64((bar+2)/3)) {
		t.Errorf("OOS window = %d trading days, minimum rows a day = %v, reached = %v; want 3 days and ceil(%d/3)", s.OOSWindowTradingDays, s.OOSMinRowsPerTradingDay, s.RateReachesOOSBar, bar)
	}
}

// When the rows after the floor outnumber what the bar asks for, the window
// opens where it holds exactly the minimum rows (min_rows); when the newest
// minimum days already hold more rows than that, it opens at the first row of
// the oldest of those days (min_days). Nothing is missing then.
func TestReadinessSaysWhatSizesTheOOSWindow(t *testing.T) {
	now := testkit.FixedNow()
	for _, tc := range []struct {
		name         string
		recent       int
		step         time.Duration
		wantReason   string
		wantMoreRows int
	}{
		// 100 more rows than the bar over about 12 days: the 7 newest days hold fewer than the bar's rows.
		{"min_rows", oosBarRows() + 100, 20 * time.Minute, metalabeldiag.OOSStartMinRows, 0},
		// every 7 minutes: the 7 newest days alone hold more than the bar's rows.
		{"min_days", 2 * oosBarRows(), 7 * time.Minute, metalabeldiag.OOSStartMinDays, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path, w := readinessDB(t)
			seedEvery(t, w, 2*fitAndCalibration(), 1, now.Add(-25*24*time.Hour), 20*time.Minute) // fit and calibration side
			seedEvery(t, w, tc.recent, 10_000, now.Add(-30*time.Minute), tc.step)
			d := callReadiness(t, newTestServer(t, path, now), fullLookback)

			if !d.Split.TrainReady || d.Split.OOSStartReason != tc.wantReason || d.Split.OOSFloorSource != metalabeldiag.OOSStartMaxWindow {
				t.Fatalf("train ready = %v, oos start reason = %s (floor %s), want %s", d.Split.TrainReady, d.Split.OOSStartReason, d.Split.OOSFloorSource, tc.wantReason)
			}
			if d.Split.OOSStart == d.Split.OOSFloor {
				t.Errorf("the window opens at its floor %s, so the rows or the days did not decide it", d.Split.OOSFloor)
			}
			if s := d.Split.Shortfall; s.AdditionalRowsNeeded != tc.wantMoreRows || s.OOSRowsMissing != 0 || s.TrainRowsMissing != 0 || s.EstimatedTradingDays == nil || *s.EstimatedTradingDays != 0 {
				t.Errorf("shortfall = %+v, want nothing missing", s)
			}
		})
	}
}

// With enough rows only just (the window holds the bar's rows from its floor
// and not one more), the floor is what decides and the max window is named when
// the cutoff is older than it.
func TestReadinessNamesTheMaxWindowWhenTheCutoffIsOlder(t *testing.T) {
	now := testkit.FixedNow()
	path, w := readinessDB(t) // cutoff 2026-09-01, older than 21 days before now
	history(t, w, now, 2*fitAndCalibration(), 200)
	d := callReadiness(t, newTestServer(t, path, now), fullLookback)
	if d.Split.OOSStartReason != metalabeldiag.OOSStartMaxWindow || d.Split.OOSFloorSource != metalabeldiag.OOSStartMaxWindow ||
		d.Split.OOSFloor != now.Add(-21*24*time.Hour).Format(time.RFC3339) {
		t.Errorf("oos start reason = %s, floor = %s (%s), want max_window at 21 days before as-of", d.Split.OOSStartReason, d.Split.OOSFloor, d.Split.OOSFloorSource)
	}
	if d.Split.Shortfall.OOSRowsMissing != oosBarRows()-200 {
		t.Errorf("oos rows missing = %d, want %d", d.Split.Shortfall.OOSRowsMissing, oosBarRows()-200)
	}
}
