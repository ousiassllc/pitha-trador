package calibration_test

import (
	"context"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/calibration"
)

func TestCalibrationRepository_EachLabelByDecisionRange_OnlyDecisionsInPeriod(t *testing.T) {
	ctx := context.Background()
	repo, decisions, instID := newCalibrationFixtures(t)
	base := time.Date(2026, 9, 28, 0, 30, 0, 0, time.UTC)
	inRange := insertTraderDecision(t, decisions, instID, base, domain.JevDirectionLong)
	outOfRange := insertTraderDecision(t, decisions, instID, base.Add(time.Hour), domain.JevDirectionLong)

	for _, d := range []domain.JevDecision{inRange, outOfRange} {
		for _, h := range []int{15, 5} {
			if _, err := repo.Insert(ctx, domain.CalibrationOutcome{
				JevDecisionID: d.ID, HorizonMinutes: h, FutureReturn: 0.01, LabelVersion: 1,
			}); err != nil {
				t.Fatalf("insert outcome: %v", err)
			}
		}
		if err := repo.MarkUnlabelable(ctx, d.ID, 30, "lunch break"); err != nil {
			t.Fatalf("mark unlabelable: %v", err)
		}
	}

	var outcomes []domain.CalibrationOutcome
	if err := repo.EachOutcomeByDecisionRange(ctx, base, base.Add(30*time.Minute), func(o domain.CalibrationOutcome) error {
		outcomes = append(outcomes, o)
		return nil
	}); err != nil {
		t.Fatalf("EachOutcomeByDecisionRange: %v", err)
	}
	if len(outcomes) != 2 || outcomes[0].JevDecisionID != inRange.ID || outcomes[0].HorizonMinutes != 5 || outcomes[1].HorizonMinutes != 15 {
		t.Fatalf("outcomes = %+v, want the in-range decision's horizons 5 then 15", outcomes)
	}

	var skips []calibration.LabelSkip
	if err := repo.EachLabelSkipByDecisionRange(ctx, base, base.Add(30*time.Minute), func(s calibration.LabelSkip) error {
		skips = append(skips, s)
		return nil
	}); err != nil {
		t.Fatalf("EachLabelSkipByDecisionRange: %v", err)
	}
	if len(skips) != 1 || skips[0].JevDecisionID != inRange.ID || skips[0].HorizonMinutes != 30 || skips[0].Reason != "lunch break" || skips[0].CreatedAt.IsZero() {
		t.Fatalf("skips = %+v, want the in-range decision's skip mark", skips)
	}
}
