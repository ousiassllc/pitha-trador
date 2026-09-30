package router_test

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/router"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/system"
	"github.com/ousiassllc/pitha-trador/internal/web/middleware"
)

var csrfMetaPattern = regexp.MustCompile(`<meta name="csrf-token" content="([^"]+)"`)

// authorize attaches what a browser that already loaded a page from engine
// would send on req: the session cookie and the CSRF token rendered into
// the page's `<meta name="csrf-token">` (issues #90/#98/#99).
func authorize(t *testing.T, engine *gin.Engine, req *http.Request) *http.Request {
	t.Helper()
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/setup", nil))
	cookies := rec.Result().Cookies()
	match := csrfMetaPattern.FindStringSubmatch(rec.Body.String())
	if len(cookies) != 1 || match == nil {
		t.Fatalf("GET /setup gave cookies=%v csrf meta=%v, want one session cookie and a csrf-token meta", cookies, match)
	}
	req.AddCookie(cookies[0])
	req.Header.Set(middleware.CSRFHeader, match[1])
	return req
}

func TestNew_StateChangingRoutesRejectRequestsWithoutSessionCookieAndCSRFToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := router.New(
		router.WithSystemEngine(system.StaticSystemEngine{State_: domain.SystemStateKilled}),
		router.WithSecretsStore(requiredSecretsStore()),
	)

	for _, tc := range []struct{ method, path string }{
		{http.MethodPost, "/positions/1/close"},
		{http.MethodPost, "/settings/JEV_API_KEY"},
		{http.MethodDelete, "/settings/JEV_API_KEY"},
		{http.MethodPost, "/system/update-check"},
		{http.MethodPost, "/api/v1/system/pause"},
		{http.MethodPost, "/api/v1/system/resume"},
		{http.MethodPost, "/api/v1/system/kill"},
	} {
		// Neither cookie nor token.
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, nil))
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s %s without cookie/token = %d, want 403", tc.method, tc.path, rec.Code)
		}

		// Valid cookie but no CSRF token (what a cross-site form POST would
		// carry if SameSite were ignored).
		req := authorize(t, engine, httptest.NewRequest(tc.method, tc.path, nil))
		req.Header.Del(middleware.CSRFHeader)
		rec = httptest.NewRecorder()
		engine.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s %s with cookie but no CSRF token = %d, want 403", tc.method, tc.path, rec.Code)
		}
	}
}

func TestNew_WebSocketRoutesRejectUpgradeWithoutSessionCookie(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := router.New()

	for _, path := range []string{"/ws/scanner", "/ws/system", "/ws/activity", "/ws/symbols/7203"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Upgrade", "websocket")
		req.Header.Set("Connection", "Upgrade")
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("GET %s upgrade without cookie = %d, want 403", path, rec.Code)
		}
	}
}

func TestNew_PagesEmbedCSRFTokenForHTMXAndLit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := router.New()

	for _, path := range []string{"/scanner", "/settings", "/setup"} {
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		body := rec.Body.String()
		match := csrfMetaPattern.FindStringSubmatch(body)
		if match == nil {
			t.Fatalf("GET %s renders no <meta name=\"csrf-token\">; body=%s", path, body)
		}
		wantHeaders := `hx-headers="{&#34;X-CSRF-Token&#34;:&#34;` + match[1] + `&#34;}"`
		if !strings.Contains(body, wantHeaders) {
			t.Errorf("GET %s <body> lacks %s; body=%s", path, wantHeaders, body)
		}
	}
}

// A cookie-less form POST from a page navigation used to get a bare
// "forbidden: ..." text body; it now gets the ErrorPage (issue #171) while
// keeping the stale-session marker header. Non-navigation requests are
// unchanged.
func TestNew_SessionRejectionOfPageNavigationRendersErrorPage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := router.New(router.WithSystemEngine(system.StaticSystemEngine{State_: domain.SystemStateKilled}))

	req := httptest.NewRequest(http.MethodPost, "/settings/JEV_API_KEY", nil)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `data-testid="error-page"`) {
		t.Errorf("body has no error page: %q", rec.Body.String())
	}
	if got := rec.Header().Get(middleware.CSRFRejectHeader); got != middleware.CSRFRejectStale {
		t.Errorf("%s = %q, want %q", middleware.CSRFRejectHeader, got, middleware.CSRFRejectStale)
	}

	req = httptest.NewRequest(http.MethodPost, "/settings/JEV_API_KEY", nil)
	req.Header.Set("HX-Request", "true")
	req.Header.Set("Accept", "text/html")
	rec = httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden || strings.Contains(rec.Body.String(), `data-testid="error-page"`) {
		t.Errorf("htmx: status=%d body=%q, want 403 without the error page", rec.Code, rec.Body.String())
	}
}
