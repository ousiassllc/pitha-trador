package mcpsettings

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/proc"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/settings"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/shared"
)

// Start implements `POST /settings/mcp/start`: launch paper-results-mcp as a
// child process. Like every POST it passes through middleware.Session (the
// session cookie plus the CSRF token), so only the operator's own WebView or
// browser tab can start it; there is no GET that starts anything.
func (h *Handler) Start(c *gin.Context) {
	h.process(c, "起動しました。", h.admin.Start)
}

// Stop implements `POST /settings/mcp/stop`.
func (h *Handler) Stop(c *gin.Context) {
	h.process(c, "停止しました。", h.admin.Stop)
}

// Restart implements `POST /settings/mcp/restart`: stop (if running) and
// start with the saved settings, so a changed address or a regenerated key
// takes effect.
func (h *Handler) Restart(c *gin.Context) {
	h.process(c, "再起動しました。", h.admin.Restart)
}

func (h *Handler) process(c *gin.Context, done string, run func(ctx context.Context) error) {
	noStore(c)
	ctx := c.Request.Context()
	if err := run(ctx); err != nil {
		if errors.Is(err, context.Canceled) {
			// The operator closed the page mid-request; what was started
			// stays started and the next panel read shows it.
			c.Status(http.StatusRequestTimeout)
			c.Abort()
			return
		}
		// A failure can change the state shown (error, stopped): ask the
		// open panel to reload itself, since an error response is not swapped
		// into it.
		c.Header("HX-Trigger", "mcp-refresh")
		status, message := processFailure(err)
		if status >= http.StatusInternalServerError {
			slog.ErrorContext(ctx, "mcpsettings: process control", "error", err)
		}
		shared.RespondActionError(c, status, message)
		return
	}
	h.render(c, done, "")
}

// processFailure maps proc's fixed errors to a status and a one-line toast
// sentence (design guide O3: a toast states the fact only). The reason in
// detail and the remedy are worded in the panel (stateNote, which the
// HX-Trigger mcp-refresh reloads).
func processFailure(err error) (int, string) {
	switch {
	case errors.Is(err, proc.ErrAlreadyRunning):
		return http.StatusConflict, "すでに起動しています。"
	case errors.Is(err, proc.ErrAlreadyListening):
		return http.StatusConflict, "すでに別の MCP が応答しています。"
	case errors.Is(err, proc.ErrNotManaged):
		return http.StatusConflict, "この画面からは停止できません。"
	case errors.Is(err, proc.ErrNotRunning):
		return http.StatusConflict, "起動していません。"
	case errors.Is(err, proc.ErrExeMissing):
		return http.StatusConflict, "実行ファイルが見つかりません。"
	case errors.Is(err, proc.ErrClosed):
		return http.StatusConflict, "アプリの終了中は起動できません。"
	case errors.Is(err, proc.ErrDisabled):
		return http.StatusUnprocessableEntity, "MCP が無効です。"
	case errors.Is(err, proc.ErrNoKey):
		return http.StatusUnprocessableEntity, "API キーが未設定です。"
	case errors.Is(err, proc.ErrSettingsUnusable):
		return http.StatusUnprocessableEntity, "設定ファイルを使えません。"
	case errors.Is(err, proc.ErrAccessIncomplete):
		return http.StatusUnprocessableEntity, "Access の認証情報が空です。"
	case errors.Is(err, settings.ErrListenNotAllowed):
		return http.StatusUnprocessableEntity, "待受アドレスが許可範囲外です。"
	case errors.Is(err, proc.ErrExitedEarly):
		return http.StatusInternalServerError, "起動後すぐに終了しました。"
	default:
		return http.StatusInternalServerError, "起動できませんでした。"
	}
}
