package healthtests

import (
	"database/sql"
	"slices"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

// minutesOf are the snapshot minutes of [from, to) on October day.
func minutesOf(day, fh, fm, th, tm int) []time.Time {
	var out []time.Time
	for m := at(day, fh, fm); m.Before(at(day, th, tm)); m = m.Add(time.Minute) {
		out = append(out, m)
	}
	return out
}

// replaceDay empties the snapshots of October day and stores the given ones.
func replaceDay(t *testing.T, w *sql.DB, day int, minutes ...[]time.Time) {
	t.Helper()
	testkit.MustExec(t, w, `DELETE FROM market_snapshots WHERE timestamp >= ? AND timestamp < ?`,
		stored(t, at(day, 0, 0)), stored(t, at(day+1, 0, 0)))
	for _, m := range minutes {
		testkit.SnapshotsAt(t, w, m...)
	}
}

func gapCodes(h query.Health, date string) []string {
	for _, g := range h.IntradayGaps {
		if g.Date == date {
			return g.Codes
		}
	}
	return nil
}

// Issue #1054: a day with snapshots that lacks part of a session is an
// intraday gap, raised as partial_session with the gap codes. It is distinct
// from a whole-day gap (missing_session_dates / missing_session) and from
// insufficient history (#814), and a day that is complete raises neither.
func TestIntradayGapIsFlaggedApartFromWholeDayAndInsufficientHistory(t *testing.T) {
	now := at(20, 13, 0) // the days checked are Oct 13-19
	afternoonOnly := func(day int) func(*testing.T, *sql.DB) {
		return func(t *testing.T, w *sql.DB) { replaceDay(t, w, day, minutesOf(day, 12, 30, 15, 30)) }
	}
	tests := []struct {
		name         string
		setup        func(t *testing.T, w *sql.DB)
		wantIntraday map[string][]string
		wantMissing  []string
		wantNoData   []string
		wantCodes    []string
	}{
		{name: "complete sessions raise neither", setup: func(*testing.T, *sql.DB) {}, wantCodes: []string{}},
		{name: "started at 12:30: the morning is missing", setup: afternoonOnly(15),
			wantIntraday: map[string][]string{"2026-10-15": {"morning_missing"}}, wantCodes: []string{query.AttentionPartialSession}},
		{name: "first snapshot at 09:23 is a late start (the day of 11-09 in the report)",
			setup: func(t *testing.T, w *sql.DB) {
				replaceDay(t, w, 16, minutesOf(16, 9, 23, 11, 30), minutesOf(16, 12, 30, 15, 30))
			},
			wantIntraday: map[string][]string{"2026-10-16": {"late_start"}}, wantCodes: []string{query.AttentionPartialSession}},
		{name: "a long hole in the afternoon",
			setup: func(t *testing.T, w *sql.DB) {
				replaceDay(t, w, 14, minutesOf(14, 9, 0, 11, 30), minutesOf(14, 12, 30, 13, 0), minutesOf(14, 13, 40, 15, 30))
			},
			wantIntraday: map[string][]string{"2026-10-14": {"mid_session_gap"}}, wantCodes: []string{query.AttentionPartialSession}},
		{name: "a whole-day gap is missing_session, not partial_session",
			setup:       func(t *testing.T, w *sql.DB) { replaceDay(t, w, 15) },
			wantMissing: []string{"2026-10-15"}, wantCodes: []string{query.AttentionMissingSession}},
		{name: "a whole-day gap and an intraday gap are reported by their own kind",
			setup: func(t *testing.T, w *sql.DB) {
				replaceDay(t, w, 15)
				afternoonOnly(16)(t, w)
			},
			wantMissing: []string{"2026-10-15"}, wantIntraday: map[string][]string{"2026-10-16": {"morning_missing"}},
			wantCodes: []string{query.AttentionMissingSession, query.AttentionPartialSession}},
		{name: "days before the first snapshot are insufficient history, never a gap",
			setup: func(t *testing.T, w *sql.DB) {
				for _, d := range []int{13, 14} {
					replaceDay(t, w, d)
				}
			},
			wantNoData: []string{"2026-10-13", "2026-10-14"}, wantCodes: []string{}},
		{name: "the first day of the history is not judged for the part before it began",
			setup: func(t *testing.T, w *sql.DB) {
				for _, d := range []int{13, 14} {
					replaceDay(t, w, d)
				}
				replaceDay(t, w, 15, minutesOf(15, 12, 30, 15, 30)) // history begins at 12:30
			},
			wantNoData: []string{"2026-10-13", "2026-10-14"}, wantCodes: []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, w := testkit.HealthyDay(t, now)
			tt.setup(t, w)
			h := healthAt(t, w, now)
			got := map[string][]string{}
			for _, g := range h.IntradayGaps {
				got[g.Date] = g.Codes
			}
			if len(got) != len(tt.wantIntraday) {
				t.Errorf("intraday_gaps = %+v, want %v", h.IntradayGaps, tt.wantIntraday)
			}
			for date, codes := range tt.wantIntraday {
				if !slices.Equal(gapCodes(h, date), codes) {
					t.Errorf("intraday_gaps[%s] = %v, want %v", date, gapCodes(h, date), codes)
				}
			}
			if !slices.Equal(h.MissingSessionDates, tt.wantMissing) {
				t.Errorf("missing_session_dates = %v, want %v", h.MissingSessionDates, tt.wantMissing)
			}
			if !slices.Equal(h.InsufficientHistoryDates, tt.wantNoData) {
				t.Errorf("insufficient_history_dates = %v, want %v", h.InsufficientHistoryDates, tt.wantNoData)
			}
			if !slices.Equal(h.AttentionCodes, tt.wantCodes) {
				t.Errorf("attention_codes = %v, want %v", h.AttentionCodes, tt.wantCodes)
			}
		})
	}
}

