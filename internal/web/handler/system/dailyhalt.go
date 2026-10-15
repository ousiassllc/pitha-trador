package system

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/shared"
	"github.com/ousiassllc/pitha-trador/internal/web/organisms"
)

// DailyHaltSource is what `GET /system/daily-halt` needs from
// internal/service/risk.Engine: today's daily entry halt (issue #753,
// FR-RISK-8), nil while none applies.
type DailyHaltSource interface {
	DailyHalt(ctx context.Context) (*domain.DailyEntryHalt, error)
}

// DailyHaltHandler implements `GET /system/daily-halt`.
type DailyHaltHandler struct {
	source DailyHaltSource
}

// NewDailyHaltHandler returns a DailyHaltHandler backed by source; a nil
// source (no Risk Engine wired) renders no banner.
func NewDailyHaltHandler(source DailyHaltSource) *DailyHaltHandler {
	return &DailyHaltHandler{source: source}
}

// Status implements `GET /system/daily-halt`: Header's `#daily-halt-banner`
// fragment (organisms.DailyHaltBanner), non-empty only while a daily entry
// halt is in force. A read failure is logged and renders no banner: the
// halt itself is enforced by Risk Engine's Check, not by this display.
func (h *DailyHaltHandler) Status(c *gin.Context) {
	var props organisms.DailyHaltBannerProps
	if h.source != nil {
		halt, err := h.source.DailyHalt(c.Request.Context())
		if err != nil {
			slog.ErrorContext(c.Request.Context(), "handler: daily halt banner", "error", err)
		} else if halt != nil {
			props = organisms.DailyHaltBannerProps{Reason: halt.Reason, Label: halt.Label(), Message: halt.Message}
		}
	}
	shared.RenderHTML(c, http.StatusOK, organisms.DailyHaltBanner(props))
}
