package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"
)

// SecurityHeaders sets the defence-in-depth response headers on every
// response (issue #378): a strict Content-Security-Policy, `nosniff`,
// clickjacking protection and a Referrer-Policy. wsBase is the separate
// WebSocket listener's base (e.g. `ws://wails.localhost:51234`, see
// WebSocketBase) or "" when WebSockets share the page's origin; it is added
// to `connect-src`, since `'self'` does not cover another port.
//
// The headers are set before c.Next, so a handler may override one (the
// `/swagger` page replaces the CSP with SwaggerCSP) and rejections from later
// middleware (HostGuard 403, Session 403, Recovery 500) carry them too.
// Install it first.
func SecurityHeaders(wsBase string) gin.HandlerFunc {
	csp := contentSecurityPolicy(wsBase)
	return func(c *gin.Context) {
		h := c.Writer.Header()
		h.Set("Content-Security-Policy", csp)
		h.Set("X-Content-Type-Options", "nosniff")
		// Legacy equivalent of `frame-ancestors 'none'` for older WebViews.
		h.Set("X-Frame-Options", "DENY")
		// `same-origin` (not `no-referrer`): settings handlers derive the
		// return path from the same-origin Referer's path.
		h.Set("Referrer-Policy", "same-origin")
		c.Next()
	}
}

// lightweightChartsAttributionStyleHash allows the one inline `<style>`
// element lightweight-charts (static/package.json, 4.x) appends for its
// TradingView attribution logo, which the library's licence asks pages to
// show. It is the SHA-256 of that element's text; re-compute it (the CSP
// violation console message prints it) when upgrading lightweight-charts.
const lightweightChartsAttributionStyleHash = "'sha256-3pRED1tOXas1FXFoPb9TGCjmYe9XQsmO9OV23khV2nY='"

// contentSecurityPolicy is the policy every page and API response gets. All
// scripts and stylesheets are same-origin files under `/static` (htmx is
// vendored, Lit components use constructable stylesheets and set per-element
// styles through the CSSOM, and the htmx `htmx-config` meta turns off
// `allowEval`/`allowScriptTags` and `includeIndicatorStyles`), so neither
// `'unsafe-inline'` nor `'unsafe-eval'` is needed.
func contentSecurityPolicy(wsBase string) string {
	connect := "'self'"
	if wsBase != "" {
		connect += " " + wsBase
	}
	return strings.Join([]string{
		"default-src 'self'",
		"script-src 'self'",
		"style-src 'self' " + lightweightChartsAttributionStyleHash,
		"img-src 'self' data:",
		"font-src 'self'",
		"connect-src " + connect,
		"object-src 'none'",
		"base-uri 'self'",
		"form-action 'self'",
		"frame-ancestors 'none'",
	}, "; ")
}

// SwaggerCSP is the policy for the `/swagger` page: Stoplight Elements
// (React + styled inline styles) injects `<style>` elements and style
// attributes at runtime, so styles may be inline; scripts stay same-origin
// only and framing stays denied. `/swagger` is opt-in (SWAGGER_ENABLED) and
// serves a static page.
const SwaggerCSP = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data:; font-src 'self' data:; connect-src 'self'; object-src 'none'; " +
	"base-uri 'self'; form-action 'self'; frame-ancestors 'none'"
