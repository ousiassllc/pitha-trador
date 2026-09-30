package settings_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/settings"
)

func TestSettingsHandler_Page_ShowsConfiguredStateNotValues(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := newFakeSecretsStore()
	store.values[config.KeyJevAPIKey] = "super-secret-value"
	h := settings.NewSettingsHandler(store)
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

// The banner only guides toward unset optional keys (issue #80): the
// required keys are handled by the Setup Guard redirect, never here.
func TestSettingsHandler_Status_ListsUnsetOptionalKeysOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := newFakeSecretsStore()
	store.values[config.KeyLunaAPIKey] = "luna-key"
	h := settings.NewSettingsHandler(store)
	engine := gin.New()
	engine.GET("/system/secrets-status", h.Status)

	req := httptest.NewRequest(http.MethodGet, "/system/secrets-status", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()
	if !strings.Contains(body, config.KeySlackWebhookURL) {
		t.Fatalf("banner does not list unset optional SLACK_WEBHOOK_URL; body=%s", body)
	}
	if strings.Contains(body, config.KeyLunaAPIKey) {
		t.Fatalf("banner lists LUNA_API_KEY even though it is configured; body=%s", body)
	}
	for _, key := range config.RequiredSecretKeys() {
		if strings.Contains(body, key) {
			t.Fatalf("banner warns about required key %s (unset here); Setup Guard owns that; body=%s", key, body)
		}
	}
}

func TestSettingsHandler_Status_RendersNothingWhenEveryOptionalKeyConfigured(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := newFakeSecretsStore()
	for _, key := range config.OptionalSecretKeys() {
		store.values[key] = "configured"
	}
	h := settings.NewSettingsHandler(store)
	engine := gin.New()
	engine.GET("/system/secrets-status", h.Status)

	req := httptest.NewRequest(http.MethodGet, "/system/secrets-status", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if strings.TrimSpace(rec.Body.String()) != "" {
		t.Fatalf("banner rendered non-empty body once every optional key is configured: %q", rec.Body.String())
	}
}

// issue #70 ("設定画面が開かず、エラーになる"): a single unreadable
// stored value (e.g. corrupted/undecryptable, or a transient "database
// is locked") must not 500 the whole Settings screen - it used to,
// permanently locking the operator out of the one screen that could fix
// it. Page now degrades just that field to "not configured" so the
// screen still renders (and the operator can re-save it).
func TestSettingsHandler_Page_StoreErrorDegradesGracefully(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := newFakeSecretsStore()
	store.values[config.KeyJevAPIKey] = "jev-key"
	store.values[config.KeyJevBaseURL] = "https://jev.example.com"
	store.getErr = errors.New("db is locked")
	store.getErrKey = config.KeyJevBaseURL
	h := settings.NewSettingsHandler(store)
	engine := gin.New()
	engine.GET("/settings", h.Page)

	req := httptest.NewRequest(http.MethodGet, "/settings", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (a broken key must not 500 the whole screen)", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `data-testid="configured-`+config.KeyJevAPIKey+`"`) {
		t.Fatalf("healthy key JEV_API_KEY not shown as configured; body=%s", body)
	}
	if strings.Contains(body, `data-testid="configured-`+config.KeyJevBaseURL+`"`) {
		t.Fatalf("broken key JEV_BASE_URL shown as configured; want it degraded to unset; body=%s", body)
	}
}

// issue #70: the same per-key error must not 500 the header's
// secrets-status banner either (it renders on every page, not just
// Settings) - the broken optional key is reported as unset instead.
func TestSettingsHandler_Status_StoreErrorDegradesGracefully(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := newFakeSecretsStore()
	for _, key := range config.OptionalSecretKeys() {
		store.values[key] = "configured"
	}
	store.getErr = errors.New("db is locked")
	store.getErrKey = config.KeySlackWebhookURL
	h := settings.NewSettingsHandler(store)
	engine := gin.New()
	engine.GET("/system/secrets-status", h.Status)

	req := httptest.NewRequest(http.MethodGet, "/system/secrets-status", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (a broken key must not 500 the banner)", rec.Code, http.StatusOK)
	}
	if !strings.Contains(rec.Body.String(), config.KeySlackWebhookURL) {
		t.Fatalf("banner does not report broken optional key as unset; body=%s", rec.Body.String())
	}
}

// TestSettingsHandler_Page_RendersOneIndependentRowPerAllowedKey covers
// issue #79: every allow-listed key (incl. the Luna/Sol/Opus/News Feed
// ones) gets its own row/form, and the screen has no shared form.
func TestSettingsHandler_Page_RendersOneIndependentRowPerAllowedKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := settingsRouter(settings.NewSettingsHandler(newFakeSecretsStore()))

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/settings", nil))

	body := rec.Body.String()
	for _, key := range config.AllowedSecretKeys() {
		if !strings.Contains(body, `data-testid="secret-field-row-`+key+`"`) {
			t.Errorf("Settings page has no row for %s", key)
		}
		if !strings.Contains(body, `hx-post="/settings/`+key+`"`) {
			t.Errorf("row for %s does not post to its own /settings/%s", key, key)
		}
	}
	if got, want := strings.Count(body, `data-testid="secret-field-row-`), len(config.AllowedSecretKeys()); got != want {
		t.Errorf("row count = %d, want %d", got, want)
	}
	if strings.Contains(body, `action="/settings"`) {
		t.Errorf("Settings page still has a bulk form posting to /settings")
	}
}
