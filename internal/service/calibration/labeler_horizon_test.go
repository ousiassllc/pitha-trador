package calibration_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/jobqueue"
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

func isDeferred(err error) bool {
	var d *jobqueue.DeferredError
	return errors.As(err, &d)
}

func isSkipped(err error) bool {
	var s *jobqueue.SkippedError
	return errors.As(err, &s)
}

// A decision at 15:25 JST has only ~5 minutes of bars before the 15:30
// close: the 15-minute horizon must not be labeled with that shortened
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

	if err := f.handleJob(t, decision.ID, 15); !isDeferred(err) {
		t.Fatalf("HandleJob error = %v, want jobqueue.Defer for a window that ends 10 minutes short", err)
	}
	if n := countOutcomes(t, f, decision.ID, 15); n != 0 {
		t.Fatalf("calibration_outcomes rows for the 15m horizon = %d, want 0", n)
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

	if err := f.handleJob(t, decision.ID, 10); !isDeferred(err) {
		t.Fatalf("HandleJob across the lunch break error = %v, want jobqueue.Defer (not labeled, not a failure)", err)
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
		15*time.Minute - 90*time.Second: 1030, // 13.5m: one missed bar before the 15m horizon
	}, base)

	if err := f.handleJob(t, decision.ID, 15); err != nil {
		t.Fatalf("HandleJob: %v", err)
	}
	o := f.getOutcome(t, decision.ID, 15)
	if !almostEqual(o.FutureReturn, 3.0) {
		t.Fatalf("FutureReturn = %v, want ~3.0", o.FutureReturn)
	}
}

func countSkips(t *testing.T, f labelerFixtures, decisionID int64, horizonMinutes int) int {
	t.Helper()
	var n int
	if err := f.db.QueryRow(
		`SELECT COUNT(*) FROM calibration_label_skips WHERE jev_decision_id = ? AND horizon_minutes = ?`,
		decisionID, horizonMinutes,
	).Scan(&n); err != nil {
		t.Fatalf("count calibration label skips: %v", err)
	}
	return n
}

func pendingPairs(t *testing.T, f labelerFixtures, asOf time.Time) map[[2]int64]bool {
	t.Helper()
	pending, err := f.outcomes.PendingLabels(context.Background(), []int{5, 15}, asOf.Add(-24*time.Hour), asOf)
	if err != nil {
		t.Fatalf("PendingLabels: %v", err)
	}
	out := make(map[[2]int64]bool)
	for _, p := range pending {
		out[[2]int64{p.JevDecisionID, int64(p.HorizonMinutes)}] = true
	}
	return out
}

// A window that falls short of the horizon is deferred (job back to
// pending, never failed) while the data may still be landing, but becomes
// a permanent skip (job succeeded with a skipped note) once
// horizonDataGrace has passed: no calibration_outcomes row (no shortened
// horizon), a skip marker instead, and PendingLabels stops returning the
// pair (issues #481, #710).
func TestLabeler_HandleJob_ShortWindowIsTransientThenPermanentlySkipped(t *testing.T) {
	f := newLabelerFixtures(t)
	base := time.Date(2026, 9, 28, 15, 25, 0, 0, time.FixedZone("JST", 9*3600))
	decision := f.insertDecision(t, base, domain.JevDirectionLong, 0.82)
	f.insertSnapshots(t, map[time.Duration]float64{
		0:               1000,
		5 * time.Minute: 1020, // 15:30 close bar
	}, base)
	horizonEnd := base.Add(15 * time.Minute)
	key := [2]int64{decision.ID, 15}

	// Within the grace period: error, nothing persisted, still pending.
	*f.now = horizonEnd.Add(4 * time.Minute)
	err := f.handleJob(t, decision.ID, 15)
	var deferred *jobqueue.DeferredError
	if !errors.As(err, &deferred) {
		t.Fatalf("HandleJob within the data grace period error = %v, want jobqueue.Defer (pending, not a failure)", err)
	}
	if want := f.now.Add(time.Minute); !deferred.RetryAt.Equal(want) {
		t.Fatalf("deferred RetryAt = %v, want %v", deferred.RetryAt, want)
	}
	if n := countSkips(t, f, decision.ID, 15); n != 0 {
		t.Fatalf("skip markers within grace = %d, want 0", n)
	}
	if !pendingPairs(t, f, *f.now)[key] {
		t.Fatalf("pair not pending within the grace period, want it re-enqueued by the next scan")
	}

	// Past the grace period: success, skip marker, no outcome, not pending.
	*f.now = horizonEnd.Add(5 * time.Minute)
	if err := f.handleJob(t, decision.ID, 15); !isSkipped(err) {
		t.Fatalf("HandleJob past the grace period: %v, want jobqueue.Skip (permanent skip, not a failure)", err)
	}
	if n := countOutcomes(t, f, decision.ID, 15); n != 0 {
		t.Fatalf("calibration_outcomes rows = %d, want 0 (no shortened horizon)", n)
	}
	if n := countSkips(t, f, decision.ID, 15); n != 1 {
		t.Fatalf("skip markers = %d, want 1", n)
	}
	if pendingPairs(t, f, *f.now)[key] {
		t.Fatalf("permanently skipped pair is still pending, want it excluded")
	}
	// Re-running a skipped pair is idempotent.
	if err := f.handleJob(t, decision.ID, 15); !isSkipped(err) {
		t.Fatalf("HandleJob on an already-skipped pair: %v, want jobqueue.Skip", err)
	}
	if n := countSkips(t, f, decision.ID, 15); n != 1 {
		t.Fatalf("skip markers after rerun = %d, want 1", n)
	}

	// The fully covered 5-minute horizon is unaffected: still labeled.
	if !pendingPairs(t, f, *f.now)[[2]int64{decision.ID, 5}] {
		t.Fatalf("5m horizon not pending, want it still to be labeled")
	}
	if err := f.handleJob(t, decision.ID, 5); err != nil {
		t.Fatalf("HandleJob(5m horizon): %v", err)
	}
	if n := countOutcomes(t, f, decision.ID, 5); n != 1 {
		t.Fatalf("calibration_outcomes rows for 5m = %d, want 1", n)
	}
}

// A decision whose entry bar never landed has no window at all; past the
// grace period it is skipped rather than retried forever.
func TestLabeler_HandleJob_MissingEntryBarIsSkippedAfterGrace(t *testing.T) {
	f := newLabelerFixtures(t)
	base := time.Date(2026, 9, 27, 9, 0, 0, 0, time.UTC)
	decision := f.insertDecision(t, base, domain.JevDirectionLong, 0.82)
	f.insertSnapshots(t, map[time.Duration]float64{
		1 * time.Minute: 1000,
		5 * time.Minute: 1010,
	}, base)

	*f.now = base.Add(10 * time.Minute)
	if err := f.handleJob(t, decision.ID, 5); !isSkipped(err) {
		t.Fatalf("HandleJob error = %v, want jobqueue.Skip (permanent skip, not a failure)", err)
	}
	if n := countSkips(t, f, decision.ID, 5); n != 1 {
		t.Fatalf("skip markers = %d, want 1", n)
	}
	if n := countOutcomes(t, f, decision.ID, 5); n != 0 {
		t.Fatalf("calibration_outcomes rows = %d, want 0", n)
	}
}
