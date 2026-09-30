package handler

import (
	"context"
	"log/slog"
	"net/http"
	"time"

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

// SystemHandler implements the Kill Switch routes: `GET /system/status`
// (docs/api/endpoints.md §4, HTMX badge fragment), `GET|POST
// /api/v1/system/status|pause|resume|kill` (§5, JSON, driven by the Lit
// pitha-kill-switch-panel) and `/ws/system` (system_ws.go).
type SystemHandler struct {
	engine       SystemEngine
	pollInterval time.Duration
}

// defaultSystemPollInterval is `/ws/system`'s State polling spacing
// (system_ws.go). docs/api/endpoints.md §6 does not specify a cadence;
// 2s matches symbol_ws.go's defaultTickInterval.
const defaultSystemPollInterval = 2 * time.Second

// NewSystemHandler returns a SystemHandler backed by engine.
func NewSystemHandler(engine SystemEngine) *SystemHandler {
	return &SystemHandler{engine: engine, pollInterval: defaultSystemPollInterval}
}

// SetPollInterval overrides `/ws/system`'s State polling spacing
// (default defaultSystemPollInterval). Exposed for tests that need a
// fast interval rather than production callers.
func (h *SystemHandler) SetPollInterval(d time.Duration) { h.pollInterval = d }

// Status implements `GET /system/status`: the system status badge
// fragment alone, which Header's StatusDot re-fetches (hx-get) on the
// `systemStateChanged` event.
func (h *SystemHandler) Status(c *gin.Context) {
	h.renderBadge(c)
}

// renderBadge writes the current system state as an
// organisms.SystemStatusBadge fragment.
func (h *SystemHandler) renderBadge(c *gin.Context) {
	state, _, err := h.engine.State(c.Request.Context())
	if err != nil {
		slog.ErrorContext(c.Request.Context(), "handler: system status badge", "error", err)
		respondActionError(c, http.StatusInternalServerError, "システム状態の取得に失敗しました。")
		return
	}
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Status(http.StatusOK)
	_ = organisms.SystemStatusBadge(state).Render(c.Request.Context(), c.Writer)
}

// SystemStateOutput is the Huma response body for
// `GET /api/v1/system/status` and `POST /api/v1/system/pause|resume|kill`
// (docs/api/endpoints.md §5).
type SystemStateOutput struct {
	Body struct {
		State     string `json:"state" doc:"Overall system state: running, paused, or killed."`
		CanPause  bool   `json:"can_pause" doc:"Whether POST /api/v1/system/pause is a valid transition from state."`
		CanResume bool   `json:"can_resume" doc:"Whether POST /api/v1/system/resume is a valid transition from state."`
		CanKill   bool   `json:"can_kill" doc:"Whether POST /api/v1/system/kill is a valid transition from state."`
	}
}

func (h *SystemHandler) stateOutput(ctx context.Context) (*SystemStateOutput, error) {
	state, _, err := h.engine.State(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError("read system state failed", err)
	}
	out := &SystemStateOutput{}
	out.Body.State = string(state)
	out.Body.CanPause = state.CanPause()
	out.Body.CanResume = state.CanResume()
	out.Body.CanKill = state.CanKill()
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

// APIStatus implements `GET /api/v1/system/status`: the read-only JSON
// twin of `GET /system/status` (which returns an HTML badge fragment for
// HTMX), used by `pitha-kill-switch-panel`
// (static/src/components/kill-switch-panel/pitha-kill-switch-panel.ts) to
// resync its state (and which actions the server currently allows) after
// a Risk-Engine push or a WebSocket reconnect; the initial state is
// already SSR'd into the panel's attributes (organisms.Header).
func (h *SystemHandler) APIStatus(ctx context.Context, _ *struct{}) (*SystemStateOutput, error) {
	return h.stateOutput(ctx)
}
