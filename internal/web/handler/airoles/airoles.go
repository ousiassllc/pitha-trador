// Package airoles serves the Settings screen's 役割ごとのモデル section (issue
// #1080): `GET /settings/ai-roles-panel`, `POST /settings/ai-roles/:role` (save
// a role's provider and model) and `DELETE /settings/ai-roles/:role` (back to the
// default). Like every write it passes the session cookie and CSRF check. These
// are the only routes that can change a role's provider or model, the live Jev
// role included: nothing in the MCP writes these settings.
package airoles

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/config/airole"
	"github.com/ousiassllc/pitha-trador/internal/service/airoles"
	"github.com/ousiassllc/pitha-trador/internal/web/atoms"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/shared"
	view "github.com/ousiassllc/pitha-trador/internal/web/organisms/airoles"
)

// Service is the subset of *airoles.Service the handlers need.
type Service interface {
	State(ctx context.Context) (airoles.State, error)
	Save(ctx context.Context, role, provider, model string) error
	Reset(ctx context.Context, role string) error
}

// Handler implements the AI role Settings routes.
type Handler struct{ svc Service }

// New returns a Handler over svc.
func New(svc Service) *Handler { return &Handler{svc: svc} }

// Register mounts the routes on engine.
func (h *Handler) Register(engine *gin.Engine) {
	engine.GET("/settings/ai-roles-panel", h.Panel)
	engine.POST("/settings/ai-roles/:role", h.Save)
	engine.DELETE("/settings/ai-roles/:role", h.Reset)
}

// Sol, Opus and Luna read their setting on every call (issue #1112): a save applies
// to their next call. The live Jev (what is traded) keeps its start-up connection
// until the app restarts.
const (
	savedHotNote     = "保存しました。次の呼び出しから反映されます。"
	savedRestartNote = "保存しました。反映にはアプリの再起動が必要です。"
	resetHotNote     = "既定に戻しました。次の呼び出しから反映されます。"
	resetRestartNote = "既定に戻しました。反映にはアプリの再起動が必要です。"
)

// notice picks the hot or the restart note for role.
func notice(role, hot, restart string) string {
	if role == airole.RoleJev {
		return restart
	}
	return hot
}

// Panel implements `GET /settings/ai-roles-panel`.
func (h *Handler) Panel(c *gin.Context) { h.render(c, "", "") }

// Save implements `POST /settings/ai-roles/:role`: form `provider` and `model`.
// Invalid input is a 422 and nothing is saved.
func (h *Handler) Save(c *gin.Context) {
	role := c.Param("role")
	err := h.svc.Save(c.Request.Context(), role, c.PostForm("provider"), c.PostForm("model"))
	if !h.handled(c, role, err, "AI の設定を保存できませんでした。") {
		return
	}
	h.render(c, role, notice(role, savedHotNote, savedRestartNote))
}

// Reset implements `DELETE /settings/ai-roles/:role`.
func (h *Handler) Reset(c *gin.Context) {
	role := c.Param("role")
	err := h.svc.Reset(c.Request.Context(), role)
	if !h.handled(c, role, err, "AI の設定を既定に戻せませんでした。") {
		return
	}
	h.render(c, role, notice(role, resetHotNote, resetRestartNote))
}

// handled answers a failed action and reports whether the action succeeded.
func (h *Handler) handled(c *gin.Context, role string, err error, failure string) bool {
	switch {
	case err == nil:
		return true
	case errors.Is(err, airoles.ErrInvalidInput):
		shared.RespondFormError(c, errorTarget(role), strings.TrimPrefix(err.Error(), airoles.ErrInvalidInput.Error()+": ")+"。保存していません。")
	default:
		slog.ErrorContext(c.Request.Context(), "airoles: change", "role", role, "error", err)
		shared.RespondActionError(c, http.StatusInternalServerError, failure)
	}
	return false
}

// errorTarget is the inline-alert slot of role's form; an unknown role (a
// hand-made request) gets the panel's own slot, never its raw name in a selector.
func errorTarget(role string) string {
	if airole.IsRole(role) {
		return "#ai-role-error-" + role
	}
	return "#ai-roles-panel"
}

func (h *Handler) render(c *gin.Context, role, notice string) {
	c.Header("Cache-Control", "no-store")
	ctx := c.Request.Context()
	st, err := h.svc.State(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "airoles: read state", "error", err)
		shared.RespondActionError(c, http.StatusInternalServerError, "AI の設定を読み込めませんでした。")
		return
	}
	shared.RenderHTML(c, http.StatusOK, view.Panel(props(st, role, notice)))
}

func props(st airoles.State, noticeRole, notice string) view.PanelProps {
	p := view.PanelProps{}
	for _, id := range airole.Providers() {
		p.Providers = append(p.Providers, view.ProviderOption{ID: id, Name: airole.ProviderName(id)})
	}
	for _, pr := range st.Providers {
		p.ProviderRows = append(p.ProviderRows, view.ProviderRow{Name: pr.Name, KeyKnown: pr.KeyKnown, KeySet: pr.KeySet})
	}
	for _, r := range st.Roles {
		row := view.RoleRow{
			ID: r.ID, Name: r.Name, Description: r.Description, Live: r.Live,
			Provider: r.Setting.Provider, Model: r.Setting.Model, Stored: r.Stored, Damaged: r.Damaged,
			ModelHint: modelHint(r.ID),
		}
		if r.Running != nil && r.Live { // only the live Jev stays on its start-up pair; the others follow their setting
			row.Running = label(*r.Running)
		}
		if r.ID == noticeRole {
			row.Notice = notice
		}
		p.Roles = append(p.Roles, row)
	}
	for _, ch := range st.History {
		p.History = append(p.History, view.HistoryRow{At: atoms.FormatJST(ch.At), Role: roleName(ch.Role), Detail: detail(ch)})
	}
	return p
}

// modelHint is the model field's placeholder: what an empty model means.
func modelHint(role string) string {
	if role == airole.RoleJev {
		return "空欄で既定のモデル"
	}
	return "例: provider/model"
}

func roleName(id string) string {
	for _, in := range airole.Infos() {
		if in.ID == id {
			return in.Name
		}
	}
	return id
}

// label renders a provider/model pair; an empty model is "モデルの指定なし".
func label(s airole.Setting) string {
	model := s.Model
	if model == "" {
		model = "モデルの指定なし"
	}
	return airole.ProviderName(s.Provider) + " / " + model
}

func detail(c airoles.Change) string {
	if c.Saved {
		return "設定を保存: " + label(c.Before) + " → " + label(c.After)
	}
	return "起動時に使い始めた組: " + label(c.After)
}
