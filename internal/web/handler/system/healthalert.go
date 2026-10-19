package system

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/marketcalendar"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/shared"
	"github.com/ousiassllc/pitha-trador/internal/web/organisms"
)

// HealthAlertSource is what `GET /system/health-alerts` needs from
// internal/service/healthalert.Detector: the alerts active now (issue #802),
// none while the market-data pipeline is healthy or outside the watched hours.
type HealthAlertSource interface {
	Active(ctx context.Context) ([]domain.HealthAlert, error)
}

// HealthAlertHandler implements `GET /system/health-alerts`.
type HealthAlertHandler struct {
	source HealthAlertSource
}

// NewHealthAlertHandler returns a HealthAlertHandler backed by source; a nil
// source (no detector wired) renders no banner.
func NewHealthAlertHandler(source HealthAlertSource) *HealthAlertHandler {
	return &HealthAlertHandler{source: source}
}

// Status implements `GET /system/health-alerts`: Header's
// `#health-alert-banner` fragment (organisms.HealthAlertBanner), non-empty only
// while an alert is active. A read failure is logged and renders no banner: the
// alert itself is also readable through the Paper results MCP.
func (h *HealthAlertHandler) Status(c *gin.Context) {
	var views []organisms.HealthAlertView
	if h.source != nil {
		alerts, err := h.source.Active(c.Request.Context())
		if err != nil {
			slog.ErrorContext(c.Request.Context(), "handler: health alert banner", "error", err)
		}
		for _, a := range alerts {
			views = append(views, organisms.HealthAlertView{Code: a.Code, Since: a.Since.In(marketcalendar.JST).Format("15:04")})
		}
	}
	shared.RenderHTML(c, http.StatusOK, organisms.HealthAlertBanner(views))
}
