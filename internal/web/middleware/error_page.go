package middleware

import (
	"log/slog"
	"strings"

	"github.com/gin-gonic/gin"
)

// ErrorPageRenderer writes a full HTML error page (status line and body) for
// c. pages depends on middleware (layout.Shell reads CSRFToken), so Recovery
// and Session cannot call pages.ErrorPage themselves; internal/router
// injects the renderer instead (issue #171, follow-up of #143).
type ErrorPageRenderer func(c *gin.Context, status int, message string)

// respondError answers a middleware-level failure. Only a plain page
// navigation (see wantsHTMLPage) gets render's page; every other client -
// JSON API, WebSocket upgrade, htmx (whose `pitha-htmx-errors` toast reads
// the status/headers) - keeps fallback's status/body. A nil render, or one
// that panics before writing anything, also ends in fallback.
func respondError(c *gin.Context, render ErrorPageRenderer, status int, message string, fallback func()) {
	if render != nil && wantsHTMLPage(c) {
		renderSafely(c, render, status, message)
		if c.Writer.Written() {
			c.Abort()
			return
		}
	}
	fallback()
}

func renderSafely(c *gin.Context, render ErrorPageRenderer, status int, message string) {
	defer func() {
		if r := recover(); r != nil {
			slog.ErrorContext(c.Request.Context(), "middleware: error page renderer panicked", "panic", r)
		}
	}()
	render(c, status, message)
}

// wantsHTMLPage reports whether c is a browser page navigation or no-JS form
// submission: it accepts text/html and is neither an htmx request, a
// WebSocket upgrade nor an `/api/v1` call. fetch() from the Lit components
// sends `Accept: */*` and so never matches.
func wantsHTMLPage(c *gin.Context) bool {
	path := c.Request.URL.Path
	return strings.Contains(c.GetHeader("Accept"), "text/html") &&
		c.GetHeader("HX-Request") != "true" &&
		!isWebSocketUpgrade(c.Request) &&
		path != "/api/v1" && !strings.HasPrefix(path, "/api/v1/")
}
