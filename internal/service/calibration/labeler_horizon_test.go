package calibration_test

import (
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

func countOutcomes(t *testing.T, f labelerFixtures, decisionID int64, horizonMinutes int) int {
	t.Helper()
	var n int
	if err := f.db.QueryRow(
		`SELECT COUNT(*) FROM calibration_outcomes WHERE jev_decision_id = ? AND horizon_minutes = ?`,
		decisionID, horizonMinutes,
	).Scan(&n); err != nil {
		t.Fatalf("count calibration outcomes: %v", err)
	}
	return n
}

// A decision at 15:25 JST has only ~5 minutes of bars before the 15:30
// close: the 20-minute horizon must not be labeled with that shortened
// window (FR-CAL-4).
func TestLabeler_HandleJob_HorizonCrossingCloseIsNotLabeled(t *testing.T) {
	f := newLabelerFixtures(t)
	base := time.Date(2026, 9, 28, 15, 25, 0, 0, time.FixedZone("JST", 9*3600))
	decision := f.insertDecision(t, base, domain.JevDirectionLong, 0.82)
	f.insertSnapshots(t, map[time.Duration]float64{
		0:               1000,
		2 * time.Minute: 1010,
		5 * time.Minute: 1020, // 15:30 close bar
	}, base)

	if err := f.handleJob(t, decision.ID, 20); err == nil {
		t.Fatalf("HandleJob labeled a 20m horizon from a window that ends 15 minutes short, want an error")
	}
	if n := countOutcomes(t, f, decision.ID, 20); n != 0 {
		t.Fatalf("calibration_outcomes rows for the 20m horizon = %d, want 0", n)
	}
	// The 5-minute horizon is fully covered by the same bars and is still labeled.
	if err := f.handleJob(t, decision.ID, 5); err != nil {
		t.Fatalf("HandleJob(5m horizon): %v", err)
	}
}

// A horizon spanning the lunch break (bars stop at 11:30, resume 12:30)
// has a window whose last bar is well before the horizon time, so it is
// not labeled even though later bars exist outside the window.
func TestLabeler_HandleJob_HorizonCrossingLunchBreakIsNotLabeled(t *testing.T) {
	f := newLabelerFixtures(t)
	jst := time.FixedZone("JST", 9*3600)
	base := time.Date(2026, 9, 28, 11, 25, 0, 0, jst)
	decision := f.insertDecision(t, base, domain.JevDirectionShort, 0.7)
	f.insertSnapshots(t, map[time.Duration]float64{
		0:                 1000,
		4 * time.Minute:   990,
		65 * time.Minute:  980, // 12:30 afternoon open
		70 * time.Minute:  985,
		120 * time.Minute: 985,
	}, base)

	if err := f.handleJob(t, decision.ID, 10); err == nil {
		t.Fatalf("HandleJob labeled a 10m horizon across the lunch break, want an error")
	}
	if n := countOutcomes(t, f, decision.ID, 10); n != 0 {
		t.Fatalf("calibration_outcomes rows = %d, want 0", n)
	}
}

// A last bar within horizonBarTolerance of the horizon time (one missed
// 1-minute cycle) still labels the decision.
func TestLabeler_HandleJob_LastBarWithinToleranceIsLabeled(t *testing.T) {
	f := newLabelerFixtures(t)
	base := time.Date(2026, 9, 27, 9, 0, 0, 0, time.UTC)
	decision := f.insertDecision(t, base, domain.JevDirectionLong, 0.82)
	f.insertSnapshots(t, map[time.Duration]float64{
		0:                               1000,
		10*time.Minute + 5*time.Second:  1010,
		20*time.Minute - 90*time.Second: 1030, // 18.5m: one missed bar before the 20m horizon
	}, base)

	if err := f.handleJob(t, decision.ID, 20); err != nil {
		t.Fatalf("HandleJob: %v", err)
	}
	o := f.getOutcome(t, decision.ID, 20)
	if !almostEqual(o.FutureReturn, 3.0) {
		t.Fatalf("FutureReturn = %v, want ~3.0", o.FutureReturn)
	}
}
