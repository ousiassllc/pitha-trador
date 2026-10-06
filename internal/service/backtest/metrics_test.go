package backtest_test

import (
	"math"
	"math/rand"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/backtest"
)

func TestAggregate_Empty(t *testing.T) {
	m := backtest.Aggregate(nil)
	if m.TradeCount != 0 || m.WinRate != 0 || m.ProfitFactor != 0 {
		t.Errorf("Aggregate(nil) = %+v, want zero value", m)
	}
}

// TestAggregate_ComputesFRBT1Metrics exercises every FR-BT-1 metric
// (trade count, win rate, avg profit/loss, Profit Factor, Expectancy,
// Max Drawdown, slippage/fee-inclusive PnL) over a small, hand-checked
// set of trades.
func TestAggregate_ComputesFRBT1Metrics(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	trades := []backtest.Trade{
		{ExitTimestamp: base.Add(1 * time.Hour), GrossReturnPct: 2.0, SlippageReturnPct: 1.8, FeeReturnPct: 1.9, NetReturnPct: 1.7},
		{ExitTimestamp: base.Add(2 * time.Hour), GrossReturnPct: -1.0, SlippageReturnPct: -1.2, FeeReturnPct: -1.1, NetReturnPct: -1.3},
		{ExitTimestamp: base.Add(3 * time.Hour), GrossReturnPct: 3.0, SlippageReturnPct: 2.7, FeeReturnPct: 2.9, NetReturnPct: 2.6},
		{ExitTimestamp: base.Add(4 * time.Hour), GrossReturnPct: -2.0, SlippageReturnPct: -2.3, FeeReturnPct: -2.1, NetReturnPct: -2.4},
	}

	m := backtest.Aggregate(trades)

	if m.TradeCount != 4 {
		t.Errorf("TradeCount = %d, want 4", m.TradeCount)
	}
	if m.WinRate != 0.5 {
		t.Errorf("WinRate = %v, want 0.5", m.WinRate)
	}
	// Net basis (issue #598): wins 1.7/2.6, losses -1.3/-2.4.
	if math.Abs(m.AvgProfit-2.15) > 1e-9 {
		t.Errorf("AvgProfit = %v, want 2.15", m.AvgProfit)
	}
	if math.Abs(m.AvgLoss-(-1.85)) > 1e-9 {
		t.Errorf("AvgLoss = %v, want -1.85", m.AvgLoss)
	}
	// ProfitFactor = sum(net wins)/-sum(net losses) = 4.3 / 3.7.
	if math.Abs(m.ProfitFactor-4.3/3.7) > 1e-9 {
		t.Errorf("ProfitFactor = %v, want %v", m.ProfitFactor, 4.3/3.7)
	}
	// Expectancy = mean net = (1.7-1.3+2.6-2.4)/4 = 0.15.
	if math.Abs(m.Expectancy-0.15) > 1e-9 {
		t.Errorf("Expectancy = %v, want 0.15", m.Expectancy)
	}
	// Net equity curve: 1.7, 0.4, 3.0, 0.6 -> peak 3.0, trough after = 0.6,
	// drawdown = 2.4. Earlier dip (1.7 -> 0.4) is only 1.3, smaller.
	if math.Abs(m.MaxDrawdownPct-2.4) > 1e-9 {
		t.Errorf("MaxDrawdownPct = %v, want 2.4", m.MaxDrawdownPct)
	}
	if math.Abs(m.GrossPnLPct-2.0) > 1e-9 {
		t.Errorf("GrossPnLPct = %v, want 2.0", m.GrossPnLPct)
	}
	if math.Abs(m.SlippagePnLPct-1.0) > 1e-9 {
		t.Errorf("SlippagePnLPct = %v, want 1.0", m.SlippagePnLPct)
	}
	if math.Abs(m.FeePnLPct-1.6) > 1e-9 {
		t.Errorf("FeePnLPct = %v, want 1.6", m.FeePnLPct)
	}
	if math.Abs(m.NetPnLPct-0.6) > 1e-9 {
		t.Errorf("NetPnLPct = %v, want 0.6", m.NetPnLPct)
	}
}

