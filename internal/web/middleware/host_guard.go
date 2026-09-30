package middleware

import (
	"net"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
)

// LoopbackHosts are the hostnames a browser uses to reach cmd/server's
// loopback-bound listener.
func LoopbackHosts() []string {
	return []string{"localhost", "127.0.0.1", "::1"}
}

// WailsHosts are the hostnames the Wails AssetServer presents to the Gin
// engine: `wails.localhost` (Windows/WebView2) and `wails` (`wails://wails/`
// on macOS/Linux).
func WailsHosts() []string {
	return []string{"wails.localhost", "wails"}
}

// HostGuard rejects (403) every request, `/static` included, whose Host
// header names a host outside allowed, so a DNS-rebinding page
// (`evil.example` resolving to 127.0.0.1) can neither obtain the session
// cookie/CSRF token nor call the API (issue #136): to the browser such a
// page is "same origin" with this app, which Session's defence otherwise
// relies on not happening. Entries of allowed are hostnames (an entry's
// port, if any, is ignored, as is the request's: only a hostname can be
// rebound); IP literals cannot be rebound and need no DNS.
//
// State-changing requests and WebSocket upgrades that carry an Origin
// header additionally need it to name an allowed host (an Origin of `null`
// never does); requests without one (non-browser clients) pass.
//
// Install it before Session.Handler.
func HostGuard(allowed []string) gin.HandlerFunc {
	hosts := make(map[string]struct{}, len(allowed))
	for _, entry := range allowed {
		hosts[hostname(entry)] = struct{}{}
	}
	permitted := func(hostport string) bool {
		_, ok := hosts[hostname(hostport)]
		return ok
	}
	return func(c *gin.Context) {
		if !permitted(c.Request.Host) {
			forbid(c, "host not allowed")
			return
		}
		if origin := c.GetHeader("Origin"); origin != "" && (!isSafeMethod(c.Request.Method) || isWebSocketUpgrade(c.Request)) {
			parsed, err := url.Parse(origin)
			if err != nil || !permitted(parsed.Host) {
				forbid(c, "origin not allowed")
				return
			}
		}
		c.Next()
	}
}

// hostname lower-cases hostport and strips its port, IPv6 brackets and any
// trailing root dot.
func hostname(hostport string) string {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	return strings.TrimSuffix(strings.ToLower(strings.Trim(host, "[]")), ".")
}
