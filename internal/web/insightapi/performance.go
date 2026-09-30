package insightapi

import (
	"context"
	"time"

	"github.com/danielgtaylor/huma/v2"
)

// PerformanceAPIOutput is the Huma response body for `GET
// /api/v1/performance` (docs/api/endpoints.md §5). See
// execution.Performance for each field's definition; profit_factor,
// sharpe_ref and sortino_ref are null when undefined.
type PerformanceAPIOutput struct {
	Body struct {
		TotalPnL               float64  `json:"total_pnl"`
		DailyPnL               float64  `json:"daily_pnl"`
		TradeCount             int      `json:"trade_count"`
		WinRate                float64  `json:"win_rate"`
		ProfitFactor           *float64 `json:"profit_factor"`
		Expectancy             float64  `json:"expectancy"`
		MaxDrawdownPct         float64  `json:"max_drawdown_pct"`
		AverageHoldTimeMinutes float64  `json:"average_hold_time_minutes"`
		SharpeRef              *float64 `json:"sharpe_ref"`
		SortinoRef             *float64 `json:"sortino_ref"`
		SignalCount            int64    `json:"signal_count"`
	}
}

// Performance implements `GET /api/v1/performance`
// (docs/api/endpoints.md §5): realized-trade statistics over every
// closed position.
func (h *Handler) Performance(ctx context.Context, _ *struct{}) (*PerformanceAPIOutput, error) {
	perf, err := h.provider.Performance(ctx, time.Now())
	if err != nil {
		return nil, huma.Error500InternalServerError("read performance failed", err)
	}

	out := &PerformanceAPIOutput{}
	out.Body.TotalPnL = perf.TotalPnL
	out.Body.DailyPnL = perf.DailyPnL
	out.Body.TradeCount = perf.TradeCount
	out.Body.WinRate = perf.WinRate
	out.Body.ProfitFactor = perf.ProfitFactor
	out.Body.Expectancy = perf.Expectancy
	out.Body.MaxDrawdownPct = perf.MaxDrawdownPct
	out.Body.AverageHoldTimeMinutes = perf.AverageHoldTimeMinutes
	out.Body.SharpeRef = perf.SharpeRef
	out.Body.SortinoRef = perf.SortinoRef
	out.Body.SignalCount = perf.SignalCount
	return out, nil
}
