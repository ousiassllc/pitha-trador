package router_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/router"
	"github.com/ousiassllc/pitha-trador/internal/service/updater"
)

// fakeSecretsStore is a minimal handler.SecretsStore for router-wiring
// tests (router_test cannot reuse internal/web/handler's own unexported
// test fake across packages, mirroring fakeSymbolProvider above).
type fakeSecretsStore map[string]string

func (f fakeSecretsStore) Get(_ context.Context, key string) (string, bool, error) {
	value, ok := f[key]
	return value, ok, nil
}

func (f fakeSecretsStore) Set(_ context.Context, key, plaintext string) error {
	f[key] = plaintext
	return nil
}

func (f fakeSecretsStore) Delete(_ context.Context, key string) error {
	delete(f, key)
	return nil
}

// Without WithSecretsStore (router-level tests) no Setup Guard is
// installed, so the Settings page renders even though every key is unset.
func TestNew_SettingsPageRouteIsRegistered(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := router.New()

	req := httptest.NewRequest(http.MethodGet, "/settings", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body=%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "JEV_API_KEY") {
		t.Fatalf("Settings page does not render the JEV_API_KEY field; body=%s", rec.Body.String())
	}
}

func TestNew_SettingsPerKeyRoutesUseWithSecretsStoreOption(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := fakeSecretsStore{"KABU_API_PASSWORD": "kabu"}
	engine := router.New(router.WithSecretsStore(store))

	form := url.Values{"value": {"new-jev-key"}}
	req := authorize(t, engine, httptest.NewRequest(http.MethodPost, "/settings/JEV_API_KEY", strings.NewReader(form.Encode())))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("POST status = %d, want %d (body=%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	if store["JEV_API_KEY"] != "new-jev-key" || store["KABU_API_PASSWORD"] != "kabu" {
		t.Fatalf("POST /settings/JEV_API_KEY did not write only that key: %v", store)
	}

	rec = httptest.NewRecorder()
	engine.ServeHTTP(rec, hxDelete(t, engine, "/settings/JEV_API_KEY"))
	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE status = %d, want %d (body=%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	if _, ok := store["JEV_API_KEY"]; ok || store["KABU_API_PASSWORD"] != "kabu" {
		t.Fatalf("DELETE /settings/JEV_API_KEY did not remove only that key: %v", store)
	}

	rec = httptest.NewRecorder()
	engine.ServeHTTP(rec, authorize(t, engine, httptest.NewRequest(http.MethodDelete, "/settings/NOT_A_KEY", nil)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("DELETE unknown key status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestNew_BulkSettingsPostIsGone(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := requiredSecretsStore()
	engine := router.New(router.WithSecretsStore(store))

	req := authorize(t, engine, httptest.NewRequest(http.MethodPost, "/settings", strings.NewReader("JEV_API_KEY=x")))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound || store["JEV_API_KEY"] != "jev-key" {
		t.Fatalf("POST /settings = %d with store %v, want 404 and no writes", rec.Code, store)
	}
}

func TestNew_SecretsStatusRouteReflectsWithSecretsStoreOption(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := requiredSecretsStore()
	engine := router.New(router.WithSecretsStore(store))

	get := func() string {
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/system/secrets-status", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
		}
		return rec.Body.String()
	}
	if body := get(); !strings.Contains(body, "SLACK_WEBHOOK_URL") {
		t.Fatalf("secrets-status banner does not list unset optional SLACK_WEBHOOK_URL: %q", body)
	}
	for _, key := range config.OptionalSecretKeys() {
		store[key] = "configured"
	}
	if body := get(); strings.TrimSpace(body) != "" {
		t.Fatalf("secrets-status banner non-empty though every key is configured: %q", body)
	}
}

// requiredSecretsStore returns a store with every required key (and no
// optional one) configured, i.e. one the Setup Guard lets through.
func requiredSecretsStore() fakeSecretsStore {
	return fakeSecretsStore{
		"JEV_API_KEY":       "jev-key",
		"JEV_BASE_URL":      "https://jev.example.com",
		"KABU_API_PASSWORD": "kabu-pass",
	}
}

// issue #80 (FR-SETUP-1): while any required key is unset, every route
// except `/setup`, `POST`/`DELETE /settings/:key` and `/static/...`
// redirects to `/setup`.
func TestNew_SetupGuardRedirectsEveryGuardedRouteWhileRequiredKeyIsUnset(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := requiredSecretsStore()
	delete(store, "KABU_API_PASSWORD")
	engine := router.New(router.WithSecretsStore(store))

	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/"},
		{http.MethodGet, "/scanner"},
		{http.MethodGet, "/settings"},
		{http.MethodGet, "/system/secrets-status"},
		{http.MethodGet, "/api/v1/scanner"},
		{http.MethodPost, "/api/v1/system/kill"},
		{http.MethodPost, "/settings"},
		{http.MethodGet, "/no-such-route"},
	} {
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, authorize(t, engine, httptest.NewRequest(tc.method, tc.path, nil)))
		if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/setup" {
			t.Errorf("%s %s = %d Location=%q, want 302 to /setup", tc.method, tc.path, rec.Code, rec.Header().Get("Location"))
		}
	}
}

