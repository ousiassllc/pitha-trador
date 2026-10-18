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
	want := "extra_cost_bps,trade_count,expectancy_pct,expectancy_bps,positive_share\n" +
		"0,4,0.0600,6.00,0.7500\n" +
		"5,4,0.0100,1.00,0.5000\n"
	if buf.String() != want {
		t.Fatalf("CSV =\n%s\nwant\n%s", buf.String(), want)
	}
}
