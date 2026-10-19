package mcptests

import (
	"slices"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp"
)

type perfData struct {
	ClosedTradeCount        int      `json:"closed_trade_count"`
	OpenPositionCount       int      `json:"open_position_count"`
	WinCount                int      `json:"win_count"`
	LossCount               int      `json:"loss_count"`
	FlatCount               int      `json:"flat_count"`
	WinRate                 *float64 `json:"win_rate"`
	TotalPnlJPY             float64  `json:"total_pnl_jpy"`
	ExpectancyJPY           *float64 `json:"expectancy_jpy"`
	ExpectancyReturnPct     *float64 `json:"expectancy_return_pct"`
	ProfitFactor            *float64 `json:"profit_factor"`
	MaxDrawdownJPY          float64  `json:"max_drawdown_jpy"`
	CapitalJPY              *float64 `json:"capital_jpy"`
	MaxDrawdownCapitalRatio *float64 `json:"max_drawdown_capital_ratio"`
}

// Hand computation over the seeded closed trades (see seedPositions):
// pnl in close order +1000 -3000 +2000 0 -500.
//
//	wins 2 / losses 2 / flat 1 of 5 -> win rate 0.4; total -500 -> mean -100
//	returns% = 1, -3, 1, 0, 0 -> mean -0.2
//	profit factor = 3000 / 3500
//	equity 1000, -2000, 0, 0, -500; peak 1000 -> max drawdown 3000 (-2000)
//	capital 30,000,000 -> ratio 3000 / 30,000,000 = 0.0001
func TestPaperPerformanceSummary(t *testing.T) {
	srv := seededServer(t, nil)
	var d perfData
	srv.call(t, "paper_performance_summary", period).data(t, "paper_performance_summary", &d)
	if d.ClosedTradeCount != 5 || d.OpenPositionCount != 1 || d.WinCount != 2 || d.LossCount != 2 || d.FlatCount != 1 {
		t.Fatalf("counts: %+v", d)
	}
	near(t, "win_rate", d.WinRate, 0.4)
	near(t, "expectancy_jpy", d.ExpectancyJPY, -100)
	near(t, "expectancy_return_pct", d.ExpectancyReturnPct, -0.2)
	near(t, "profit_factor", d.ProfitFactor, 3000.0/3500.0)
	near(t, "capital_jpy", d.CapitalJPY, 30_000_000)
	near(t, "max_drawdown_capital_ratio", d.MaxDrawdownCapitalRatio, 0.0001)
	if d.TotalPnlJPY != -500 || d.MaxDrawdownJPY != 3000 {
		t.Fatalf("total=%v drawdown=%v, want -500 / 3000", d.TotalPnlJPY, d.MaxDrawdownJPY)
	}

	// Undefined capital: yen drawdown stays, ratio and capital are null.
	noCap := seededServer(t, func(o *paperresultsmcp.Options) { o.CapitalJPY = -1 })
	var nc perfData
	noCap.call(t, "paper_performance_summary", period).data(t, "paper_performance_summary", &nc)
	wantNull(t, "capital_jpy", nc.CapitalJPY)
	wantNull(t, "max_drawdown_capital_ratio", nc.MaxDrawdownCapitalRatio)
	if nc.MaxDrawdownJPY != 3000 {
		t.Fatalf("drawdown without capital = %v", nc.MaxDrawdownJPY)
	}
}

// An empty period never reads as 0%: ratios are null, counts 0.
func TestPaperPerformanceSummaryEmptyPeriod(t *testing.T) {
	srv := seededServer(t, nil)
	var d perfData
	srv.call(t, "paper_performance_summary", map[string]any{"from": "2026-07-01", "to": "2026-07-10"}).
		data(t, "paper_performance_summary", &d)
	if d.ClosedTradeCount != 0 || d.MaxDrawdownJPY != 0 || d.TotalPnlJPY != 0 {
		t.Fatalf("%+v", d)
	}
	wantNull(t, "win_rate", d.WinRate)
	wantNull(t, "expectancy_jpy", d.ExpectancyJPY)
	wantNull(t, "expectancy_return_pct", d.ExpectancyReturnPct)
	wantNull(t, "profit_factor", d.ProfitFactor)
}

func TestPaperDailyPnL(t *testing.T) {
	srv := seededServer(t, nil)
	var d struct {
		Days []struct {
			Date       string  `json:"date"`
			TradeCount int     `json:"trade_count"`
			PnlJPY     float64 `json:"pnl_jpy"`
			WinCount   int     `json:"win_count"`
			LossCount  int     `json:"loss_count"`
		} `json:"days"`
	}
	srv.call(t, "paper_daily_pnl", period).data(t, "paper_daily_pnl", &d)
	// JST days: 10-01 (+1000, -3000), 10-02 (+2000), 10-03 (0 at 00:30 JST,
	// -500). Days with no close (10-04..10-09) have no row.
	type day = struct {
		Date       string  `json:"date"`
		TradeCount int     `json:"trade_count"`
		PnlJPY     float64 `json:"pnl_jpy"`
		WinCount   int     `json:"win_count"`
		LossCount  int     `json:"loss_count"`
	}
	want := []day{
		{"2026-10-01", 2, -2000, 1, 1},
		{"2026-10-02", 1, 2000, 1, 0},
		{"2026-10-03", 2, -500, 0, 1},
	}
	if !slices.Equal(d.Days, want) {
		t.Fatalf("days = %+v, want %+v", d.Days, want)
	}
}
