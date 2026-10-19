package sensitivity_test

import (
	"bytes"
	"math"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/backtest"
	"github.com/ousiassllc/pitha-trador/internal/service/backtest/sensitivity"
)

// Synthetic trades: net returns (%) of +0.30, +0.10, +0.04, -0.20.
func trades() []backtest.Trade {
	return []backtest.Trade{{NetReturnPct: 0.30}, {NetReturnPct: 0.10}, {NetReturnPct: 0.04}, {NetReturnPct: -0.20}}
}

func TestTable(t *testing.T) {
	rows := sensitivity.Table(trades(), sensitivity.DefaultExtraBps)
	want := []struct {
		extra, expectancyPct, share float64
	}{
		{0, 0.06, 0.75},   // mean (0.30+0.10+0.04-0.20)/4 = 0.06
		{2, 0.04, 0.75},   // each trade 0.02 lower; 0.04-0.02 = 0.02 still > 0
		{5, 0.01, 0.50},   // 0.04-0.05 < 0
		{10, -0.04, 0.25}, // only 0.30-0.10 and 0.10-0.10=0 (not > 0)
	}
	if len(rows) != len(want) {
		t.Fatalf("len(rows) = %d, want %d", len(rows), len(want))
	}
	for i, w := range want {
		r := rows[i]
		if r.ExtraCostBps != w.extra || r.TradeCount != 4 ||
			math.Abs(r.ExpectancyPct-w.expectancyPct) > 1e-9 || math.Abs(r.ExpectancyBps-w.expectancyPct*100) > 1e-7 || r.PositiveShare != w.share {
			t.Errorf("row %d = %+v, want extra %v expectancy %v%% share %v", i, r, w.extra, w.expectancyPct, w.share)
		}
	}
}

func ptr(v float64) *float64 { return &v }

// Predicted EVs (bps) of 12, 6, 3 and none (a trade of the legacy entry rule):
// the denominator is the 3 trades that carry one, independent of the realized
// net returns.
func predictedTrades() []backtest.Trade {
	return []backtest.Trade{
		{NetReturnPct: -0.20, PredictedEVBps: ptr(12)},
		{NetReturnPct: 0.30, PredictedEVBps: ptr(6)},
		{NetReturnPct: 0.10, PredictedEVBps: ptr(3)},
		{NetReturnPct: 0.50},
	}
}

func TestTable_PredictedEVShare(t *testing.T) {
	rows := sensitivity.Table(predictedTrades(), []float64{0, 5, 6, 12})
	want := []struct {
		extra float64
		share float64
	}{
		{0, 1.0},       // 12, 6, 3 are all > 0
		{5, 2.0 / 3.0}, // 12-5 and 6-5 stay > 0, 3-5 does not
		{6, 1.0 / 3.0}, // 6-6 = 0 is not > 0
		{12, 0},        // 12-12 = 0 is not > 0
	}
	for i, w := range want {
		r := rows[i]
		if r.TradeCount != 4 || r.PredictedEVTradeCount != 3 || math.Abs(r.PredictedEVPositiveShare-w.share) > 1e-12 {
			t.Errorf("extra %v: row = %+v, want 4 trades, 3 predicted, predicted share %v", w.extra, r, w.share)
		}
	}
	// The realized share is a separate figure: 3 of 4 trades have a net > 0 at 0 bps.
	if rows[0].PositiveShare != 0.75 {
		t.Errorf("realized PositiveShare = %v, want 0.75", rows[0].PositiveShare)
	}
}

func TestTable_NoPredictedEV(t *testing.T) {
	rows := sensitivity.Table(trades(), []float64{0, 5})
	for _, r := range rows {
		if r.PredictedEVTradeCount != 0 || r.PredictedEVPositiveShare != 0 {
			t.Errorf("row = %+v, want no predicted-EV trades and share 0", r)
		}
	}
}

func TestTable_NoTrades(t *testing.T) {
	rows := sensitivity.Table(nil, []float64{0, 5})
	if len(rows) != 2 || rows[1] != (sensitivity.Row{ExtraCostBps: 5}) {
		t.Fatalf("rows = %+v, want zero statistics", rows)
	}
}

func TestWriteCSV(t *testing.T) {
	var buf bytes.Buffer
	if err := sensitivity.WriteCSV(&buf, sensitivity.Table(trades(), []float64{0, 5})); err != nil {
		t.Fatalf("WriteCSV: %v", err)
	}
	want := "extra_cost_bps,trade_count,expectancy_pct,expectancy_bps,positive_share,predicted_ev_trade_count,predicted_ev_positive_share\n" +
		"0,4,0.0600,6.00,0.7500,0,0.0000\n" +
		"5,4,0.0100,1.00,0.5000,0,0.0000\n"
	if buf.String() != want {
		t.Fatalf("CSV =\n%s\nwant\n%s", buf.String(), want)
	}
}

func TestWriteCSV_PredictedEV(t *testing.T) {
	var buf bytes.Buffer
	if err := sensitivity.WriteCSV(&buf, sensitivity.Table(predictedTrades(), []float64{5})); err != nil {
		t.Fatalf("WriteCSV: %v", err)
	}
	want := "extra_cost_bps,trade_count,expectancy_pct,expectancy_bps,positive_share,predicted_ev_trade_count,predicted_ev_positive_share\n" +
		"5,4,0.1250,12.50,0.7500,3,0.6667\n"
	if buf.String() != want {
		t.Fatalf("CSV =\n%s\nwant\n%s", buf.String(), want)
	}
}
