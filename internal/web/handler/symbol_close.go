package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository"
	"github.com/ousiassllc/pitha-trador/internal/web/molecules"
)

// ClosePosition implements `POST /positions/:id/close` (docs/api/endpoints.md
// §4): a manual, market-price Paper Exit (domain.ExitReasonManual),
// returning the updated molecules.PositionRow fragment.
func (h *SymbolHandler) ClosePosition(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.Status(http.StatusBadRequest)
		return
	}

	ctx := c.Request.Context()
	position, err := h.provider.GetPosition(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrPositionNotFound) {
			c.Status(http.StatusNotFound)
			return
		}
		c.Status(http.StatusInternalServerError)
		return
	}
	if !position.IsOpen() {
		c.Status(http.StatusConflict)
		return
	}

	exitPrice := position.CurrentPrice
	if state, err := h.provider.State(ctx, position.Symbol); err == nil && state.LastPrice > 0 {
		exitPrice = state.LastPrice
	}

	closed, err := h.provider.Close(ctx, id, domain.ExitReasonManual, exitPrice, h.now())
	if err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}

	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Status(http.StatusOK)
	_ = molecules.PositionRow(closed).Render(ctx, c.Writer)
}
