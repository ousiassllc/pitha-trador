// Package apierrorpage turns a failed `/api/v1` response into an HTML error
// page for browser page navigations (issue #1196). It lives in its own package
// because internal/web/middleware is at the linterly directory limit.
package apierrorpage

import (
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/web/middleware"
)

// Fixed user-facing texts of APIErrorPage; never the handler's error text or
// the problem+json `errors[]`.
const (
	invalidMessage     = "入力内容を確認してください。期間・種類などの条件を見直して、もう一度お試しください。"
	forbiddenMessage   = "リクエストを受け付けられませんでした。ページを再読み込みして、もう一度お試しください。"
	notFoundMessage    = "対象が見つかりませんでした。"
	unavailableMessage = "現在この操作を受け付けられません。初期設定が完了しているか確認して、もう一度お試しください。"
	serverMessage      = "処理に失敗しました。時間をおいて、もう一度お試しください。"
	otherMessage       = "リクエストを処理できませんでした。"
)

// Handler returns Gin middleware that turns a failed `/api/v1` response
// (status >= 400) into render's error page when the request is a browser page
// navigation (Accept: text/html, not htmx, not a WebSocket upgrade). The
// Settings/Scanner download forms (`hx-disable`, plain GET to `/api/v1/...`)
// are such navigations; without this the operator is left on a raw
// application/problem+json page with no way back, which in the desktop app
// has neither a back button nor in-app navigation (issue #1196).
//
// Every other client (Lit fetch, MCP, API clients: Accept `*/*` or
// application/json) keeps the problem+json untouched, and so does every
// status < 400: the writer is only wrapped to withhold a >= 400 header and
// body, and forwards Flush/Hijack/Unwrap, so a ZIP or NDJSON stream passes
// through unchanged. render may be nil, which disables the middleware.
//
// Install it before Session and SetupGuard so their `/api/v1` rejections are
// covered too.
func Handler(render middleware.ErrorPageRenderer) gin.HandlerFunc {
	return func(c *gin.Context) {
		if render == nil || !wantsErrorPage(c) {
			c.Next()
			return
		}
		orig := c.Writer
		w := &errorCapturingWriter{ResponseWriter: orig}
		// Restored by defer as well, so a panic reaches Recovery with the
		// real writer and its 500 is not swallowed.
		defer func() { c.Writer = orig }()
		c.Writer = w
		c.Next()
		c.Writer = orig
		if !w.failed {
			return
		}
		h := orig.Header()
		for _, k := range []string{"Content-Type", "Content-Length", "Content-Encoding", "Content-Disposition", "Link"} {
			h.Del(k)
		}
		renderSafely(c, render, w.status, message(w.status))
		c.Abort()
	}
}

// renderSafely keeps a panicking renderer from losing the response: nothing
// was written yet, so the failure ends as a bare status.
func renderSafely(c *gin.Context, render middleware.ErrorPageRenderer, status int, msg string) {
	defer func() {
		if r := recover(); r != nil {
			slog.ErrorContext(c.Request.Context(), "apierrorpage: error page renderer panicked", "panic", r, "stack", string(debug.Stack()))
			c.AbortWithStatus(status)
		}
	}()
	render(c, status, msg)
}

// wantsErrorPage reports whether c is a page navigation to a registered
// `/api/v1` route (an unknown path stays the router's problem+json 404).
func wantsErrorPage(c *gin.Context) bool {
	path := c.Request.URL.Path
	return c.FullPath() != "" &&
		(path == "/api/v1" || strings.HasPrefix(path, "/api/v1/")) &&
		strings.Contains(c.GetHeader("Accept"), "text/html") &&
		c.GetHeader("HX-Request") != "true" &&
		!strings.EqualFold(c.GetHeader("Upgrade"), "websocket")
}

func message(status int) string {
	switch {
	case status == http.StatusBadRequest || status == http.StatusUnprocessableEntity:
		return invalidMessage
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return forbiddenMessage
	case status == http.StatusNotFound:
		return notFoundMessage
	case status == http.StatusServiceUnavailable:
		return unavailableMessage
	case status >= http.StatusInternalServerError:
		return serverMessage
	default:
		return otherMessage
	}
}

// errorCapturingWriter withholds a >= 400 status and its body from the
// underlying writer (nothing reaches the client until Handler renders the
// page) and forwards everything else. gin only commits a status on the first
// body write or WriteHeaderNow, so withholding is enough to keep the real
// writer uncommitted.
type errorCapturingWriter struct {
	gin.ResponseWriter
	failed bool
	status int
}

func (w *errorCapturingWriter) WriteHeader(code int) {
	if code >= http.StatusBadRequest {
		w.failed, w.status = true, code
		return
	}
	w.failed = false
	w.ResponseWriter.WriteHeader(code)
}

func (w *errorCapturingWriter) WriteHeaderNow() {
	if !w.failed {
		w.ResponseWriter.WriteHeaderNow()
	}
}

func (w *errorCapturingWriter) Write(b []byte) (int, error) {
	if w.failed {
		return len(b), nil
	}
	return w.ResponseWriter.Write(b)
}

func (w *errorCapturingWriter) WriteString(s string) (int, error) {
	if w.failed {
		return len(s), nil
	}
	return w.ResponseWriter.WriteString(s)
}

func (w *errorCapturingWriter) Status() int {
	if w.failed {
		return w.status
	}
	return w.ResponseWriter.Status()
}

// Flush must not commit the real writer's default 200 while a failure is
// pending.
func (w *errorCapturingWriter) Flush() {
	if !w.failed {
		w.ResponseWriter.Flush()
	}
}

// Unwrap lets http.ResponseController reach the real writer (Flush, Hijack,
// deadlines).
func (w *errorCapturingWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
