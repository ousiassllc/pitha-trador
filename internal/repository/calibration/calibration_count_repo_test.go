package calibration_test

import (
	"context"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

func TestCalibrationRepository_CountLabeledSamplesInConfidenceRange(t *testing.T) {
	outcomes, decisions, instID := newCalibrationFixtures(t)
	ctx := context.Background()
	now := time.Now().UTC()

	seed := func(confidence float64, direction string, offset int) {
		t.Helper()
		dir := direction
		d, err := decisions.Insert(ctx, domain.JevDecision{
			InstrumentID: instID, Symbol: "7203", Timestamp: now.Add(time.Duration(offset) * time.Minute),
			DecisionType: domain.JevDecisionTypeTrader, StateHash: "h", StateJSON: "{}", QuestionVersion: "v1",
			ResponseJSON: "{}", Direction: &dir, Confidence: &confidence, ModelID: "m",
		})
		if err != nil {
			t.Fatalf("insert decision: %v", err)
		}
		var correct *bool
		if direction != domain.JevDirectionNone {
			correct = ptr(true)
		}
		for _, horizon := range []int{5, 20} {
			if _, err := outcomes.Insert(ctx, domain.CalibrationOutcome{JevDecisionID: d.ID, HorizonMinutes: horizon, WasDirectionCorrect: correct}); err != nil {
				t.Fatalf("insert outcome: %v", err)
			}
		}
	}
	seed(0.60, domain.JevDirectionLong, 0)  // lower bound is inclusive
	seed(0.65, domain.JevDirectionShort, 1) // inside
	seed(0.70, domain.JevDirectionLong, 2)  // upper bound is exclusive
	seed(0.65, domain.JevDirectionNone, 3)  // NONE is never a sample

	tests := []struct {
		name        string
		low, high   float64
		includeHigh bool
		limit       int
		want        int
	}{
		{"half-open range", 0.60, 0.70, false, 100, 4},
		{"closed upper bound", 0.60, 0.70, true, 100, 6},
		{"empty range", 0.80, 0.90, false, 100, 0},
		{"limit = count+1", 0.60, 0.70, false, 5, 4},
		{"limit = count", 0.60, 0.70, false, 4, 4},
		{"limit = count-1 caps the count", 0.60, 0.70, false, 3, 3},
		{"limit 1", 0.60, 0.70, true, 1, 1},
		{"non-positive limit", 0.60, 0.70, false, 0, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := outcomes.CountLabeledSamplesInConfidenceRange(ctx, tc.low, tc.high, tc.includeHigh, tc.limit)
			if err != nil {
				t.Fatalf("CountLabeledSamplesInConfidenceRange: %v", err)
			}
			if got != tc.want {
				t.Fatalf("count = %d, want %d", got, tc.want)
			}
		})
	}
}
