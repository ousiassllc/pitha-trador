package settings_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/settings"
)

func TestSettingsHandler_SetupPage_ShowsRequiredFieldsAndOptionalSlack(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := newFakeSecretsStore()
	store.values[config.KeyJevAPIKey] = "super-secret-value"
	h := settings.NewSettingsHandler(store)
	engine := gin.New()
	engine.GET("/setup", h.SetupPage)

	req := httptest.NewRequest(http.MethodGet, "/setup", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body=%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	body := rec.Body.String()
	for _, key := range append(config.RequiredSecretKeys(), config.KeySlackWebhookURL) {
		if !strings.Contains(body, `data-testid="secret-field-row-`+key+`"`) {
			t.Errorf("Setup lacks a field row for %s", key)
		}
		if !strings.Contains(body, `data-testid="save-`+key+`"`) {
			t.Errorf("Setup lacks a save button for %s", key)
		}
		// Same forms as Settings: they post to the shared per-key route.
		if !strings.Contains(body, `hx-post="/settings/`+key+`"`) {
			t.Errorf("Setup row for %s does not post to /settings/%s", key, key)
		}
	}
	if strings.Contains(body, `data-testid="secret-field-row-`+config.KeyLunaAPIKey+`"`) {
		t.Errorf("Setup offers optional %s; only SLACK_WEBHOOK_URL belongs on it", config.KeyLunaAPIKey)
	}
	if strings.Contains(body, "super-secret-value") {
		t.Fatalf("Setup rendered the stored plaintext value; body=%s", body)
	}
	if !strings.Contains(body, `data-testid="configured-`+config.KeyJevAPIKey+`"`) {
		t.Errorf("Setup does not mark stored JEV_API_KEY as configured")
	}
	if !strings.Contains(body, `data-testid="setup-incomplete"`) || strings.Contains(body, `data-testid="setup-complete"`) {
		t.Errorf("Setup with unset required keys must show the incomplete hint only; body=%s", body)
	}
	if strings.Contains(body, `id="config-banner"`) {
		t.Errorf("Setup renders the global Header; its guarded hx-get fragments would redirect back to /setup")
	}
}

func TestSettingsHandler_SetupPage_ReportsCompleteOnceRequiredKeysAreSet(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := newFakeSecretsStore()
	for _, key := range config.RequiredSecretKeys() {
		store.values[key] = "configured"
	}
	h := settings.NewSettingsHandler(store)
	engine := gin.New()
	engine.GET("/setup", h.SetupPage)

	req := httptest.NewRequest(http.MethodGet, "/setup", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, `data-testid="setup-complete"`) {
		t.Fatalf("Setup after configuring every required key = %d, want 200 with the complete notice; body=%s", rec.Code, body)
	}
}

func TestStaticSecretsStore_EverythingUnsetAndWritesAreNoOps(t *testing.T) {
	store := settings.StaticSecretsStore{}
	value, ok, err := store.Get(context.Background(), config.KeyJevAPIKey)
	if err != nil || ok || value != "" {
		t.Fatalf("Get = (%q, %v, %v), want (\"\", false, nil)", value, ok, err)
	}
	if err := store.Set(context.Background(), config.KeyJevAPIKey, "anything"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := store.Delete(context.Background(), config.KeyJevAPIKey); err != nil {
		t.Fatalf("Delete: %v", err)
	}
}
