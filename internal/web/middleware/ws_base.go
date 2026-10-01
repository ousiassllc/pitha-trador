package middleware

import (
	"context"

	"github.com/gin-gonic/gin"
)

type wsBaseContextKey struct{}

// WebSocketBase puts base (e.g. `ws://wails.localhost:51234`) on every
// request context so layout.Shell can tell the page's Lit components where
// to open their WebSockets (see WebSocketBaseURL). cmd/desktop needs this:
// the Wails AssetServer cannot carry WebSockets, so they go to a separate
// loopback listener (issue #266).
func WebSocketBase(base string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), wsBaseContextKey{}, base))
		c.Next()
	}
}

// WebSocketBaseURL returns the base WebSocketBase put on ctx, or "" when
// WebSockets share the page's own origin (cmd/server).
func WebSocketBaseURL(ctx context.Context) string {
	base, _ := ctx.Value(wsBaseContextKey{}).(string)
	return base
}
