package settingsroutes_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/web/middleware"
)

var csrfMeta = regexp.MustCompile(`<meta name="csrf-token" content="([^"]+)"`)

// send performs method path as a browser that already loaded /setup would
// (session cookie + CSRF token, which the Setup Guard sits behind).
func send(t *testing.T, engine *gin.Engine, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	page := get(engine, "/setup")
	cookies := page.Result().Cookies()
	match := csrfMeta.FindStringSubmatch(page.Body.String())
	if len(cookies) != 1 || match == nil {
		t.Fatalf("GET /setup gave cookies=%v csrf meta=%v, want a session cookie and a csrf-token meta", cookies, match)
	}
	req := httptest.NewRequest(method, path, strings.NewReader(url.Values{"value": {"on"}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req.AddCookie(cookies[0])
	req.Header.Set(middleware.CSRFHeader, match[1])
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

func redirectsToSetup(rec *httptest.ResponseRecorder) bool {
	return rec.Code == http.StatusNoContent && rec.Header().Get("HX-Redirect") == "/setup"
}

// issue #1175: on the production-wired router, while setup is incomplete the
// Setup Guard lets through exactly what the Setup screen needs - the save and
// delete routes of every allowed secrets key (`POST`/`DELETE /settings/:key`)
// and the broker selection rows (`/ops-settings/:key`) - and keeps every other
// POST/DELETE under `/settings/` (jev-trial, mcp, ai-roles, ...) closed.
func TestSetupGuard_OpensOnlySetupWriteRoutes(t *testing.T) {
	engine := newEngine(t, secrets{full: false})

	for _, key := range config.AllowedSecretKeys() {
		for _, method := range []string{http.MethodPost, http.MethodDelete} {
			if rec := send(t, engine, method, "/settings/"+key); redirectsToSetup(rec) {
				t.Errorf("%s /settings/%s is closed by the Setup Guard, want it open for setup", method, key)
			}
		}
	}
	for _, key := range []string{"broker.provider", "broker.tachibana.demo.private_key_path"} {
		for _, method := range []string{http.MethodPost, http.MethodDelete} {
			if rec := send(t, engine, method, "/ops-settings/"+key); redirectsToSetup(rec) {
				t.Errorf("%s /ops-settings/%s is closed by the Setup Guard, want it open for setup", method, key)
			}
		}
	}

	closed := 0
	for _, r := range engine.Routes() {
		if (r.Method != http.MethodPost && r.Method != http.MethodDelete) || !strings.HasPrefix(r.Path, "/settings/") || r.Path == "/settings/:key" {
			continue
		}
		path := regexp.MustCompile(`:[A-Za-z]+`).ReplaceAllString(r.Path, "x")
		if rec := send(t, engine, r.Method, path); !redirectsToSetup(rec) {
			t.Errorf("%s %s = %d, want the Setup Guard to redirect to /setup before setup is complete", r.Method, path, rec.Code)
		}
		closed++
	}
	if closed == 0 {
		t.Fatal("found no POST/DELETE /settings/... route besides /settings/:key; the sibling-route check is vacuous")
	}
	if !redirectsToSetup(send(t, engine, http.MethodPost, "/settings/jev-trial")) {
		t.Error("POST /settings/jev-trial is not closed by the Setup Guard")
	}
}
