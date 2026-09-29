package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/web/molecules"
)

// ClosePosition implements `POST /positions/:id/close` (docs/api/endpoints.md
// §4): a manual, market-price Paper Exit (domain.ExitReasonManual),
// returning the updated molecules.PositionRow fragment.
func (h *SymbolHandler) ClosePosition(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		respondActionError(c, http.StatusBadRequest, "ポジション ID が不正です。")
		return
	}

	ctx := c.Request.Context()
	position, err := h.provider.GetPosition(ctx, id)
	if err != nil {
		if errors.Is(err, domain.ErrPositionNotFound) {
			respondActionError(c, http.StatusNotFound, "ポジションが見つかりません。")
			return
		}
		slog.ErrorContext(ctx, "handler: close position get", "position_id", id, "error", err)
		respondActionError(c, http.StatusInternalServerError, "ポジションの取得に失敗しました。")
		return
	}
	if !position.IsOpen() {
		respondActionError(c, http.StatusConflict, "このポジションは既に決済済みです。")
		return
	}

	exitPrice := position.CurrentPrice
	if state, err := h.provider.State(ctx, position.Symbol); err == nil && state.LastPrice > 0 {
		exitPrice = state.LastPrice
	}

	closed, err := h.provider.Close(ctx, id, domain.ExitReasonManual, exitPrice, h.now())
	if err != nil {
		// A concurrent exit (Exit monitor / CloseAll / another click) won the race.
		if errors.Is(err, domain.ErrPositionAlreadyClosed) {
			respondActionError(c, http.StatusConflict, "このポジションは既に決済済みです。")
			return
		}
		slog.ErrorContext(ctx, "handler: close position", "position_id", id, "error", err)
		respondActionError(c, http.StatusInternalServerError, "ポジションの決済に失敗しました。")
		return
	}

	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Status(http.StatusOK)
	_ = molecules.PositionRow(closed).Render(ctx, c.Writer)
}
