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
	if plaintext == "" {
		delete(f, key)
		return nil
	}
	f[key] = plaintext
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

func TestNew_SettingsSaveRouteUsesWithSecretsStoreOption(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := fakeSecretsStore{}
	engine := router.New(router.WithSecretsStore(store))

	form := url.Values{"JEV_API_KEY": {"new-jev-key"}}
	req := httptest.NewRequest(http.MethodPost, "/settings", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body=%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	if store["JEV_API_KEY"] != "new-jev-key" {
		t.Fatalf("WithSecretsStore's store was not written by POST /settings: %v", store)
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
