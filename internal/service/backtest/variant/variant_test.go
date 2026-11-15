package variant_test

import (
	"errors"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/service/backtest"
	"github.com/ousiassllc/pitha-trador/internal/service/backtest/simrisk"
	"github.com/ousiassllc/pitha-trador/internal/service/backtest/variant"
)

func baseConfigs() []backtest.RunConfig {
	risk := &simrisk.Config{StopLossPct: 1, Limits: config.RiskLimits{MaxTradeLossPct: 1, MaxPositionPerSymbolPct: 20}}
	var cfgs []backtest.RunConfig
	for id := int64(1); id <= 2; id++ {
		c := backtest.RunConfig{InstrumentID: id, Risk: risk}
		c.Thresholds.Policy.Long.MinEntryQuality = "strong"
		c.Exit.StopLossPct = 1
		c.Exit.MaxHolding = 30 * time.Minute
		cfgs = append(cfgs, c)
	}
	return cfgs
}

func TestApplyOverridesOnlyTheCopies(t *testing.T) {
	cfgs := baseConfigs()
	p := variant.Params{
		MinEntryQualityLong: new("good"), StopLossPct: new(0.5), MaxHoldingMinutes: new(10),
		SlippageBps: new(3.0), MaxTradeLossPct: new(0.5), EntryRule: new(variant.EntryRuleLegacyConfidence),
	}
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	got := p.Apply(cfgs)

	if q := cfgs[0].Thresholds.Policy.Long.MinEntryQuality; q != "strong" || cfgs[0].Exit.StopLossPct != 1 || cfgs[0].Risk.Limits.MaxTradeLossPct != 1 {
		t.Fatalf("Apply modified its input: %+v", cfgs[0])
	}
	for _, c := range got {
		if c.Thresholds.Policy.Long.MinEntryQuality != "good" || c.Exit.StopLossPct != 0.5 || c.Exit.MaxHolding != 10*time.Minute ||
			c.Cost.Long.SlippageBps != 3 || c.Cost.Short.SlippageBps != 3 || c.Entry != backtest.EntryLegacyConfidence {
			t.Errorf("override not applied: %+v", c)
		}
	}
	if got[0].Risk != got[1].Risk {
		t.Error("the configs of one portfolio must share one Risk config")
	}
	if r := got[0].Risk; r.StopLossPct != 0.5 || r.Limits.MaxTradeLossPct != 0.5 || r.Limits.MaxPositionPerSymbolPct != 20 {
		t.Errorf("risk = %+v, want the stop loss and trade loss overridden only", r)
	}
}

func TestBaselineKeepsTheSourcesRisk(t *testing.T) {
	cfgs := baseConfigs()
	got := variant.Params{}.Apply(cfgs)
	if got[0].Risk != cfgs[0].Risk || !(variant.Params{}).IsBaseline() {
		t.Error("an empty Params must change nothing")
	}
}

func TestValidateRefusesOutOfRange(t *testing.T) {
	for name, p := range map[string]variant.Params{
		"entry_rule":                  {EntryRule: new("x")},
		"min_entry_quality_short":     {MinEntryQualityShort: new("great")},
		"min_probability_long":        {MinProbabilityLong: new(0.0)},
		"stop_loss_pct":               {StopLossPct: new(-1.0)},
		"fee_bps":                     {FeeBps: new(201.0)},
		"max_trade_loss_pct":          {MaxTradeLossPct: new(5.0)},
		"max_holding_minutes":         {MaxHoldingMinutes: new(2000)},
		"max_position_per_symbol_pct": {MaxPositionPerSymbolPct: new(0.0)},
	} {
		var inv *variant.InvalidError
		if err := p.Validate(); !errors.As(err, &inv) || inv.Field != name {
			t.Errorf("%s: Validate = %v, want InvalidError for that field", name, err)
		}
	}
}

func TestCanonicalIsStableAndOmitsUnset(t *testing.T) {
	a := variant.Params{StopLossPct: new(0.5), MinEntryQualityLong: new("good")}.Canonical()
	b := variant.Params{MinEntryQualityLong: new("good"), StopLossPct: new(0.5)}.Canonical()
	if a != b || a != `{"min_entry_quality_long":"good","stop_loss_pct":0.5}` {
		t.Errorf("canonical = %s / %s", a, b)
	}
	if got := (variant.Params{}).Canonical(); got != "{}" {
		t.Errorf("empty canonical = %s", got)
	}
}
