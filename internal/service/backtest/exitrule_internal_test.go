package backtest

import (
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

func barsAt(prices []float64, base time.Time) []domain.Snapshot {
	bars := make([]domain.Snapshot, len(prices))
	for i, p := range prices {
		bars[i] = domain.Snapshot{Timestamp: base.Add(time.Duration(i) * time.Minute), Price: p}
	}
	return bars
}

func TestCloseTrade_TakeProfitLong(t *testing.T) {
	base := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	bars := barsAt([]float64{100, 100.5, 101.2, 99}, base)
	exit := ExitRule{StopLossPct: 5, TakeProfitPct: 1.0, MaxHolding: time.Hour}

	trade, exitIdx := closeTrade(bars, 0, domain.JevDirectionLong, 100, exit, CostModel{}, 1, "TEST")
	if trade.ExitReason != ExitReasonTakeProfit {
		t.Errorf("ExitReason = %q, want %q", trade.ExitReason, ExitReasonTakeProfit)
	}
	if exitIdx != 2 {
		t.Errorf("exitIdx = %d, want 2 (the first bar clearing +1%% return)", exitIdx)
	}
	if trade.GrossReturnPct < 1.0 {
		t.Errorf("GrossReturnPct = %v, want >= 1.0", trade.GrossReturnPct)
	}
}

func TestCloseTrade_StopLossShort(t *testing.T) {
	base := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	// A SHORT profits when price falls; a rise past StopLossPct must
	// stop it out even though the series later falls sharply (99 at
	// index3) - the position must already be closed by then.
	bars := barsAt([]float64{100, 100.2, 103, 99}, base)
	exit := ExitRule{StopLossPct: 2.0, TakeProfitPct: 50, MaxHolding: time.Hour}

	trade, exitIdx := closeTrade(bars, 0, domain.JevDirectionShort, 100, exit, CostModel{}, 1, "TEST")
	if trade.ExitReason != ExitReasonStopLoss {
		t.Errorf("ExitReason = %q, want %q", trade.ExitReason, ExitReasonStopLoss)
	}
	if exitIdx != 2 {
		t.Errorf("exitIdx = %d, want 2", exitIdx)
	}
	if trade.GrossReturnPct >= 0 {
		t.Errorf("GrossReturnPct = %v, want < 0 (stopped out at a loss)", trade.GrossReturnPct)
	}
}

func TestCloseTrade_MaxHolding(t *testing.T) {
	base := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	bars := barsAt([]float64{100, 100.1, 100.2, 100.3, 100.4}, base)
	exit := ExitRule{StopLossPct: 50, TakeProfitPct: 50, MaxHolding: 2 * time.Minute}

	trade, exitIdx := closeTrade(bars, 0, domain.JevDirectionLong, 100, exit, CostModel{}, 1, "TEST")
	if trade.ExitReason != ExitReasonMaxHolding {
		t.Errorf("ExitReason = %q, want %q", trade.ExitReason, ExitReasonMaxHolding)
	}
	if exitIdx != 2 {
		t.Errorf("exitIdx = %d, want 2 (the last bar at or before MaxHolding)", exitIdx)
	}
}

func TestCloseTrade_DataEnded(t *testing.T) {
	base := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	bars := barsAt([]float64{100, 100.1, 100.2}, base)
	exit := ExitRule{StopLossPct: 50, TakeProfitPct: 50, MaxHolding: time.Hour}

	trade, exitIdx := closeTrade(bars, 0, domain.JevDirectionLong, 100, exit, CostModel{}, 1, "TEST")
	if trade.ExitReason != ExitReasonDataEnded {
		t.Errorf("ExitReason = %q, want %q", trade.ExitReason, ExitReasonDataEnded)
	}
	if exitIdx != len(bars)-1 {
		t.Errorf("exitIdx = %d, want %d (last available bar)", exitIdx, len(bars)-1)
	}
}

// TestCloseTrade_CostModelReducesReturn confirms CostModel's slippage
// (unfavorable entry/exit price adjustment) and fee (flat percentage-
// point deduction) both make the reported return worse than the gross,
// look-ahead-free price return (FR-BT-1 "スリッページ込みPnL"/"手数料込
// みPnL").
func TestCloseTrade_CostModelReducesReturn(t *testing.T) {
	base := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	bars := barsAt([]float64{100, 105}, base)
	exit := ExitRule{MaxHolding: time.Hour}
	cost := CostModel{SlippageBps: 50, FeeBps: 10}

	trade, _ := closeTrade(bars, 0, domain.JevDirectionLong, 100, exit, cost, 1, "TEST")
	if trade.SlippageReturnPct >= trade.GrossReturnPct {
		t.Errorf("SlippageReturnPct = %v, want < GrossReturnPct = %v", trade.SlippageReturnPct, trade.GrossReturnPct)
	}
	if trade.FeeReturnPct >= trade.GrossReturnPct {
		t.Errorf("FeeReturnPct = %v, want < GrossReturnPct = %v", trade.FeeReturnPct, trade.GrossReturnPct)
	}
	if trade.NetReturnPct >= trade.SlippageReturnPct {
		t.Errorf("NetReturnPct = %v, want < SlippageReturnPct = %v (fee applied on top of slippage)", trade.NetReturnPct, trade.SlippageReturnPct)
	}
}
