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
	h.process(c, "再起動しました。保存済みの設定が有効になっています。", h.admin.Restart)
}

func (h *Handler) process(c *gin.Context, done string, run func(ctx context.Context) error) {
	noStore(c)
	ctx := c.Request.Context()
	if err := run(ctx); err != nil {
		status, message := processFailure(err)
		if status >= http.StatusInternalServerError {
			slog.ErrorContext(ctx, "mcpsettings: process control", "error", err)
		}
		shared.RespondActionError(c, status, message)
		return
	}
	h.render(c, done, "")
}

// processFailure maps proc's fixed errors to a status and a fixed sentence.
func processFailure(err error) (int, string) {
	switch {
	case errors.Is(err, proc.ErrAlreadyRunning):
		return http.StatusConflict, "すでに起動しています（二重には起動しません）。"
	case errors.Is(err, proc.ErrAlreadyListening):
		return http.StatusConflict, "この待受アドレスでは、すでに MCP が応答しています（アプリの外で起動されたものです）。二重には起動しません。"
	case errors.Is(err, proc.ErrNotManaged):
		return http.StatusConflict, "このアプリが起動した MCP ではないため、この画面からは停止できません。"
	case errors.Is(err, proc.ErrNotRunning):
		return http.StatusConflict, "起動していません。"
	case errors.Is(err, proc.ErrExeMissing):
		return http.StatusConflict, "paper-results-mcp の実行ファイルが見つかりません。アプリ本体と同じフォルダに置いてください。"
	case errors.Is(err, proc.ErrClosed):
		return http.StatusConflict, "アプリを終了しているため、起動できません。"
	case errors.Is(err, proc.ErrDisabled):
		return http.StatusUnprocessableEntity, "MCP が無効です。有効にして保存してから起動してください。"
	case errors.Is(err, proc.ErrNoKey):
		return http.StatusUnprocessableEntity, "API キーが未設定です。キーを生成してから起動してください。"
	case errors.Is(err, proc.ErrSettingsUnusable):
		return http.StatusUnprocessableEntity, "設定ファイルを使えないため起動できません。保存または API キーの生成で作り直してください。"
	case errors.Is(err, proc.ErrAccessIncomplete):
		return http.StatusUnprocessableEntity, "Cloudflare Access のオリジン照合が有効なのに ID またはシークレットが空のため、起動できません。"
	case errors.Is(err, settings.ErrListenNotAllowed):
		return http.StatusUnprocessableEntity, "保存されている待受アドレスが許可範囲外のため起動できません。許可されたアドレスで保存し直してください。"
	case errors.Is(err, proc.ErrExitedEarly):
		return http.StatusInternalServerError, "起動しましたが、すぐに終了しました。状態表示の理由を確認してください。"
	default:
		return http.StatusInternalServerError, "MCP を起動できませんでした。状態表示の理由を確認してください。"
	}
}
