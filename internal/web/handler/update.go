package handler

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/service/updater"
	"github.com/ousiassllc/pitha-trador/internal/version"
	"github.com/ousiassllc/pitha-trador/internal/web/organisms"
)

// UpdateController is the subset of internal/service/updater's
// SchedulerAdapter the update notification routes need (issue #76):
// Status is the last check's outcome, CheckForUpdate runs a check right
// now - the same call the scheduler's periodic tick makes, so a manual
// check that finds a verified installer restarts the app exactly like the
// automatic one. updater.SchedulerAdapter implements it directly.
type UpdateController interface {
	Status() updater.Status
	CheckForUpdate(ctx context.Context) error
}

// UpdateHandler implements `GET /system/update-status`, `GET
// /system/update-panel` and `POST /system/update-check` (issue #76). A nil
// controller means this build has no updater (cmd/server): the two GET
// routes render nothing and the POST route 404s.
type UpdateHandler struct {
	controller UpdateController
}

// NewUpdateHandler returns an UpdateHandler backed by controller (nil for
// builds without an updater).
func NewUpdateHandler(controller UpdateController) *UpdateHandler {
	return &UpdateHandler{controller: controller}
}

// updateStatusChangedEvent is the HX-Trigger event Check fires so the
// Header's `#update-banner` refreshes immediately instead of at its next
// poll (organisms.Header's doc comment).
const updateStatusChangedEvent = "updateStatusChanged"

// Status implements `GET /system/update-status`: Header's `#update-banner`
// fragment (organisms.UpdateBanner).
func (h *UpdateHandler) Status(c *gin.Context) {
	var props organisms.UpdateBannerProps
	if h.controller != nil {
		status := h.controller.Status()
		props = organisms.UpdateBannerProps{
			Available: status.Available,
			Version:   status.Version,
			Blocked:   status.Blocked,
			Ready:     status.Ready,
		}
	}
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Status(http.StatusOK)
	_ = organisms.UpdateBanner(props).Render(c.Request.Context(), c.Writer)
}

// Panel implements `GET /system/update-panel`: Settings' `#update-panel`
// fragment (organisms.UpdatePanel), empty when there is no updater.
func (h *UpdateHandler) Panel(c *gin.Context) {
	h.renderPanel(c, false)
}

// Check implements `POST /system/update-check`: runs an update check
// immediately, then renders the refreshed UpdatePanel. A failed check is
// reported in the panel (the Checker already logged and recorded it in its
// Status) rather than as an HTTP error, so HTMX still swaps the fragment.
func (h *UpdateHandler) Check(c *gin.Context) {
	if h.controller == nil {
		c.Status(http.StatusNotFound)
		return
	}
	err := h.controller.CheckForUpdate(c.Request.Context())
	if err != nil {
		slog.Error("update: manual check failed", "error", err)
	}
	c.Header("HX-Trigger", updateStatusChangedEvent)
	h.renderPanel(c, err != nil)
}

func (h *UpdateHandler) renderPanel(c *gin.Context, failed bool) {
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Status(http.StatusOK)
	if h.controller == nil {
		return
	}
	status := h.controller.Status()
	props := organisms.UpdatePanelProps{
		CurrentVersion: version.Version,
		DevBuild:       status.DevBuild,
		Failed:         failed || status.LastError != "",
		Available:      status.Available,
		Version:        status.Version,
	}
	if !status.CheckedAt.IsZero() {
		props.CheckedAt = status.CheckedAt.In(time.Local).Format("2006-01-02 15:04")
	}
	_ = organisms.UpdatePanel(props).Render(c.Request.Context(), c.Writer)
}
