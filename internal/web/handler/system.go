package handler

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/web/organisms"
)

// SystemEngine is the subset of internal/service/risk.Engine's methods
// the system action/API routes below need (docs/api/endpoints.md §4, §5;
// functional.md FR-RISK-4). An interface here keeps this handler testable
// without a real Engine (which itself requires a SQLite database) wired
// in.
type SystemEngine interface {
	State(ctx context.Context) (domain.SystemState, []domain.KillSwitchEvent, error)
	Pause(ctx context.Context) error
	Resume(ctx context.Context) error
	Kill(ctx context.Context) error
}

// StaticSystemEngine is a fixed, in-memory SystemEngine, used as
// internal/router.New()'s default until a real internal/service/risk.
// Engine is wired in (mirrors StaticCandidateSource's role for
// ScannerHandler). Pause/Resume/Kill are no-ops; State always reports the
// configured State (defaulting to domain.SystemStateRunning).
type StaticSystemEngine struct {
	State_ domain.SystemState
}

func (s StaticSystemEngine) State(context.Context) (domain.SystemState, []domain.KillSwitchEvent, error) {
	state := s.State_
	if state == "" {
		state = domain.SystemStateRunning
	}
	return state, nil, nil
}

func (StaticSystemEngine) Pause(context.Context) error  { return nil }
func (StaticSystemEngine) Resume(context.Context) error { return nil }
func (StaticSystemEngine) Kill(context.Context) error   { return nil }

// SystemHandler implements the Kill Switch action/API routes
// (docs/api/endpoints.md §4 `/system/pause|resume|kill|status`, §5
// `POST /api/v1/system/pause|resume|kill`).
type SystemHandler struct {
	engine SystemEngine
}

// NewSystemHandler returns a SystemHandler backed by engine.
func NewSystemHandler(engine SystemEngine) *SystemHandler {
	return &SystemHandler{engine: engine}
}

// Pause implements `POST /system/pause`.
func (h *SystemHandler) Pause(c *gin.Context) {
	if err := h.engine.Pause(c.Request.Context()); err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}
	h.renderBadge(c)
}

// Resume implements `POST /system/resume`.
func (h *SystemHandler) Resume(c *gin.Context) {
	if err := h.engine.Resume(c.Request.Context()); err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}
	h.renderBadge(c)
}

// Kill implements `POST /system/kill`.
func (h *SystemHandler) Kill(c *gin.Context) {
	if err := h.engine.Kill(c.Request.Context()); err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}
	h.renderBadge(c)
}

// Status implements `GET /system/status`: the system status badge
// fragment alone, for the (later sub-scope's) Kill Switch panel to poll
// or re-fetch on demand.
func (h *SystemHandler) Status(c *gin.Context) {
	h.renderBadge(c)
}

// renderBadge writes the current system state as an
// organisms.SystemStatusBadge fragment.
func (h *SystemHandler) renderBadge(c *gin.Context) {
	state, _, err := h.engine.State(c.Request.Context())
	if err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Status(http.StatusOK)
	_ = organisms.SystemStatusBadge(state).Render(c.Request.Context(), c.Writer)
}

// SystemStateOutput is the Huma response body for
// `POST /api/v1/system/pause|resume|kill` (docs/api/endpoints.md §5).
type SystemStateOutput struct {
	Body struct {
		State string `json:"state" doc:"Overall system state: running, paused, or killed."`
	}
}

func (h *SystemHandler) stateOutput(ctx context.Context) (*SystemStateOutput, error) {
	state, _, err := h.engine.State(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError("read system state failed", err)
	}
	out := &SystemStateOutput{}
	out.Body.State = string(state)
	return out, nil
}

// APIPause implements `POST /api/v1/system/pause`.
func (h *SystemHandler) APIPause(ctx context.Context, _ *struct{}) (*SystemStateOutput, error) {
	if err := h.engine.Pause(ctx); err != nil {
		return nil, huma.Error500InternalServerError("pause failed", err)
	}
	return h.stateOutput(ctx)
}

// APIResume implements `POST /api/v1/system/resume`.
func (h *SystemHandler) APIResume(ctx context.Context, _ *struct{}) (*SystemStateOutput, error) {
	if err := h.engine.Resume(ctx); err != nil {
		return nil, huma.Error500InternalServerError("resume failed", err)
	}
	return h.stateOutput(ctx)
}

// APIKill implements `POST /api/v1/system/kill`.
func (h *SystemHandler) APIKill(ctx context.Context, _ *struct{}) (*SystemStateOutput, error) {
	if err := h.engine.Kill(ctx); err != nil {
		return nil, huma.Error500InternalServerError("kill failed", err)
	}
	return h.stateOutput(ctx)
}