// TestAggregate_AllWinsInfiniteProfitFactor confirms ProfitFactor reports
// +Inf (rather than dividing by zero silently or panicking) when there
// are no losing trades to divide by.
func TestAggregate_AllWinsInfiniteProfitFactor(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	trades := []backtest.Trade{
		{ExitTimestamp: base, GrossReturnPct: 1.0, NetReturnPct: 1.0},
		{ExitTimestamp: base.Add(time.Hour), GrossReturnPct: 2.0, NetReturnPct: 2.0},
	}

	m := backtest.Aggregate(trades)
	if !math.IsInf(m.ProfitFactor, 1) {
		t.Errorf("ProfitFactor = %v, want +Inf", m.ProfitFactor)
	}
}

// TestAggregate_SortsByExitTimestamp confirms the equity curve (and
// therefore MaxDrawdownPct) is computed in chronological exit order
// regardless of the input slice's order.
func TestAggregate_SortsByExitTimestamp(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// Out of order input: the -2.0 trade (which should come last
	// chronologically) is listed first.
	trades := []backtest.Trade{
		{ExitTimestamp: base.Add(2 * time.Hour), GrossReturnPct: -2.0, NetReturnPct: -2.0},
		{ExitTimestamp: base, GrossReturnPct: 3.0, NetReturnPct: 3.0},
	}

	m := backtest.Aggregate(trades)
	// Chronological equity curve: 3.0 (peak), then 1.0 -> drawdown 2.0.
	if math.Abs(m.MaxDrawdownPct-2.0) > 1e-9 {
		t.Errorf("MaxDrawdownPct = %v, want 2.0 (chronological order), got a result implying input order was used instead", m.MaxDrawdownPct)
	}
}

// TestAggregate_CostsDecideWinAndExpectancy is issue #598: a trade whose
// price return is positive but whose cost-inclusive return is negative
// is a loss for Expectancy, WinRate and ProfitFactor (the gate the
// shadow backtest feeds to the Self-Improvement approval).
func TestAggregate_CostsDecideWinAndExpectancy(t *testing.T) {
	trades := []backtest.Trade{{
		ExitTimestamp:  time.Date(2026, 1, 1, 9, 5, 0, 0, time.UTC),
		GrossReturnPct: 0.1, SlippageReturnPct: -0.0999, FeeReturnPct: 0.1, NetReturnPct: -0.0999,
	}}

	m := backtest.Aggregate(trades)

	if m.Expectancy >= 0 {
		t.Errorf("Expectancy = %v, want < 0 (net of costs)", m.Expectancy)
	}
	if m.WinRate != 0 {
		t.Errorf("WinRate = %v, want 0", m.WinRate)
	}
	if m.ProfitFactor != 0 {
		t.Errorf("ProfitFactor = %v, want 0 (no net wins)", m.ProfitFactor)
	}
	if math.Abs(m.GrossPnLPct-0.1) > 1e-9 {
		t.Errorf("GrossPnLPct = %v, want 0.1 (still reported separately)", m.GrossPnLPct)
	}
}

// TestAggregate_MaxDrawdownIgnoresInputOrder is issue #599: trades
// exiting at the same instant must give one MaxDrawdownPct for every
// input permutation. Tied trades are applied gains first, so the curve
// is 0.6, 0.4 (-0.2), then 0.3 and 0.2 (-0.1 each): drawdown 0.4.
func TestAggregate_MaxDrawdownIgnoresInputOrder(t *testing.T) {
	at := time.Date(2026, 1, 1, 9, 5, 0, 0, time.UTC)
	trades := []backtest.Trade{
		{InstrumentID: 1, Symbol: "1111", ExitTimestamp: at, NetReturnPct: 0.6},
		{InstrumentID: 2, Symbol: "2222", ExitTimestamp: at, NetReturnPct: -0.2},
		{InstrumentID: 1, Symbol: "1111", ExitTimestamp: at.Add(time.Minute), NetReturnPct: -0.1},
		{InstrumentID: 2, Symbol: "2222", ExitTimestamp: at.Add(time.Minute), NetReturnPct: -0.1},
	}
	want := backtest.Aggregate(trades).MaxDrawdownPct

	rng := rand.New(rand.NewSource(1))
	for n := 0; n < 200; n++ {
		shuffled := append([]backtest.Trade(nil), trades...)
		rng.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
		if got := backtest.Aggregate(shuffled).MaxDrawdownPct; got != want {
			t.Fatalf("MaxDrawdownPct = %v for permutation %d, want %v regardless of input order", got, n, want)
		}
	}
	if math.Abs(want-0.4) > 1e-9 {
		t.Errorf("MaxDrawdownPct = %v, want 0.4 (gains first within an instant)", want)
	}
}
