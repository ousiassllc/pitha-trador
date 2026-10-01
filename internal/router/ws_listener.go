package router

import (
	"net/http"
	"strings"
)

// WebSocketOnly wraps next so that it answers only WebSocket upgrades of
// `/ws/...` routes and 404s everything else. cmd/desktop serves it on a
// loopback listener next to the Wails AssetServer, which cannot carry
// WebSockets (issue #266): that listener must not expose the pages or the
// API a second time.
func WebSocketOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/ws/") || !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
			http.NotFound(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}
