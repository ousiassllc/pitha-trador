package handler

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/service/execution"
	"github.com/ousiassllc/pitha-trador/internal/web/pages"
)

// defaultDecisionHistoryLimit is how many recent jev_decisions rows
// Page's Decision history section shows.
const defaultDecisionHistoryLimit = 50

// Page implements `GET /symbols/:symbol` (docs/api/endpoints.md §3): the
// full Symbol Detail page, embedding the `pitha-price-chart` island
// (functional.md §5.2). Unlike ScannerHandler.Page, no HX-Request
// fragment variant exists yet - Symbol Detail has no server-rendered
// fallback content analogous to ScannerTableFallback for `pitha-price-
// chart` (a canvas-drawn chart has nothing meaningful to show before
// JS/Lit loads), so every request renders the full page.
func (h *SymbolHandler) Page(c *gin.Context) {
	symbol := c.Param("symbol")
	ctx := c.Request.Context()

	state, err := h.provider.State(ctx, symbol)
	if errors.Is(err, execution.ErrInstrumentUnknown) {
		respondPageError(c, http.StatusNotFound, "指定された銘柄は見つかりません。")
		return
	}
	if err != nil {
		slog.ErrorContext(ctx, "handler: symbol page state", "symbol", symbol, "error", err)
		respondPageError(c, http.StatusInternalServerError, "銘柄の状態の取得に失敗しました。")
		return
	}
	decisions, err := h.provider.RecentDecisions(ctx, symbol, defaultDecisionHistoryLimit)
	if err != nil {
		slog.ErrorContext(ctx, "handler: symbol page decisions", "symbol", symbol, "error", err)
		respondPageError(c, http.StatusInternalServerError, "判断履歴の取得に失敗しました。")
		return
	}

	c.Status(http.StatusOK)
	c.Header("Content-Type", "text/html; charset=utf-8")
	_ = pages.SymbolDetailPage(pages.SymbolDetailProps{
		Symbol:             symbol,
		State:              state,
		AllowedPositionPct: h.riskParams.AllowedPositionPct,
		StopLossPct:        h.riskParams.StopLossPct,
		TakeProfitPct:      h.riskParams.TakeProfitPct,
		Decisions:          decisions,
	}).Render(ctx, c.Writer)
}
