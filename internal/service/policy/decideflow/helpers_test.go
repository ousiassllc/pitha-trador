package decideflow_test

import (
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/policy"
	"github.com/ousiassllc/pitha-trador/internal/service/policy/policytest"
)

func ptr[T any](v T) *T { return &v }

func testThresholds() policy.Thresholds {
	side := config.PolicyDirectionThresholds{
		MinProbability: 0.68, MinEntryQuality: "strong",
		MinContinuationProbability: 0.60, MaxToxicFlow: 0.35, MaxLiquidityStressed: 0.25,
	}
	return policy.Thresholds{Policy: config.PolicyConfig{Long: side, Short: side}, MaxSpreadBps: 50, MinTurnover5mJPY: 3_000_000}
}

// passingInput is an input for a Jev Trader decision that clears every guard at
// the boundary values (confidence 0.68, toxic_flow 0.35, ...).
func passingInput(direction string) policy.Input {
	return policytest.WithInputs(policy.Input{
		InstrumentID: 1, Symbol: "7203", Timestamp: time.Date(2026, 9, 27, 9, 31, 0, 0, time.UTC),
		Decision: &domain.JevDecision{
			ID: 7, Direction: ptr(direction), EntryQuality: ptr(domain.JevEntryQualityStrong), Confidence: ptr(0.68),
			ContinuationProbability: ptr(0.60), ToxicFlow: ptr(0.35), LiquidityStressed: ptr(0.25),
		},
		SpreadBps: ptr(10.0), EntryPriceReference: ptr(2110.5), Calibrated: true,
	})
}
