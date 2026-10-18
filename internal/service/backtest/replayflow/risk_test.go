package replayflow_test

import (
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/backtest"
	"github.com/ousiassllc/pitha-trador/internal/service/backtest/simrisk"
)

func riskLimits() config.RiskLimits {
	return config.RiskLimits{
		InitialCapital: 100_000_000, MaxPositionPerSymbolPct: 10, MaxTotalExposurePct: 80,
		MaxDailyLossPct: 5, MaxTradeLossPct: 0.5, MaxOpenPositions: 2, MaxSameDirectionPositions: 2,
		MarketAdverseReturn5mPct: 0.3, MaxSpreadBps: 30, MaxConsecutiveLosses: 3, CooldownAfterLossMinutes: 5,
	}
}

// fiveInstruments are five instruments that all get a LONG decision at minute 1
// and hold a flat price for the whole window.
func fiveInstruments(risk *simrisk.Config) []backtest.RunConfig {
	var cfgs []backtest.RunConfig
	for id := int64(1); id <= 5; id++ {
		bars := minuteBars(id, base, []float64{1000, 1000, 1000, 1000, 1000, 1000, 1000, 1000}, nil)
		cfg := replayConfig(id, "S"+string(rune('0'+id)), bars, []domain.JevDecision{longDecision(id, at(1))}, liveExit())
		cfg.Risk = risk
		cfgs = append(cfgs, cfg)
	}
	return cfgs
}

func TestRiskReplay_NeverExceedsMaxOpenPositions(t *testing.T) {
	limits := riskLimits()
	limits.MaxSameDirectionPositions = 10 // only max_open_positions binds
	trades := replayTrades(t, fiveInstruments(&simrisk.Config{Limits: limits, StopLossPct: 1})...)
	if len(trades) != 2 {
		t.Fatalf("%d trades, want 2: max_open_positions=2 admits two of the five simultaneous entries", len(trades))
	}
	if got := maxConcurrent(trades); got > limits.MaxOpenPositions {
		t.Fatalf("max concurrent positions = %d, exceeds max_open_positions=%d", got, limits.MaxOpenPositions)
	}
}

func TestRiskReplay_NeverExceedsSameDirectionCap(t *testing.T) {
	limits := riskLimits()
	limits.MaxOpenPositions = 10 // only max_same_direction_positions binds
	limits.MaxSameDirectionPositions = 1
	trades := replayTrades(t, fiveInstruments(&simrisk.Config{Limits: limits, StopLossPct: 1})...)
	if len(trades) != 1 {
		t.Fatalf("%d trades, want 1: same-direction cap 1", len(trades))
	}
	if got := maxConcurrent(trades); got > limits.MaxSameDirectionPositions {
		t.Fatalf("max concurrent same-direction positions = %d, exceeds the cap %d", got, limits.MaxSameDirectionPositions)
	}
}

func TestRiskReplay_WithoutARiskEngineAllFiveEnter(t *testing.T) {
	if got := replayTrades(t, fiveInstruments(nil)...); len(got) != 5 || maxConcurrent(got) != 5 {
		t.Fatalf("%d trades, max concurrent %d; want 5 and 5 (the explicit no-Risk comparison mode)", len(got), maxConcurrent(got))
	}
}

func TestRiskReplay_CapFreesWhenAPositionCloses(t *testing.T) {
	// One slot: the first instrument holds 3 minutes (max holding); the second
	// instrument's decision at minute 10 finds the slot free again (and the
	// portfolio-wide 5-minute cooldown after A's losing close over).
	limits := riskLimits()
	limits.MaxOpenPositions = 1
	risk := &simrisk.Config{Limits: limits, StopLossPct: 1}
	exit := liveExit()
	exit.MaxHolding = 3 * time.Minute
	flat := []float64{1000, 1000, 1000, 1000, 1000, 1000, 1000, 1000, 1000, 1000, 1000, 1000, 1000, 1000}
	a := replayConfig(1, "A", minuteBars(1, base, flat, nil), []domain.JevDecision{longDecision(1, at(1))}, exit)
	b := replayConfig(2, "B", minuteBars(2, base, flat, nil), []domain.JevDecision{longDecision(2, at(2)), longDecision(2, at(10))}, exit)
	a.Risk, b.Risk = risk, risk

	trades := replayTrades(t, a, b)
	if len(trades) != 2 || trades[1].Symbol != "B" || !trades[1].EntryTimestamp.Equal(at(10)) {
		t.Fatalf("trades = %+v, want A at minute 1 then B at minute 10 (its minute-2 decision was rejected)", trades)
	}
	if got := maxConcurrent(trades); got != 1 {
		t.Fatalf("max concurrent = %d, want 1", got)
	}
}

func TestRiskReplay_SizesThePosition(t *testing.T) {
	limits := riskLimits()
	trades := replayTrades(t, fiveInstruments(&simrisk.Config{Limits: limits, StopLossPct: 1})...)
	for _, tr := range trades {
		if tr.Quantity <= 0 || tr.Quantity%100 != 0 {
			t.Errorf("%s: Quantity = %d, want a positive multiple of the 100-share lot (sizing.Quantity)", tr.Symbol, tr.Quantity)
		}
	}
	if got := replayTrades(t, fiveInstruments(nil)...); got[0].Quantity != 0 {
		t.Errorf("without a Risk Engine Quantity = %d, want 0", got[0].Quantity)
	}
}

func TestRiskReplay_ConsecutiveLossesStopEntriesUntilTheNextDay(t *testing.T) {
	limits := riskLimits()
	limits.MaxConsecutiveLosses = 2
	limits.CooldownAfterLossMinutes = 0
	risk := &simrisk.Config{Limits: limits, StopLossPct: 1}
	exit := liveExit()
	exit.CooldownAfterLoss = 0
	// Stop-outs at minutes 2 and 4, then a decision at minute 6 (same day) that must
	// be rejected by the loss streak.
	prices := []float64{1000, 1000, 985, 1000, 985, 1000, 1000, 1000, 1000}
	decisions := []domain.JevDecision{longDecision(1, at(1)), longDecision(1, at(3)), longDecision(1, at(6))}
	cfg := replayConfig(1, "7203", minuteBars(1, base, prices, nil), decisions, exit)
	cfg.Risk = risk

	trades := replayTrades(t, cfg)
	if len(trades) != 2 {
		t.Fatalf("%d trades, want 2: the third entry follows two consecutive losses", len(trades))
	}
}
