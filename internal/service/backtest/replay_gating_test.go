package backtest_test

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/backtest"
)

// shortThresholds returns a RunConfig whose Thresholds also admit SHORT
// (testThresholds has only a LONG boundary).
func shortThresholds() backtest.RunConfig {
	cfg := backtest.RunConfig{Thresholds: testThresholds()}
	cfg.Thresholds.Policy.Short = cfg.Thresholds.Policy.Long
	return cfg
}

func shortDecision(instrumentID int64, ts time.Time) domain.JevDecision {
	d := longDecision(instrumentID, ts)
	d.Direction = ptr(domain.JevDirectionShort)
	return d
}

// flaggedTrades replays one decision at the first bar of a ramp whose
// every bar is marked by mark, with direction/threshold set by the
// caller's decision and cfg.
func flaggedTrades(t *testing.T, decision domain.JevDecision, mark func(*domain.Snapshot)) []backtest.Trade {
	t.Helper()
	const instrumentID = int64(1)
	base := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	bars := rampBars(instrumentID, base, 12, 2000, 1.005, 10)
	for i := range bars {
		mark(&bars[i])
	}
	decision.InstrumentID = instrumentID
	decision.Timestamp = base
	cfg := shortThresholds()
	cfg.InstrumentID = instrumentID
	cfg.Symbol = "7203"
	cfg.Snapshots = bars
	cfg.Decisions = backtest.NewSliceDecisionSource([]domain.JevDecision{decision})
	cfg.Exit = backtest.ExitRule{StopLossPct: 5, TakeProfitPct: 1.0, MaxHolding: 10 * time.Minute}
	period := backtest.Period{Start: base, End: base.Add(12 * time.Minute)}
	trades, err := backtest.ShadowBacktestTrades(context.Background(), cfg, period)
	if err != nil {
		t.Fatalf("ShadowBacktestTrades: %v", err)
	}
	return trades
}

// TestReplay_EntryEligibilityFlagsMatchLive is issue #595: replay feeds
// the Policy Engine the same SpecialQuote/PriceLimit/Lendable the live
// Handler does, so a bar the live pipeline rejects is not replayed.
func TestReplay_EntryEligibilityFlagsMatchLive(t *testing.T) {
	long := longDecision(1, time.Time{})
	short := shortDecision(1, time.Time{})

	if got := flaggedTrades(t, long, func(*domain.Snapshot) {}); len(got) != 1 {
		t.Fatalf("baseline LONG trades = %d, want 1", len(got))
	}
	if got := flaggedTrades(t, long, func(s *domain.Snapshot) { s.SpecialQuote = true }); len(got) != 0 {
		t.Errorf("special-quote bars: trades = %d, want 0", len(got))
	}
	for _, limit := range []domain.PriceLimit{domain.PriceLimitUp, domain.PriceLimitDown} {
		if got := flaggedTrades(t, long, func(s *domain.Snapshot) { s.PriceLimit = limit }); len(got) != 0 {
			t.Errorf("price limit %q bars: trades = %d, want 0", limit, len(got))
		}
	}

	notLendable := func(s *domain.Snapshot) { s.Lendable = ptr(false) }
	if got := flaggedTrades(t, short, notLendable); len(got) != 0 {
		t.Errorf("non-lendable SHORT: trades = %d, want 0 (not_lendable)", len(got))
	}
	if got := flaggedTrades(t, long, notLendable); len(got) != 1 {
		t.Errorf("non-lendable LONG: trades = %d, want 1 (Lendable blocks only SHORT)", len(got))
	}
	if got := flaggedTrades(t, short, func(s *domain.Snapshot) { s.Lendable = ptr(true) }); len(got) != 1 {
		t.Errorf("lendable SHORT: trades = %d, want 1", len(got))
	}
}

// TestRun_DecisionBeforeWindowIsNotReusedAcrossWindows is issue #596:
// one decision recorded inside Validation must not re-enter in Forward.
func TestRun_DecisionBeforeWindowIsNotReusedAcrossWindows(t *testing.T) {
	const instrumentID = int64(1)
	base := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	bars := rampBars(instrumentID, base, 70, 2000, 1.0001, 10)

	cfg := shortThresholds()
	cfg.InstrumentID = instrumentID
	cfg.Symbol = "7203"
	cfg.Snapshots = bars
	cfg.Decisions = backtest.NewSliceDecisionSource([]domain.JevDecision{
		longDecision(instrumentID, base.Add(25*time.Minute)), // inside Validation
	})
	cfg.Exit = backtest.ExitRule{MaxHolding: 5 * time.Minute}
	wf := backtest.WalkForwardConfig{
		Start: base, End: base.Add(60 * time.Minute),
		TrainingPeriod: 20 * time.Minute, ValidationPeriod: 20 * time.Minute, ForwardPeriod: 20 * time.Minute,
	}

	result, err := backtest.Run(context.Background(), cfg, wf)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(result.Splits) != 1 {
		t.Fatalf("len(Splits) = %d, want 1", len(result.Splits))
	}
	sp := result.Splits[0]
	if sp.Validation.TradeCount != 1 {
		t.Errorf("Validation.TradeCount = %d, want 1 (decision recorded in the window is evaluated once)", sp.Validation.TradeCount)
	}
	if sp.Forward.TradeCount != 0 || result.Combined.TradeCount != 0 {
		t.Errorf("Forward/Combined trades = %d/%d, want 0/0 (a pre-window decision is not re-entered)",
			sp.Forward.TradeCount, result.Combined.TradeCount)
	}
}

// TestReplay_DoesNotLogTradeSignalDecided is issue #602: replay calls
// the Policy Engine on every bar and must not emit the production
// "Signal count" log line for any of them.
func TestReplay_DoesNotLogTradeSignalDecided(t *testing.T) {
	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	const instrumentID = int64(1)
	base := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	bars := rampBars(instrumentID, base, 30, 2000, 1.005, 10)
	trades := shadowTrades(t, instrumentID, bars, []domain.JevDecision{longDecision(instrumentID, base)})
	if len(trades) == 0 {
		t.Fatal("fixture produced no trade; the replay path was not exercised")
	}
	if strings.Contains(buf.String(), "trade signal decided") {
		t.Errorf("replay emitted 'policy: trade signal decided' lines:\n%s", buf.String())
	}
}
