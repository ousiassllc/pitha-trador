package performance

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/service/backtest/sensitivity"
)

// Register mounts the Performance routes on engine: the page and the cost
// sensitivity export.
func (h *PerformanceHandler) Register(engine *gin.Engine) {
	engine.GET("/performance", h.Page)
	engine.GET("/performance/cost-sensitivity", h.CostSensitivity)
}

type costSensitivityRow struct {
	ExtraCostBps  float64 `json:"extra_cost_bps"`
	TradeCount    int     `json:"trade_count"`
	ExpectancyPct float64 `json:"expectancy_pct"`
	ExpectancyBps float64 `json:"expectancy_bps"`
	PositiveShare float64 `json:"positive_share"`
}

// CostSensitivity implements `GET /performance/cost-sensitivity` (issue #779):
// it runs the Performance form's Walk Forward backtest (same query
// parameters) and prints how its Forward trades' Expectancy and the share of
// still-profitable trades change when 0 / 2 / 5 / 10 bps of extra round-trip
// cost are added. `format=csv` (the default) downloads a CSV, `format=json`
// returns the rows as JSON. 400 on invalid input, 500 on a run failure, 503 on
// a timeout, as the page does.
func (h *PerformanceHandler) CostSensitivity(c *gin.Context) {
	format := c.DefaultQuery("format", "csv")
	if format != "csv" && format != "json" {
		c.String(http.StatusBadRequest, "format は csv か json で指定してください。")
		return
	}
	_, wf, errs := parseBacktestForm(c)
	if len(errs) > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"errors": errs})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), backtestTimeout)
	defer cancel()
	result, err := h.runner.RunWalkForward(ctx, wf)
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		c.String(http.StatusServiceUnavailable, "バックテストがタイムアウトしました。期間を短くしてください。")
		return
	case err != nil:
		slog.ErrorContext(ctx, "handler: cost sensitivity backtest", "error", err)
		c.String(http.StatusInternalServerError, "バックテストの実行に失敗しました。")
		return
	}

	rows := sensitivity.Table(result.ForwardTrades, sensitivity.DefaultExtraBps)
	if format == "json" {
		out := make([]costSensitivityRow, len(rows))
		for i, r := range rows {
			out[i] = costSensitivityRow(r)
		}
		c.JSON(http.StatusOK, gin.H{"rows": out})
		return
	}
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", `attachment; filename="cost-sensitivity.csv"`)
	c.Status(http.StatusOK)
	if err := sensitivity.WriteCSV(c.Writer, rows); err != nil {
		slog.ErrorContext(ctx, "handler: cost sensitivity csv", "error", err)
	}
}
