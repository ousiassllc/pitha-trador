package execution

import (
	"encoding/json"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// traderResponseFields mirrors internal/service/jev.TraderResponse's JSON
// shape for the fields repository.DecisionRepository does not decode into
// queryable columns (regime/entry_quality/toxic_flow/liquidity_stressed/
// continuation_probability - jev_decisions has no such columns; see
// doc.go). Declared locally instead of importing internal/service/jev to
// keep this package's dependency on repository/domain only
// (docs/architecture/overview.md §3 layer rule: service depends on
// domain/repository, not on a sibling service package).
type traderResponseFields struct {
	Regime                  string  `json:"regime"`
	EntryQuality            string  `json:"entry_quality"`
	ToxicFlow               float64 `json:"toxic_flow"`
	LiquidityStressed       float64 `json:"liquidity_stressed"`
	ContinuationProbability float64 `json:"continuation_probability"`
}

// EnrichDecision fills in Regime/EntryQuality/ToxicFlow/
// LiquidityStressed/ContinuationProbability on a
// repository.DecisionRepository-read domain.JevDecision by parsing its
// stored ResponseJSON, for callers (EvaluateExit, Symbol Detail's API
// handler) that need those fields from a decision read back on a later
// request rather than the same in-memory value
// internal/service/jev.Trader.Evaluate originally returned.
//
// It returns decision unchanged if DecisionType is not
// domain.JevDecisionTypeTrader or ResponseJSON fails to parse (a
// malformed/legacy row must not make an otherwise-valid decision
// unusable - FR-EXIT-3's "Jev API不応答時も...継続動作" spirit extends to
// a bad stored response too).
func EnrichDecision(decision domain.JevDecision) domain.JevDecision {
	if decision.DecisionType != domain.JevDecisionTypeTrader || decision.ResponseJSON == "" {
		return decision
	}

	var resp traderResponseFields
	if err := json.Unmarshal([]byte(decision.ResponseJSON), &resp); err != nil {
		return decision
	}

	if decision.Regime == nil {
		decision.Regime = &resp.Regime
	}
	if decision.EntryQuality == nil {
		decision.EntryQuality = &resp.EntryQuality
	}
	if decision.ToxicFlow == nil {
		decision.ToxicFlow = &resp.ToxicFlow
	}
	if decision.LiquidityStressed == nil {
		decision.LiquidityStressed = &resp.LiquidityStressed
	}
	if decision.ContinuationProbability == nil {
		decision.ContinuationProbability = &resp.ContinuationProbability
	}
	return decision
}
