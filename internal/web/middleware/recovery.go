package middleware

import (
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/gin-gonic/gin"
)

// Recovery returns Gin middleware that turns a handler panic into a logged
// 500 instead of letting it escape to the HTTP server. net/http alone only
// drops the connection and prints an unstructured stack to stderr, and
// cmd/desktop (Wails AssetServer) has no defined behaviour for handler
// panics; here the panic value, request line and stack are written to the
// process-wide slog.Default logger (internal/logging: JSON).
//
// http.ErrAbortHandler is re-panicked untouched: it is net/http's
// sanctioned way to abort a response silently. If the handler had already
// started the response (or hijacked the connection for a WebSocket) no
// status is written, as it can no longer be changed.
//
// Install it after RequestLog so the recovered 500 is what RequestLog
// records.
func Recovery() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			r := recover()
			if r == nil {
				return
			}
			if r == http.ErrAbortHandler {
				panic(r)
			}
			slog.ErrorContext(c.Request.Context(), "middleware: handler panicked",
				"method", c.Request.Method,
				"path", c.Request.URL.Path,
				"panic", r,
				"stack", string(debug.Stack()))
			if c.Writer.Written() {
				c.Abort()
				return
			}
			c.AbortWithStatus(http.StatusInternalServerError)
		}()
		c.Next()
	}
}
