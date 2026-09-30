package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/web/middleware"
)

// hostGuardEngine puts HostGuard in front of Session, as router.New does.
func hostGuardEngine(t *testing.T, allowed ...string) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(middleware.HostGuard(allowed), middleware.NewSession(nil).Handler())
	engine.GET("/page", func(c *gin.Context) { c.String(http.StatusOK, middleware.CSRFToken(c.Request.Context())) })
	engine.GET("/static/x", func(c *gin.Context) { c.String(http.StatusOK, "asset") })
	engine.GET("/ws", func(c *gin.Context) { c.String(http.StatusOK, "upgraded") })
	engine.POST("/act", func(c *gin.Context) { c.String(http.StatusOK, "done") })
	return engine
}

func hostRequest(method, path, host, origin string) *http.Request {
	req := httptest.NewRequest(method, path, nil)
	req.Host = host
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	return req
}

func TestHostGuard_ForeignHostGetsNoCookieNoTokenNoAsset(t *testing.T) {
	engine := hostGuardEngine(t, middleware.LoopbackHosts()...)

	for _, path := range []string{"/page", "/static/x"} {
		rec := do(engine, hostRequest(http.MethodGet, path, "evil.example:48080", ""))
		if rec.Code != http.StatusForbidden {
			t.Errorf("GET %s with Host evil.example = %d, want 403", path, rec.Code)
		}
		if got := rec.Result().Cookies(); len(got) != 0 || rec.Body.String() == "asset" {
			t.Errorf("GET %s with Host evil.example leaked cookies=%v body=%q", path, got, rec.Body.String())
		}
	}
}

func TestHostGuard_ForeignHostCannotPostEvenWithValidTokens(t *testing.T) {
	engine := hostGuardEngine(t, middleware.LoopbackHosts()...)
	login := do(engine, hostRequest(http.MethodGet, "/page", "127.0.0.1:48080", ""))
	cookie, csrf := login.Result().Cookies()[0], login.Body.String()

	req := hostRequest(http.MethodPost, "/act", "evil.example:48080", "http://evil.example:48080")
	req.AddCookie(cookie)
	req.Header.Set(middleware.CSRFHeader, csrf)
	if rec := do(engine, req); rec.Code != http.StatusForbidden {
		t.Fatalf("POST with Host evil.example = %d, want 403", rec.Code)
	}
}

func TestHostGuard_AllowsConfiguredHostsInEveryNotation(t *testing.T) {
	engine := hostGuardEngine(t, append(middleware.LoopbackHosts(), middleware.WailsHosts()...)...)

	for _, host := range []string{"127.0.0.1:48080", "127.0.0.1", "localhost:1", "LOCALHOST", "localhost.", "[::1]:48080", "[::1]", "wails.localhost", "wails"} {
		if rec := do(engine, hostRequest(http.MethodGet, "/page", host, "")); rec.Code != http.StatusOK {
			t.Errorf("Host %q = %d, want 200", host, rec.Code)
		}
	}
	for _, host := range []string{"", "localhost.evil.example", "evil.example", "127.0.0.2:48080", "[::2]:48080"} {
		if rec := do(engine, hostRequest(http.MethodGet, "/page", host, "")); rec.Code != http.StatusForbidden {
			t.Errorf("Host %q = %d, want 403", host, rec.Code)
		}
	}
}

func TestHostGuard_OriginMustBeAllowedOnStateChangesAndWebSocketUpgrades(t *testing.T) {
	engine := hostGuardEngine(t, middleware.LoopbackHosts()...)
	login := do(engine, hostRequest(http.MethodGet, "/page", "127.0.0.1:48080", ""))
	cookie, csrf := login.Result().Cookies()[0], login.Body.String()

	post := func(origin string) int {
		req := hostRequest(http.MethodPost, "/act", "127.0.0.1:48080", origin)
		req.AddCookie(cookie)
		req.Header.Set(middleware.CSRFHeader, csrf)
		return do(engine, req).Code
	}
	upgrade := func(origin string) int {
		req := hostRequest(http.MethodGet, "/ws", "127.0.0.1:48080", origin)
		req.AddCookie(cookie)
		req.Header.Set("Upgrade", "websocket")
		return do(engine, req).Code
	}
	for name, status := range map[string]func(string) int{"POST": post, "WebSocket upgrade": upgrade} {
		for origin, want := range map[string]int{
			"":                         http.StatusOK, // non-browser client
			"http://127.0.0.1:48080":   http.StatusOK,
			"http://localhost:48080":   http.StatusOK,
			"http://evil.example":      http.StatusForbidden,
			"null":                     http.StatusForbidden,
			"http://127.0.0.1.evil.io": http.StatusForbidden,
		} {
			if got := status(origin); got != want {
				t.Errorf("%s with Origin %q = %d, want %d", name, origin, got, want)
			}
		}
	}

	// A foreign Origin on a plain page load is not a state change.
	if rec := do(engine, hostRequest(http.MethodGet, "/page", "127.0.0.1:48080", "http://evil.example")); rec.Code != http.StatusOK {
		t.Errorf("GET with foreign Origin = %d, want 200", rec.Code)
	}
}
