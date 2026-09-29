package router_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/router"
	"github.com/ousiassllc/pitha-trador/internal/web/middleware"
)

func loopbackRequest(method, path string) *http.Request {
	req := httptest.NewRequest(method, path, nil)
	req.Host = "127.0.0.1:48080"
	return req
}

// Issue #136: a DNS-rebinding page reaches the server as Host
// `evil.example` and must get neither the cookie/CSRF token nor a way to
// act.
func TestNew_WithAllowedHostsRefusesDNSRebindingRequests(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := router.New(router.WithAllowedHosts(middleware.LoopbackHosts()...))

	rec := httptest.NewRecorder()
	rebound := loopbackRequest(http.MethodGet, "/scanner")
	rebound.Host = "evil.example:48080"
	engine.ServeHTTP(rec, rebound)
	if rec.Code != http.StatusForbidden || len(rec.Result().Cookies()) != 0 || csrfMetaPattern.MatchString(rec.Body.String()) {
		t.Fatalf("GET /scanner with Host evil.example = %d cookies=%v, want 403 with no cookie/token", rec.Code, rec.Result().Cookies())
	}

	// Tokens legitimately obtained via the real host are useless on the
	// rebound one.
	page := httptest.NewRecorder()
	engine.ServeHTTP(page, loopbackRequest(http.MethodGet, "/scanner"))
	match := csrfMetaPattern.FindStringSubmatch(page.Body.String())
	if page.Code != http.StatusOK || len(page.Result().Cookies()) != 1 || match == nil {
		t.Fatalf("GET /scanner on 127.0.0.1 = %d, want 200 with cookie and csrf token", page.Code)
	}
	for _, path := range []string{"/api/v1/system/kill", "/positions/1/close"} {
		req := loopbackRequest(http.MethodPost, path)
		req.Host = "evil.example:48080"
		req.Header.Set("Origin", "http://evil.example:48080")
		req.AddCookie(page.Result().Cookies()[0])
		req.Header.Set(middleware.CSRFHeader, match[1])
		rec = httptest.NewRecorder()
		engine.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("POST %s with Host evil.example = %d, want 403", path, rec.Code)
		}
	}

	ws := loopbackRequest(http.MethodGet, "/ws/system")
	ws.Host = "evil.example:48080"
	ws.Header.Set("Upgrade", "websocket")
	ws.Header.Set("Connection", "Upgrade")
	ws.AddCookie(page.Result().Cookies()[0])
	rec = httptest.NewRecorder()
	engine.ServeHTTP(rec, ws)
	if rec.Code != http.StatusForbidden {
		t.Errorf("WebSocket upgrade with Host evil.example = %d, want 403", rec.Code)
	}
}

func TestNew_WithAllowedHostsServesAllowedHostsAndStatic(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := router.New(router.WithAllowedHosts(middleware.WailsHosts()...))

	for _, path := range []string{"/scanner", "/static/vendor/htmx.min.js"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Host = "wails.localhost"
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s with Host wails.localhost = %d, want 200", path, rec.Code)
		}
	}
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, loopbackRequest(http.MethodGet, "/scanner"))
	if rec.Code != http.StatusForbidden {
		t.Errorf("GET /scanner with Host 127.0.0.1 on a Wails-only allowlist = %d, want 403", rec.Code)
	}
}

// Issue #142: the Settings row form renders a hidden `_csrf` field, and a
// plain (non-HTMX, header-less) submission carrying it is accepted and
// redirected back; without it the POST is refused.
func TestNew_SettingsFallbackFormSubmitsCSRFTokenInHiddenField(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := fakeSecretsStore{}
	engine := router.New(router.WithSecretsStore(store))

	page := httptest.NewRecorder()
	engine.ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/setup", nil))
	match := csrfMetaPattern.FindStringSubmatch(page.Body.String())
	if match == nil || !strings.Contains(page.Body.String(), `<input type="hidden" name="_csrf" value="`+match[1]+`"`) {
		t.Fatalf("Setup page does not render a hidden _csrf field carrying the csrf token %v; body=%s", match, page.Body.String())
	}
	cookie := page.Result().Cookies()[0]

	submit := func(form url.Values) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/settings/JEV_API_KEY", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Referer", "http://127.0.0.1:48080/setup")
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)
		return rec
	}

	if rec := submit(url.Values{"value": {"nope"}}); rec.Code != http.StatusForbidden || store["JEV_API_KEY"] != "" {
		t.Fatalf("fallback POST without _csrf = %d store=%v, want 403 and no write", rec.Code, store)
	}
	rec := submit(url.Values{"value": {"secret"}, "_csrf": {match[1]}})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/setup" || store["JEV_API_KEY"] != "secret" {
		t.Fatalf("fallback POST with _csrf = %d Location=%q store=%v, want 303 to /setup and the write", rec.Code, rec.Header().Get("Location"), store)
	}
}
