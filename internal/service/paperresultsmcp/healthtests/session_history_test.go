package healthtests

import (
	"database/sql"
	"slices"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

// Issue #814: a trading day before the first snapshot ever stored is a day the
// system did not exist yet, so it is "no data to judge" (insufficient history),
// not a missing session. A day after the first snapshot with no snapshot is a
// real gap and still raises missing_session.
func TestMissingSessionSeparatesInsufficientHistoryFromRealGap(t *testing.T) {
	afternoon := at(20, 13, 0) // Tuesday; the 7 days checked are Oct 13-19.
	// Snapshots of the healthy baseline: every trading day of Oct 13-19.
	deleteDays := func(days ...int) func(*testing.T, *sql.DB) {
		return func(t *testing.T, w *sql.DB) {
			for _, d := range days {
				testkit.MustExec(t, w, `DELETE FROM market_snapshots WHERE timestamp >= ? AND timestamp < ?`,
					stored(t, at(d, 0, 0)), stored(t, at(d+1, 0, 0)))
			}
		}
	}
	tests := []struct {
		name         string
		setup        func(t *testing.T, w *sql.DB)
		wantMissing  []string
		wantNoData   []string
		wantAttended bool
	}{
		{name: "one week after the first start: full history, nothing to report", setup: func(*testing.T, *sql.DB) {}},
		{name: "right after the first start: only yesterday has a snapshot", setup: deleteDays(13, 14, 15, 16),
			wantNoData: []string{"2026-10-13", "2026-10-14", "2026-10-15", "2026-10-16"}},
		{name: "first launch with no snapshot at all: nothing can be judged", setup: deleteDays(13, 14, 15, 16, 19),
			wantNoData: []string{"2026-10-13", "2026-10-14", "2026-10-15", "2026-10-16", "2026-10-19"}},
		{name: "a real gap after the first snapshot still warns", setup: deleteDays(15),
			wantMissing: []string{"2026-10-15"}, wantAttended: true},
		{name: "history partly short and a real gap: each is reported by its own kind", setup: deleteDays(13, 16),
			wantMissing: []string{"2026-10-16"}, wantNoData: []string{"2026-10-13"}, wantAttended: true},
		{name: "days before the first snapshot are never a warning by themselves", setup: deleteDays(13, 14),
			wantNoData: []string{"2026-10-13", "2026-10-14"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, w := testkit.HealthyDay(t, afternoon)
			tt.setup(t, w)
			h := healthAt(t, w, afternoon)
			if !slices.Equal(h.MissingSessionDates, tt.wantMissing) && len(h.MissingSessionDates)+len(tt.wantMissing) > 0 {
				t.Errorf("missing_session_dates = %v, want %v", h.MissingSessionDates, tt.wantMissing)
			}
			if !slices.Equal(h.InsufficientHistoryDates, tt.wantNoData) && len(h.InsufficientHistoryDates)+len(tt.wantNoData) > 0 {
				t.Errorf("insufficient_history_dates = %v, want %v", h.InsufficientHistoryDates, tt.wantNoData)
			}
			if got := slices.Contains(h.AttentionCodes, query.AttentionMissingSession); got != tt.wantAttended {
				t.Errorf("missing_session raised = %v, want %v (codes %v)", got, tt.wantAttended, h.AttentionCodes)
			}
		})
	}
}

// The first start on a trading day after the close: today has no snapshot
// either, and with no snapshot ever stored there is no history to judge it by.
func TestMissingSessionTodayAfterCloseWithNoHistoryIsInsufficient(t *testing.T) {
	now := at(20, 15, 30)
	_, w := testkit.HealthyDay(t, now)
	testkit.MustExec(t, w, `DELETE FROM market_snapshots`)
	h := healthAt(t, w, now)
	if len(h.MissingSessionDates) != 0 || slices.Contains(h.AttentionCodes, query.AttentionMissingSession) {
		t.Errorf("missing = %v, codes %v; want no warning without any history", h.MissingSessionDates, h.AttentionCodes)
	}
	if want := []string{"2026-10-13", "2026-10-14", "2026-10-15", "2026-10-16", "2026-10-19", "2026-10-20"}; !slices.Equal(h.InsufficientHistoryDates, want) {
		t.Errorf("insufficient_history_dates = %v, want %v", h.InsufficientHistoryDates, want)
	}
}
