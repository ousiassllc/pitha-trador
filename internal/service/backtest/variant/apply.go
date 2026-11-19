package variant

import (
	"slices"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/backtest"
	"github.com/ousiassllc/pitha-trador/internal/service/backtest/simrisk"
)

// Apply returns copies of cfgs with p laid over them. cfgs themselves (and the
// Risk config, thresholds and cost models they hold) are not modified, so one
// loaded set of RunConfigs can be replayed under many Params. The returned
// configs of one call share one simulated Risk config, as a portfolio replay
// requires. p must have passed Validate.
func (p Params) Apply(cfgs []backtest.RunConfig) []backtest.RunConfig {
	out := slices.Clone(cfgs)
	risk := sharedRisk(p, cfgs)
	for i := range out {
		c := &out[i]
		if p.EntryRule != nil {
			c.Entry = backtest.EntryMeta
			if *p.EntryRule == EntryRuleLegacyConfidence {
				c.Entry = backtest.EntryLegacyConfidence
			}
		}
		if p.MinEVBps != nil {
			c.Meta.MinEVBps = *p.MinEVBps
		}
		long, short := &c.Thresholds.Policy.Long, &c.Thresholds.Policy.Short
		set(&long.MinEntryQuality, p.MinEntryQualityLong)
		set(&short.MinEntryQuality, p.MinEntryQualityShort)
		set(&long.MinProbability, p.MinProbabilityLong)
		set(&short.MinProbability, p.MinProbabilityShort)
		set(&long.MinContinuationProbability, p.MinContinuationProbabilityLong)
		set(&short.MinContinuationProbability, p.MinContinuationProbabilityShort)

		set(&c.Exit.StopLossPct, p.StopLossPct)
		set(&c.Exit.TakeProfitPct, p.TakeProfitPct)
		set(&c.Exit.TrailingStopPct, p.TrailingStopPct)
		if p.MaxHoldingMinutes != nil {
			c.Exit.MaxHolding = time.Duration(*p.MaxHoldingMinutes) * time.Minute
		}
		set(&c.Cost.Long.SlippageBps, p.SlippageBps)
		set(&c.Cost.Short.SlippageBps, p.SlippageBps)
		set(&c.Cost.Long.FeeBps, p.FeeBps)
		set(&c.Cost.Short.FeeBps, p.FeeBps)
		c.Risk = risk
	}
	return out
}

// sharedRisk is the Risk config every returned RunConfig carries: the source's
// own pointer when p changes none of its values, else one modified copy.
// StopLossPct is the position sizing's stop distance, so it follows the exit's.
func sharedRisk(p Params, cfgs []backtest.RunConfig) *simrisk.Config {
	if len(cfgs) == 0 || cfgs[0].Risk == nil {
		return nil
	}
	if p.StopLossPct == nil && p.MaxTradeLossPct == nil && p.MaxPositionPerSymbolPct == nil {
		return cfgs[0].Risk
	}
	r := *cfgs[0].Risk
	set(&r.StopLossPct, p.StopLossPct)
	set(&r.Limits.MaxTradeLossPct, p.MaxTradeLossPct)
	set(&r.Limits.MaxPositionPerSymbolPct, p.MaxPositionPerSymbolPct)
	return &r
}

// set stores *v in dst when v is given.
func set[T any](dst *T, v *T) {
	if v != nil {
		*dst = *v
	}
}
