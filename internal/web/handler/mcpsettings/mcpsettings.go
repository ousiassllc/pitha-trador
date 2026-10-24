package mcpsettings

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/admin"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/mcpset"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/settings"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/shared"
	"github.com/ousiassllc/pitha-trador/internal/web/organisms/mcp"
)

// Admin is the subset of *admin.Service the handlers need.
type Admin interface {
	View(ctx context.Context) admin.View
	Update(ctx context.Context, enabled bool, listen string) error
	RegenerateKey(ctx context.Context) (string, error)
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	Restart(ctx context.Context) error
}

// WriteGate is the settings-write switch and audit log (issue #806); the
// application's *mcpset.Gate implements it.
type WriteGate interface {
	ExperimentGate
	Enabled(ctx context.Context) (bool, error)
	SetEnabled(ctx context.Context, enabled bool) error
	Recent(ctx context.Context, limit int) ([]mcpset.Entry, error)
	// The order-proposal intake switch (issue #827): off while nothing is
	// stored. Turning it on is the Settings screen's alone.
	OrderProposalsEnabled(ctx context.Context) (bool, error)
	SetOrderProposalsEnabled(ctx context.Context, enabled bool) error
}

// Handler implements the MCP settings routes.
type Handler struct {
	admin Admin
	gate  WriteGate // nil: the write section is hidden and its route absent
}

// New returns a Handler over a.
func New(a Admin) *Handler { return &Handler{admin: a} }

// WithWriteGate shows the settings-write switch and the audit log and mounts
// `POST /settings/mcp/write`.
func (h *Handler) WithWriteGate(g WriteGate) *Handler {
	h.gate = g
	return h
}

// Register mounts the routes on engine.
func (h *Handler) Register(engine *gin.Engine) {
	engine.GET("/settings/mcp-panel", h.Panel)
	engine.POST("/settings/mcp", h.Save)
	engine.POST("/settings/mcp/key", h.Regenerate)
	engine.POST("/settings/mcp/start", h.Start)
	engine.POST("/settings/mcp/stop", h.Stop)
	engine.POST("/settings/mcp/restart", h.Restart)
	if h.gate != nil {
		engine.POST("/settings/mcp/write", h.SaveWrite)
		engine.POST("/settings/mcp/order-proposals", h.SaveOrderProposals)
		engine.GET("/settings/mcp/experiments", h.Experiments)
		engine.POST("/settings/mcp/experiments", h.CreateExperiment)
		engine.GET("/settings/mcp/experiments/:id", h.CompareExperiment)
		engine.POST("/settings/mcp/experiments/:id/close", h.CloseExperiment)
	}
}

// SaveWrite implements `POST /settings/mcp/write`: form `enabled` (a checked
// checkbox sends a value, an unchecked one nothing) is the settings-write
// switch. Like every POST it passes the session cookie and CSRF check.
func (h *Handler) SaveWrite(c *gin.Context) {
	noStore(c)
	ctx := c.Request.Context()
	on := c.PostForm("enabled") != ""
	if err := h.gate.SetEnabled(ctx, on); err != nil {
		slog.ErrorContext(ctx, "mcpsettings: save write switch", "error", err)
		shared.RespondActionError(c, http.StatusInternalServerError, "設定変更の許可を保存できませんでした。")
		return
	}
	notice := "MCP からの設定変更を禁止しました。"
	if on {
		notice = "MCP からの設定変更を許可しました。"
	}
	h.render(c, notice, "")
}

// SaveOrderProposals implements `POST /settings/mcp/order-proposals`: form
// `enabled` (a checked checkbox sends a value, an unchecked one nothing) is the
// order-proposal intake switch (issue #827). It is the only way to turn the
// intake on; the MCP can only turn it off. Like every POST it passes the
// session cookie and CSRF check.
func (h *Handler) SaveOrderProposals(c *gin.Context) {
	noStore(c)
	ctx := c.Request.Context()
	on := c.PostForm("enabled") != ""
	if err := h.gate.SetOrderProposalsEnabled(ctx, on); err != nil {
		slog.ErrorContext(ctx, "mcpsettings: save order proposals switch", "error", err)
		shared.RespondActionError(c, http.StatusInternalServerError, "AIからの注文提案の受け付けを保存できませんでした。")
		return
	}
	notice := "AIからの注文提案の受け付けをオフにしました。"
	if on {
		notice = "AIからの注文提案の受け付けをオンにしました。"
	}
	h.render(c, notice, "")
}

// Panel implements `GET /settings/mcp-panel`: the panel with the key masked.
func (h *Handler) Panel(c *gin.Context) {
	h.render(c, "", "")
}

// Save implements `POST /settings/mcp`: form `enabled` (a checked checkbox
// sends a value, an unchecked one nothing) and `listen`. An address outside
// the loopback / tailnet rule is a 422 and nothing is saved; success
// re-renders the panel.
func (h *Handler) Save(c *gin.Context) {
	noStore(c)
	ctx := c.Request.Context()
	err := h.admin.Update(ctx, c.PostForm("enabled") != "", c.PostForm("listen"))
	switch {
	case errors.Is(err, settings.ErrListenNotAllowed):
		shared.RespondActionError(c, http.StatusUnprocessableEntity, "待受アドレスが許可されていません。127.0.0.1 などの loopback か、この PC の tailnet アドレス（host:port の IP リテラル）だけ指定できます。0.0.0.0・LAN のアドレス・ホスト名は使えません。保存していません。")
		return
	case err != nil:
		slog.ErrorContext(ctx, "mcpsettings: save", "error", err)
		shared.RespondActionError(c, http.StatusInternalServerError, "MCP 設定の保存に失敗しました。")
		return
	}
	h.render(c, "保存しました。反映するには再起動してください。", "")
}

// Regenerate implements `POST /settings/mcp/key`: it generates a new key and
// answers with the panel including the one-time reveal block. It is the only
// response that ever contains the key, so it is never cacheable.
func (h *Handler) Regenerate(c *gin.Context) {
	noStore(c)
	ctx := c.Request.Context()
	key, err := h.admin.RegenerateKey(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "mcpsettings: regenerate key", "error", err)
		shared.RespondActionError(c, http.StatusInternalServerError, "API キーの生成に失敗しました。")
		return
	}
	h.render(c, "API キーを生成しました。MCP を再起動すると新しいキーが有効になります。", key)
}

func (h *Handler) render(c *gin.Context, notice, revealedKey string) {
	noStore(c)
	ctx := c.Request.Context()
	props := panelProps(h.admin.View(ctx), notice, revealedKey)
	h.addWrite(ctx, &props)
	shared.RenderHTML(c, http.StatusOK, mcp.Panel(props))
}

// noStore keeps every response of this package out of browser and proxy
// caches: the key action's body holds the plaintext key.
func noStore(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
}
