package enrich_test

import (
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/execution/enrich"
)

func TestDecision_ParsesResponseJSONFieldsForTraderDecision(t *testing.T) {
	direction := domain.JevDirectionLong
	decision := domain.JevDecision{
		DecisionType: domain.JevDecisionTypeTrader,
		Direction:    &direction, // already round-tripped by repository.DecisionRepository
		ResponseJSON: `{
			"direction": "LONG", "regime": "BREAKOUT", "entry_quality": "strong",
			"toxic_flow": 0.18, "liquidity_stressed": 0.09, "continuation_probability": 0.72,
			"confidence": 0.74, "model_id": "m1"
		}`,
	}

	enriched := enrich.Decision(decision)

	if enriched.Regime == nil || *enriched.Regime != "BREAKOUT" {
		t.Fatalf("Decision().Regime = %v, want BREAKOUT", enriched.Regime)
	}
	if enriched.EntryQuality == nil || *enriched.EntryQuality != domain.JevEntryQualityStrong {
		t.Fatalf("Decision().EntryQuality = %v, want %q", enriched.EntryQuality, domain.JevEntryQualityStrong)
	}
	if enriched.ToxicFlow == nil || *enriched.ToxicFlow != 0.18 {
		t.Fatalf("Decision().ToxicFlow = %v, want 0.18", enriched.ToxicFlow)
	}
	if enriched.LiquidityStressed == nil || *enriched.LiquidityStressed != 0.09 {
		t.Fatalf("Decision().LiquidityStressed = %v, want 0.09", enriched.LiquidityStressed)
	}
	if enriched.ContinuationProbability == nil || *enriched.ContinuationProbability != 0.72 {
		t.Fatalf("Decision().ContinuationProbability = %v, want 0.72", enriched.ContinuationProbability)
	}
}

func TestDecision_LeavesNonTraderDecisionUnchanged(t *testing.T) {
	decision := domain.JevDecision{DecisionType: domain.JevDecisionTypeScout, ResponseJSON: `{"interesting_now":0.7}`}

	enriched := enrich.Decision(decision)

	if enriched.Regime != nil || enriched.ContinuationProbability != nil {
		t.Fatalf("Decision(scout decision) = %+v, want unchanged (no regime/continuation_probability fields)", enriched)
	}
}

func TestDecision_LeavesDecisionUnchangedOnMalformedJSON(t *testing.T) {
	decision := domain.JevDecision{DecisionType: domain.JevDecisionTypeTrader, ResponseJSON: `not json`}

	enriched := enrich.Decision(decision)

	if enriched.Regime != nil {
		t.Fatalf("Decision(malformed json) = %+v, want unchanged (Regime still nil)", enriched)
	}
}
