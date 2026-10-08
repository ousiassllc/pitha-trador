package middleware_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/web/middleware"
)

type stubSecrets struct {
	values map[string]string
	err    error
}

func (s stubSecrets) Get(_ context.Context, key string) (string, bool, error) {
	if s.err != nil {
		return "", false, s.err
	}
	v, ok := s.values[key]
	return v, ok, nil
}

func serve(t *testing.T, store middleware.SecretsReader, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(middleware.SetupGuard(store, middleware.StaticSetupRequirements("A", "B")))
	engine.NoRoute(func(c *gin.Context) { c.String(http.StatusOK, "reached") })
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec
}

func TestSetupGuard_Exemptions(t *testing.T) {
	unset := stubSecrets{values: map[string]string{"A": "x"}}
	for _, tc := range []struct {
		method, path string
		redirected   bool
	}{
		{http.MethodGet, "/setup", false},
		{http.MethodGet, "/static/dist/css/app.css", false},
		{http.MethodPost, "/settings/A", false},
		{http.MethodDelete, "/settings/A", false},
		{http.MethodGet, "/settings/A", true},
		// The broker selection and 立花 settings rows are part of setup (issue #734).
		{http.MethodPost, "/ops-settings/broker.provider", false},
		{http.MethodDelete, "/ops-settings/broker.tachibana.demo.private_key_path", false},
		{http.MethodPost, "/ops-settings/policy.long.min_probability", true},
		{http.MethodPost, "/ops-settings/system.backup_dir", true},
		{http.MethodGet, "/ops-settings/broker.provider", true},
		{http.MethodGet, "/settings", true},
		{http.MethodGet, "/staticfoo", true},
		{http.MethodGet, "/scanner", true},
	} {
		rec := serve(t, unset, tc.method, tc.path)
		if got := rec.Code == http.StatusFound && rec.Header().Get("Location") == "/setup"; got != tc.redirected {
			t.Errorf("%s %s redirected=%v (code %d), want %v", tc.method, tc.path, got, rec.Code, tc.redirected)
		}
	}
}

// Script-driven requests must not be handed the /setup HTML page through a
// followed 302 (issue #140): HTMX gets HX-Redirect, /api/v1 gets JSON, a
// WebSocket upgrade gets 403.
func TestSetupGuard_NonNavigationRequestsGetProtocolAppropriateResponses(t *testing.T) {
	unset := stubSecrets{values: map[string]string{"A": "x"}}
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(middleware.SetupGuard(unset, middleware.StaticSetupRequirements("A", "B")))

	do := func(method, path string, headers map[string]string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)
		return rec
	}

	hx := do(http.MethodGet, "/system/status", map[string]string{"HX-Request": "true"})
	if hx.Code != http.StatusNoContent || hx.Header().Get("HX-Redirect") != "/setup" || hx.Header().Get("Location") != "" || hx.Body.Len() != 0 {
		t.Errorf("HX-Request = %d HX-Redirect=%q Location=%q body=%q, want 204 + HX-Redirect only", hx.Code, hx.Header().Get("HX-Redirect"), hx.Header().Get("Location"), hx.Body.String())
	}

	for _, path := range []string{"/api/v1/scanner", "/api/v1"} {
		api := do(http.MethodGet, path, nil)
		if api.Code != http.StatusServiceUnavailable || !strings.HasPrefix(api.Header().Get("Content-Type"), "application/json") {
			t.Fatalf("GET %s = %d %q, want 503 JSON", path, api.Code, api.Header().Get("Content-Type"))
		}
		var body map[string]any
		if err := json.Unmarshal(api.Body.Bytes(), &body); err != nil || body["setup_required"] != true || body["setup_url"] != "/setup" {
			t.Errorf("GET %s body = %q (err %v), want setup_required JSON", path, api.Body.String(), err)
		}
	}

	ws := do(http.MethodGet, "/ws/system", map[string]string{"Upgrade": "websocket", "Connection": "Upgrade"})
	if ws.Code != http.StatusForbidden || ws.Header().Get("Location") != "" {
		t.Errorf("WebSocket upgrade = %d Location=%q, want 403", ws.Code, ws.Header().Get("Location"))
	}

	page := do(http.MethodGet, "/scanner", nil)
	if page.Code != http.StatusFound || page.Header().Get("Location") != "/setup" {
		t.Errorf("page navigation = %d Location=%q, want 302 to /setup", page.Code, page.Header().Get("Location"))
	}
}

func TestSetupGuard_PassesThroughWhenAllRequiredKeysSet(t *testing.T) {
	rec := serve(t, stubSecrets{values: map[string]string{"A": "x", "B": "y"}}, http.MethodGet, "/scanner")
	if rec.Code != http.StatusOK || rec.Body.String() != "reached" {
		t.Fatalf("GET /scanner = %d %q, want handler reached", rec.Code, rec.Body.String())
	}
}

// A store error must not let requests through as if setup were done.
func TestSetupGuard_StoreErrorCountsAsUnset(t *testing.T) {
	rec := serve(t, stubSecrets{err: errors.New("db is locked")}, http.MethodGet, "/scanner")
	if rec.Code != http.StatusFound {
		t.Fatalf("GET /scanner with unreadable store = %d, want 302", rec.Code)
	}
}

// issue #734: what the guard requires follows the selected broker/environment
// per request, and an unset 秘密鍵 path keeps it closed even with every secret.
func TestSetupGuard_RequirementsFollowBrokerAndEnvironment(t *testing.T) {
	gin.SetMode(gin.TestMode)
	secrets := stubSecrets{values: map[string]string{config.KeyJevAPIKey: "j", config.KeyTachibanaDemoAuthID: "d"}}
	serveWith := func(b config.BrokerSettings) *httptest.ResponseRecorder {
		engine := gin.New()
		engine.Use(middleware.SetupGuard(secrets, func(context.Context) config.SetupRequirements { return config.RequiredSetup(b) }))
		engine.NoRoute(func(c *gin.Context) { c.String(http.StatusOK, "reached") })
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/scanner", nil))
		return rec
	}
	demo := config.TachibanaSettings{Environment: config.TachibanaEnvDemo, DemoPrivateKeyPath: "/keys/demo.pem"}
	tests := []struct {
		name    string
		broker  config.BrokerSettings
		through bool
	}{
		{"kabu needs KABU_API_PASSWORD", config.BrokerSettings{Provider: config.BrokerKabu}, false},
		{"tachibana demo with ID and key path, no KABU_API_PASSWORD", config.BrokerSettings{Provider: config.BrokerTachibana, Tachibana: demo}, true},
		{"tachibana demo without key path", config.BrokerSettings{Provider: config.BrokerTachibana, Tachibana: config.TachibanaSettings{Environment: config.TachibanaEnvDemo}}, false},
		{"tachibana production needs the production ID", config.BrokerSettings{Provider: config.BrokerTachibana, Tachibana: config.TachibanaSettings{Environment: config.TachibanaEnvProduction, ProdPrivateKeyPath: "/keys/prod.pem"}}, false},
	}
	for _, tc := range tests {
		rec := serveWith(tc.broker)
		if got := rec.Code == http.StatusOK; got != tc.through {
			t.Errorf("%s: through = %v (code %d), want %v", tc.name, got, rec.Code, tc.through)
		}
	}
}
