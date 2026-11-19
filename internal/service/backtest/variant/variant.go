// Package variant is the set of replay parameters a research run may change:
// the entry rule and its thresholds, the exit conditions, the cost assumption
// and the position size. A Params holds only the overrides (a nil field keeps
// the value the backtest source assembled from the settings in force), so a
// run is described by its difference from the live decision flow, and an empty
// Params is the baseline.
//
// It is shared by the backtest lab (MCP backtest_start, issue #1086) and the
// virtual Paper profiles (issue #1087): both replay stored market_snapshots and
// jev_decisions through backtest.RunConfig with a Params laid over it. Apply
// changes only the copies of the RunConfigs it returns; runtime_settings, the
// live Policy Engine, Risk Engine and Execution are never read or written here.
package variant

import (
	"encoding/json"
	"fmt"
	"math"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// The entry rules a run may name (backtest.EntryMeta is the live rule).
const (
	EntryRuleMeta             = "meta"
	EntryRuleLegacyConfidence = "legacy_confidence"
)

// The accepted ranges of the numeric parameters (inclusive). They are the
// outer bounds of a sensible research grid, not the Settings screen's limits:
// a replay changes nothing live, so the bounds only reject typos and values
// that make the engine meaningless.
const (
	MaxEVBps            = 1000.0
	MaxExitPct          = 100.0
	MaxHoldingMinutes   = 24 * 60
	MaxCostBps          = 200.0
	MinTradeLossPct     = 0.1
	MaxTradeLossPct     = 3.0
	MaxPositionPerSymPc = 100.0
)

// Params are the overrides of one run. Every field is optional.
type Params struct {
	// EntryRule is "meta" (the live rule: the meta-labeling model's verdict) or
	// "legacy_confidence" (Jev's confidence against min_probability, a named
	// comparison mode). Only legacy_confidence reads min_probability_*.
	EntryRule *string `json:"entry_rule,omitempty"`
	// MinEVBps is the expected value (bps) the meta rule's verdict must exceed.
	MinEVBps *float64 `json:"min_ev_bps,omitempty"`

	MinEntryQualityLong  *string `json:"min_entry_quality_long,omitempty"`
	MinEntryQualityShort *string `json:"min_entry_quality_short,omitempty"`
	// MinProbability* are in (0, 1]; MinContinuationProbability* in [0, 1].
	MinProbabilityLong              *float64 `json:"min_probability_long,omitempty"`
	MinProbabilityShort             *float64 `json:"min_probability_short,omitempty"`
	MinContinuationProbabilityLong  *float64 `json:"min_continuation_probability_long,omitempty"`
	MinContinuationProbabilityShort *float64 `json:"min_continuation_probability_short,omitempty"`

	// The exit conditions, in percent (0 disables one) and minutes.
	StopLossPct       *float64 `json:"stop_loss_pct,omitempty"`
	TakeProfitPct     *float64 `json:"take_profit_pct,omitempty"`
	TrailingStopPct   *float64 `json:"trailing_stop_pct,omitempty"`
	MaxHoldingMinutes *int     `json:"max_holding_minutes,omitempty"`

	// SlippageBps / FeeBps replace the continuous-session slippage and the
	// proportional fee of both directions (the rest of the cost model stays).
	SlippageBps *float64 `json:"slippage_bps,omitempty"`
	FeeBps      *float64 `json:"fee_bps,omitempty"`

	// The position size, through the simulated Risk Engine's limits: the largest
	// loss of one trade (% of capital) and the largest position per symbol (%).
	MaxTradeLossPct         *float64 `json:"max_trade_loss_pct,omitempty"`
	MaxPositionPerSymbolPct *float64 `json:"max_position_per_symbol_pct,omitempty"`
}

// InvalidError names the parameter that was refused and why. Reason quotes only
// the accepted range, never a stored value.
type InvalidError struct {
	Field  string
	Reason string
}

func (e *InvalidError) Error() string { return fmt.Sprintf("variant: %s: %s", e.Field, e.Reason) }

// Validate reports the first parameter outside its range, as *InvalidError.
func (p Params) Validate() error {
	if p.EntryRule != nil && *p.EntryRule != EntryRuleMeta && *p.EntryRule != EntryRuleLegacyConfidence {
		return &InvalidError{"entry_rule", "meta / legacy_confidence のどちらかです"}
	}
	for _, q := range []struct {
		field string
		v     *string
	}{{"min_entry_quality_long", p.MinEntryQualityLong}, {"min_entry_quality_short", p.MinEntryQualityShort}} {
		if q.v == nil {
			continue
		}
		if _, ok := domain.EntryQualityRank(*q.v); !ok {
			return &InvalidError{q.field, "poor / fair / good / strong / exceptional のいずれかです"}
		}
	}
	checks := []struct {
		field    string
		v        *float64
		lo, hi   float64
		loOpen   bool
		rangeTxt string
	}{
		{"min_ev_bps", p.MinEVBps, 0, MaxEVBps, false, "0 以上 1000 以下"},
		{"min_probability_long", p.MinProbabilityLong, 0, 1, true, "0 より大きく 1 以下"},
		{"min_probability_short", p.MinProbabilityShort, 0, 1, true, "0 より大きく 1 以下"},
		{"min_continuation_probability_long", p.MinContinuationProbabilityLong, 0, 1, false, "0 以上 1 以下"},
		{"min_continuation_probability_short", p.MinContinuationProbabilityShort, 0, 1, false, "0 以上 1 以下"},
		{"stop_loss_pct", p.StopLossPct, 0, MaxExitPct, false, "0 以上 100 以下（0 は使わない）"},
		{"take_profit_pct", p.TakeProfitPct, 0, MaxExitPct, false, "0 以上 100 以下（0 は使わない）"},
		{"trailing_stop_pct", p.TrailingStopPct, 0, MaxExitPct, false, "0 以上 100 以下（0 は使わない）"},
		{"slippage_bps", p.SlippageBps, 0, MaxCostBps, false, "0 以上 200 以下"},
		{"fee_bps", p.FeeBps, 0, MaxCostBps, false, "0 以上 200 以下"},
		{"max_trade_loss_pct", p.MaxTradeLossPct, MinTradeLossPct, MaxTradeLossPct, false, "0.1 以上 3 以下"},
		{"max_position_per_symbol_pct", p.MaxPositionPerSymbolPct, 0, MaxPositionPerSymPc, true, "0 より大きく 100 以下"},
	}
	for _, c := range checks {
		if c.v == nil {
			continue
		}
		v := *c.v
		if math.IsNaN(v) || math.IsInf(v, 0) || v > c.hi || v < c.lo || (c.loOpen && v <= c.lo) {
			return &InvalidError{c.field, c.rangeTxt}
		}
	}
	if p.MaxHoldingMinutes != nil && (*p.MaxHoldingMinutes < 0 || *p.MaxHoldingMinutes > MaxHoldingMinutes) {
		return &InvalidError{"max_holding_minutes", "0 以上 1440 以下（0 は使わない）"}
	}
	return nil
}

// IsBaseline reports whether p overrides nothing.
func (p Params) IsBaseline() bool { return p == Params{} }

// Canonical is p's JSON with the fixed member order and no unset fields: the
// same overrides always give the same bytes (stored with a run, and the key to
// compare two runs' parameters).
func (p Params) Canonical() string {
	b, err := json.Marshal(p)
	if err != nil { // a struct of pointers to strings, floats and ints cannot fail
		return "{}"
	}
	return string(b)
}
