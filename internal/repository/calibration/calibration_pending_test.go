package calibration_test

import (
	"context"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// PendingLabels only looks at decisions newer than since: an unlabeled,
// unmarked decision older than the retry window must not come back, while
// one inside it still does and labeled/marked pairs stay excluded
// (issue #484, FR-CAL-4).
func TestCalibrationRepository_PendingLabelsExcludesDecisionsOlderThanSince(t *testing.T) {
	outcomes, decisions, instID := newCalibrationFixtures(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 28, 9, 0, 0, 0, time.UTC)
	since := now.Add(-24 * time.Hour)

	stale := insertTraderDecision(t, decisions, instID, now.Add(-25*time.Hour), domain.JevDirectionLong)
	atSince := insertTraderDecision(t, decisions, instID, since, domain.JevDirectionLong)
	recent := insertTraderDecision(t, decisions, instID, now.Add(-23*time.Hour), domain.JevDirectionLong)
	labeled := insertTraderDecision(t, decisions, instID, now.Add(-22*time.Hour), domain.JevDirectionLong)
	skipped := insertTraderDecision(t, decisions, instID, now.Add(-21*time.Hour), domain.JevDirectionLong)
	if _, err := outcomes.Insert(ctx, domain.CalibrationOutcome{
		JevDecisionID: labeled.ID, HorizonMinutes: 5, FutureReturn: 0.1, WasDirectionCorrect: ptr(true),
	}); err != nil {
		t.Fatalf("seed outcome: %v", err)
	}
	if err := outcomes.MarkUnlabelable(ctx, skipped.ID, 5, "close"); err != nil {
		t.Fatalf("MarkUnlabelable: %v", err)
	}

	pending, err := outcomes.PendingLabels(ctx, []int{5}, since, now)
	if err != nil {
		t.Fatalf("PendingLabels: %v", err)
	}
	if len(pending) != 1 || pending[0].JevDecisionID != recent.ID {
		t.Fatalf("PendingLabels = %+v, want only recent decision %d (not stale %d, atSince %d, labeled %d, skipped %d)",
			pending, recent.ID, stale.ID, atSince.ID, labeled.ID, skipped.ID)
	}
}
