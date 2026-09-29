package router_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

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
	req := httptest.NewRequest(http.MethodPost, "/settings/JEV_API_KEY", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("POST status = %d, want %d (body=%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	if store["JEV_API_KEY"] != "new-jev-key" || store["KABU_API_PASSWORD"] != "kabu" {
		t.Fatalf("POST /settings/JEV_API_KEY did not write only that key: %v", store)
	}

	rec = httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/settings/JEV_API_KEY", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE status = %d, want %d (body=%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	if _, ok := store["JEV_API_KEY"]; ok || store["KABU_API_PASSWORD"] != "kabu" {
		t.Fatalf("DELETE /settings/JEV_API_KEY did not remove only that key: %v", store)
	}

	rec = httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/settings/NOT_A_KEY", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("DELETE unknown key status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestNew_BulkSettingsPostIsGone(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := fakeSecretsStore{}
	engine := router.New(router.WithSecretsStore(store))

	req := httptest.NewRequest(http.MethodPost, "/settings", strings.NewReader("JEV_API_KEY=x"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound || len(store) != 0 {
		t.Fatalf("POST /settings = %d with store %v, want 404 and no writes", rec.Code, store)
	}
}

func TestNew_SecretsStatusRouteReflectsWithSecretsStoreOption(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := fakeSecretsStore{
		"JEV_API_KEY":       "jev-key",
		"JEV_BASE_URL":      "https://jev.example.com",
		"KABU_API_PASSWORD": "kabu-pass",
	}
	engine := router.New(router.WithSecretsStore(store))

	req := httptest.NewRequest(http.MethodGet, "/system/secrets-status", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if strings.TrimSpace(rec.Body.String()) != "" {
		t.Fatalf("secrets-status banner non-empty though every required key is configured: %q", rec.Body.String())
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
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/system/update-check", nil))
	if rec.Code != http.StatusOK || controller.checks != 1 {
		t.Fatalf("POST /system/update-check = %d with %d checks, want 200 with 1", rec.Code, controller.checks)
	}
}

func TestNew_UpdateCheckRouteIs404WithoutUpdater(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	router.New().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/system/update-check", nil))
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