func TestNew_SetupGuardLetsSetupSettingsWritesAndStaticThrough(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := fakeSecretsStore{}
	engine := router.New(router.WithSecretsStore(store))

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/setup", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /setup = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}
	for _, key := range []string{"JEV_API_KEY", "JEV_BASE_URL", "KABU_API_PASSWORD", "SLACK_WEBHOOK_URL"} {
		if !strings.Contains(rec.Body.String(), `data-testid="secret-field-row-`+key+`"`) {
			t.Errorf("GET /setup lacks the %s field", key)
		}
	}

	rec = httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/static/vendor/htmx.min.js", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /static/vendor/htmx.min.js = %d, want 200", rec.Code)
	}

	form := url.Values{"value": {"jev-key"}}
	req := authorize(t, engine, httptest.NewRequest(http.MethodPost, "/settings/JEV_API_KEY", strings.NewReader(form.Encode())))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	rec = httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || store["JEV_API_KEY"] != "jev-key" {
		t.Fatalf("POST /settings/JEV_API_KEY = %d store=%v, want 200 and the key stored", rec.Code, store)
	}

	rec = httptest.NewRecorder()
	engine.ServeHTTP(rec, hxDelete(t, engine, "/settings/JEV_API_KEY"))
	if rec.Code != http.StatusOK || len(store) != 0 {
		t.Fatalf("DELETE /settings/JEV_API_KEY = %d store=%v, want 200 and the key removed", rec.Code, store)
	}
}

// The guard reads the store on every request, so saving the last required
// key through the Setup screen's own route lifts the redirect from the
// next request on, and `/setup` stays reachable afterwards.
func TestNew_SetupGuardLiftsOnceLastRequiredKeyIsSaved(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := requiredSecretsStore()
	delete(store, "JEV_BASE_URL")
	engine := router.New(router.WithSecretsStore(store))

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/scanner", nil))
	if rec.Code != http.StatusFound {
		t.Fatalf("GET /scanner before setup = %d, want 302", rec.Code)
	}

	form := url.Values{"value": {"https://jev.example.com"}}
	req := authorize(t, engine, httptest.NewRequest(http.MethodPost, "/settings/JEV_BASE_URL", strings.NewReader(form.Encode())))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	rec = httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /settings/JEV_BASE_URL = %d, want 200", rec.Code)
	}

	for _, path := range []string{"/scanner", "/settings", "/setup"} {
		rec = httptest.NewRecorder()
		engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s after setup = %d, want 200", path, rec.Code)
		}
	}
	rec = httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/setup", nil))
	if !strings.Contains(rec.Body.String(), `data-testid="setup-complete"`) {
		t.Errorf("GET /setup after setup does not report completion")
	}
}

type fakeUpdateController struct{ checks int }

func (f *fakeUpdateController) Status() updater.Status {
	return updater.Status{Available: true, Version: "v9.9.9", Blocked: true}
}

func (f *fakeUpdateController) CheckForUpdate(context.Context) error {
	f.checks++
	return nil
}

func TestNew_UpdateRoutesAreRegistered(t *testing.T) {
	gin.SetMode(gin.TestMode)
	controller := &fakeUpdateController{}
	engine := router.New(router.WithUpdateController(controller))

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/system/update-status", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "v9.9.9") {
		t.Fatalf("GET /system/update-status = %d %q, want 200 with the available version", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	engine.ServeHTTP(rec, authorize(t, engine, httptest.NewRequest(http.MethodPost, "/system/update-check", nil)))
	if rec.Code != http.StatusOK || controller.checks != 1 {
		t.Fatalf("POST /system/update-check = %d with %d checks, want 200 with 1", rec.Code, controller.checks)
	}
}

func TestNew_UpdateCheckRouteIs404WithoutUpdater(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := router.New()
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, authorize(t, engine, httptest.NewRequest(http.MethodPost, "/system/update-check", nil)))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (cmd/server has no updater)", rec.Code)
	}
}

func TestNew_SettingsAndHeaderEmbedUpdateSlots(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	router.New().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/settings", nil))
	body := rec.Body.String()
	for _, want := range []string{`id="update-banner"`, `id="update-panel"`} {
		if !strings.Contains(body, want) {
			t.Errorf("Settings page missing %s; body=%s", want, body)
		}
	}
}

// hxDelete builds an authorized `DELETE path` the way htmx sends it.
func hxDelete(t *testing.T, engine *gin.Engine, path string) *http.Request {
	t.Helper()
	req := authorize(t, engine, httptest.NewRequest(http.MethodDelete, path, nil))
	req.Header.Set("HX-Request", "true")
	return req
}
