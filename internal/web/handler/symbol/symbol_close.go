package symbol

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/execution"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/shared"
	"github.com/ousiassllc/pitha-trador/internal/web/molecules"
)

// ClosePosition implements `POST /positions/:id/close` (docs/api/endpoints.md
// §4): a manual, market-price Paper Exit (domain.ExitReasonManual) priced
// by execution.Engine.CloseAtMarket (the handler assembles no fill inputs),
// returning the updated molecules.PositionRow fragment.
func (h *SymbolHandler) ClosePosition(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		shared.RespondActionError(c, http.StatusBadRequest, "ポジション ID が不正です。")
		return
	}

	ctx := c.Request.Context()
	position, err := h.provider.GetPosition(ctx, id)
	if err != nil {
		if errors.Is(err, domain.ErrPositionNotFound) {
			shared.RespondActionError(c, http.StatusNotFound, "ポジションが見つかりません。")
			return
		}
		slog.ErrorContext(ctx, "handler: close position get", "position_id", id, "error", err)
		shared.RespondActionError(c, http.StatusInternalServerError, "ポジションの取得に失敗しました。")
		return
	}
	if !position.IsOpen() {
		shared.RespondActionError(c, http.StatusConflict, "このポジションは既に決済済みです。")
		return
	}

	closed, err := h.provider.CloseAtMarket(ctx, id, domain.ExitReasonManual, h.now())
	if err != nil {
		// 昼休み・立会時間外は約定しない（次の立会で再度決済できる）。
		if errors.Is(err, execution.ErrOutsideTradingSession) {
			shared.RespondActionError(c, http.StatusConflict, "立会時間外（昼休み・大引け後）のため決済できません。次の立会で再度お試しください。")
			return
		}
		// A concurrent exit (Exit monitor / CloseAll / another click) won the race.
		if errors.Is(err, domain.ErrPositionAlreadyClosed) {
			shared.RespondActionError(c, http.StatusConflict, "このポジションは既に決済済みです。")
			return
		}
		slog.ErrorContext(ctx, "handler: close position", "position_id", id, "error", err)
		shared.RespondActionError(c, http.StatusInternalServerError, "ポジションの決済に失敗しました。")
		return
	}

	shared.RenderHTML(c, http.StatusOK, molecules.PositionRow(closed))
}