// The app stopped for the night: no record of the previous trading day exists.
// On the next MCP read (or start) the gap still shows as a to-do, judged from
// the snapshots as they are.
func TestPreviousBusinessDayGapShowsOnReadWithoutARecord(t *testing.T) {
	now := at(20, 9, 30) // Tuesday morning; the previous trading day is Monday 10-19
	_, w := testkit.HealthyDay(t, now)
	replaceDay(t, w, 19, minutesOf(19, 12, 30, 15, 30))
	h := healthAt(t, w, now)
	if !slices.Equal(gapCodes(h, "2026-10-19"), []string{"morning_missing"}) || !slices.Contains(h.AttentionCodes, query.AttentionPartialSession) {
		t.Fatalf("health = intraday %+v, codes %v; want 10-19 morning_missing and partial_session", h.IntradayGaps, h.AttentionCodes)
	}
}

// A recorded day is taken from its record, which outlives the snapshot and job
// retention (the snapshots of 10-15 are gone here); the lookback of 7 days is
// still bounded.
func TestRecordedDayIsReadFromItsRecord(t *testing.T) {
	now := at(20, 13, 0)
	_, w := testkit.HealthyDay(t, now)
	replaceDay(t, w, 15) // the snapshots of the day are gone (retention)
	testkit.MustExec(t, w, `INSERT INTO session_diagnostics_daily (session_date, gap_kind, gap_codes_json, gaps_json, recorded_at)
		VALUES ('2026-10-15', 'intraday', '["late_start"]', '[{"code":"late_start","from":"2026-10-15T00:00:00Z","to":"2026-10-15T01:12:00Z","minutes":72}]', ?)`,
		stored(t, at(15, 15, 40)))
	h := healthAt(t, w, now)
	if !slices.Equal(gapCodes(h, "2026-10-15"), []string{"late_start"}) || len(h.MissingSessionDates) != 0 {
		t.Fatalf("health = intraday %+v, missing %v; want 10-15 late_start from the record, not a whole-day gap", h.IntradayGaps, h.MissingSessionDates)
	}
	if got := h.IntradayGaps[0]; len(got.Gaps) != 1 || got.Gaps[0].Minutes != 72 {
		t.Errorf("gap spans = %+v, want the recorded 72-minute span", got.Gaps)
	}
}

// An in-progress row (today's, still being watched) is no verdict.
func TestInProgressRecordIsNotAVerdict(t *testing.T) {
	now := at(20, 13, 0)
	_, w := testkit.HealthyDay(t, now)
	testkit.MustExec(t, w, `INSERT INTO session_diagnostics_daily (session_date, gap_kind, recorded_at) VALUES ('2026-10-19', 'in_progress', ?)`,
		stored(t, at(19, 10, 0)))
	h := healthAt(t, w, now)
	if len(h.IntradayGaps) != 0 || len(h.MissingSessionDates) != 0 || len(h.AttentionCodes) != 0 {
		t.Fatalf("health = intraday %+v missing %v codes %v; want the day judged from its snapshots (complete)", h.IntradayGaps, h.MissingSessionDates, h.AttentionCodes)
	}
}

// Holidays hold no snapshot and are not trading days, so no gap is counted for
// them: Sports Day 2026-10-12 and Culture Day 2026-11-03 sit inside the seven
// days checked here.
func TestHolidaysAreNeverCountedAsGaps(t *testing.T) {
	for name, now := range map[string]time.Time{
		"Sports Day":  time.Date(2026, 10, 15, 13, 0, 0, 0, at(1, 0, 0).Location()), // checks 10-08..10-14
		"Culture Day": time.Date(2026, 11, 6, 13, 0, 0, 0, at(1, 0, 0).Location()),  // checks 10-30..11-05
	} {
		t.Run(name, func(t *testing.T) {
			_, w := testkit.HealthyDay(t, now)
			h := healthAt(t, w, now)
			if len(h.MissingSessionDates) != 0 || len(h.InsufficientHistoryDates) != 0 || len(h.IntradayGaps) != 0 {
				t.Fatalf("missing %v, insufficient %v, intraday %+v; want none: a holiday is no gap",
					h.MissingSessionDates, h.InsufficientHistoryDates, h.IntradayGaps)
			}
			for _, c := range []string{query.AttentionMissingSession, query.AttentionPartialSession} {
				if slices.Contains(h.AttentionCodes, c) {
					t.Errorf("%s raised on a holiday: %v", c, h.AttentionCodes)
				}
			}
		})
	}
}
