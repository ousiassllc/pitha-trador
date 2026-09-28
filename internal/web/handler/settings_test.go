package handler_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/web/handler"
)

// fakeSecretsStore is a configurable handler.SecretsStore for tests,
// backed by an in-memory map instead of a real
// internal/repository.SecretsRepository (which needs a *sql.DB).
type fakeSecretsStore struct {
	values map[string]string
	getErr error
	setErr error
}

func newFakeSecretsStore() *fakeSecretsStore {
	return &fakeSecretsStore{values: map[string]string{}}
}

func (f *fakeSecretsStore) Get(_ context.Context, key string) (string, bool, error) {
	if f.getErr != nil {
		return "", false, f.getErr
	}
	value, ok := f.values[key]
	return value, ok, nil
}

func (f *fakeSecretsStore) Set(_ context.Context, key, plaintext string) error {
	if f.setErr != nil {
		return f.setErr
	}
	if plaintext == "" {
		delete(f.values, key)
		return nil
	}
	f.values[key] = plaintext
	return nil
}

func TestSettingsHandler_Page_ShowsConfiguredStateNotValues(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := newFakeSecretsStore()
	store.values[config.KeyJevAPIKey] = "super-secret-value"
	h := handler.NewSettingsHandler(store)
	engine := gin.New()
	engine.GET("/settings", h.Page)

	req := httptest.NewRequest(http.MethodGet, "/settings", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body=%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, "super-secret-value") {
		t.Fatalf("Page rendered the stored plaintext value; body=%s", body)
	}
	if !strings.Contains(body, `data-testid="configured-JEV_API_KEY"`) {
		t.Fatalf("Page does not mark JEV_API_KEY as configured; body=%s", body)
	}
	if strings.Contains(body, `data-testid="configured-JEV_BASE_URL"`) {
		t.Fatalf("Page marks unset JEV_BASE_URL as configured; body=%s", body)
	}
}

func TestSettingsHandler_Save_StoresSubmittedValuesAndShowsSavedNotice(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := newFakeSecretsStore()
	h := handler.NewSettingsHandler(store)
	engine := gin.New()
	engine.POST("/settings", h.Save)

	form := url.Values{
		config.KeyJevAPIKey:       {"new-jev-key"},
		config.KeyJevBaseURL:      {"https://jev.example.com"},
		config.KeyKabuAPIPassword: {"kabu-pass"},
	}
	req := httptest.NewRequest(http.MethodPost, "/settings", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body=%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `data-testid="settings-saved"`) {
		t.Fatalf("Save response does not show the saved notice; body=%s", rec.Body.String())
	}
	if store.values[config.KeyJevAPIKey] != "new-jev-key" {
		t.Errorf("stored JEV_API_KEY = %q, want %q", store.values[config.KeyJevAPIKey], "new-jev-key")
	}
	if store.values[config.KeyKabuAPIPassword] != "kabu-pass" {
		t.Errorf("stored KABU_API_PASSWORD = %q, want %q", store.values[config.KeyKabuAPIPassword], "kabu-pass")
	}
}

func TestSettingsHandler_Save_EmptyFieldClearsExistingValue(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := newFakeSecretsStore()
	store.values[config.KeyJevAPIKey] = "old-value"
	h := handler.NewSettingsHandler(store)
	engine := gin.New()
	engine.POST("/settings", h.Save)

	form := url.Values{config.KeyJevAPIKey: {""}}
	req := httptest.NewRequest(http.MethodPost, "/settings", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if _, ok := store.values[config.KeyJevAPIKey]; ok {
		t.Fatalf("JEV_API_KEY still stored after submitting it blank: %q", store.values[config.KeyJevAPIKey])
	}
}

func TestSettingsHandler_Status_ListsMissingRequiredKeysOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := newFakeSecretsStore()
	store.values[config.KeyJevAPIKey] = "jev-key"
	h := handler.NewSettingsHandler(store)
	engine := gin.New()
	engine.GET("/system/secrets-status", h.Status)

	req := httptest.NewRequest(http.MethodGet, "/system/secrets-status", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()
	if !strings.Contains(body, config.KeyJevBaseURL) || !strings.Contains(body, config.KeyKabuAPIPassword) {
		t.Fatalf("banner does not list missing required keys; body=%s", body)
	}
	if strings.Contains(body, config.KeyJevAPIKey) {
		t.Fatalf("banner lists JEV_API_KEY even though it is configured; body=%s", body)
	}
	if strings.Contains(body, config.KeySlackWebhookURL) {
		t.Fatalf("banner lists optional SLACK_WEBHOOK_URL; body=%s", body)
	}
}

func TestSettingsHandler_Status_RendersNothingWhenEverythingConfigured(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := newFakeSecretsStore()
	store.values[config.KeyJevAPIKey] = "jev-key"
	store.values[config.KeyJevBaseURL] = "https://jev.example.com"
	store.values[config.KeyKabuAPIPassword] = "kabu-pass"
	h := handler.NewSettingsHandler(store)
	engine := gin.New()
	engine.GET("/system/secrets-status", h.Status)

	req := httptest.NewRequest(http.MethodGet, "/system/secrets-status", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if strings.TrimSpace(rec.Body.String()) != "" {
		t.Fatalf("banner rendered non-empty body once every required key is configured: %q", rec.Body.String())
	}
}

func TestSettingsHandler_Page_StoreErrorReturns500(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := newFakeSecretsStore()
	store.getErr = errors.New("db is locked")
	h := handler.NewSettingsHandler(store)
	engine := gin.New()
	engine.GET("/settings", h.Page)

	req := httptest.NewRequest(http.MethodGet, "/settings", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}

func TestStaticSecretsStore_EverythingUnsetAndSetIsNoOp(t *testing.T) {
	store := handler.StaticSecretsStore{}
	value, ok, err := store.Get(context.Background(), config.KeyJevAPIKey)
	if err != nil || ok || value != "" {
		t.Fatalf("Get = (%q, %v, %v), want (\"\", false, nil)", value, ok, err)
	}
	if err := store.Set(context.Background(), config.KeyJevAPIKey, "anything"); err != nil {
		t.Fatalf("Set: %v", err)
	}
}
