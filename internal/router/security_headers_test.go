package router_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/router"
	"github.com/ousiassllc/pitha-trador/internal/web/middleware"
)

// cspDirective returns the value of directive in csp ("" when absent).
func cspDirective(csp, directive string) string {
	for _, part := range strings.Split(csp, ";") {
		name, value, _ := strings.Cut(strings.TrimSpace(part), " ")
		if name == directive {
			return value
		}
	}
	return ""
}

// assertSecurityHeaders checks the headers every response must carry
// (issue #378) and returns the CSP for further inspection.
func assertSecurityHeaders(t *testing.T, label string, rec *httptest.ResponseRecorder) string {
	t.Helper()
	h := rec.Header()
	if got := h.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("%s: X-Content-Type-Options = %q, want nosniff", label, got)
	}
	if got := h.Get("X-Frame-Options"); got != "DENY" {
		t.Errorf("%s: X-Frame-Options = %q, want DENY", label, got)
	}
	if got := h.Get("Referrer-Policy"); got != "same-origin" {
		t.Errorf("%s: Referrer-Policy = %q, want same-origin", label, got)
	}
	csp := h.Get("Content-Security-Policy")
	if got := cspDirective(csp, "frame-ancestors"); got != "'none'" {
		t.Errorf("%s: CSP frame-ancestors = %q, want 'none' (csp=%q)", label, got, csp)
	}
	if got := cspDirective(csp, "script-src"); got != "'self'" && label != "/swagger" {
		t.Errorf("%s: CSP script-src = %q, want 'self'", label, got)
	}
	return csp
}

func TestNew_EveryRouteCarriesSecurityHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("SWAGGER_ENABLED", "true")
	engine := router.New(router.WithAllowedHosts(middleware.LoopbackHosts()...))

	for _, path := range []string{
		"/scanner",                   // SSR page
		"/api/v1/scanner",            // JSON API
		"/static/vendor/htmx.min.js", // static file
		"/swagger",                   // API docs
		"/no-such-route",             // NoRoute 404
		"/",                          // redirect
		"/system/status",             // HTMX fragment
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)
		assertSecurityHeaders(t, path, rec)
	}
}

func TestNew_RejectedRequestsCarrySecurityHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := router.New(router.WithAllowedHosts(middleware.LoopbackHosts()...))

	req := httptest.NewRequest(http.MethodGet, "/scanner", nil)
	req.Host = "evil.example"
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	assertSecurityHeaders(t, "HostGuard 403", rec)
}

func TestNew_CSPForbidsInlineAndEvalExceptSwaggerStyles(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("SWAGGER_ENABLED", "true")
	engine := router.New()

	get := func(path string) string {
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec.Header().Get("Content-Security-Policy")
	}

	page := get("/scanner")
	for _, dir := range []string{"default-src", "script-src", "style-src", "connect-src"} {
		v := cspDirective(page, dir)
		if strings.Contains(v, "'unsafe-inline'") || strings.Contains(v, "'unsafe-eval'") {
			t.Errorf("page CSP %s = %q, must not allow inline/eval", dir, v)
		}
	}
	if got := cspDirective(page, "connect-src"); got != "'self'" {
		t.Errorf("page CSP connect-src = %q, want 'self' without a WebSocket base", got)
	}

	swagger := get("/swagger")
	if got := cspDirective(swagger, "style-src"); !strings.Contains(got, "'unsafe-inline'") {
		t.Errorf("swagger CSP style-src = %q, want 'unsafe-inline' for Stoplight Elements", got)
	}
	if got := cspDirective(swagger, "script-src"); got != "'self'" {
		t.Errorf("swagger CSP script-src = %q, want 'self'", got)
	}
}

func TestNew_WithWebSocketBaseAllowsItInConnectSrc(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const base = "ws://wails.localhost:51234"
	engine := router.New(router.WithWebSocketBase(base))

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/scanner", nil))

	got := cspDirective(rec.Header().Get("Content-Security-Policy"), "connect-src")
	if got != "'self' "+base {
		t.Errorf("CSP connect-src = %q, want %q", got, "'self' "+base)
	}
}
