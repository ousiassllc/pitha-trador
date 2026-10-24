package pageflow_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
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
	engine := settingsRouter(h)

	req := httptest.NewRequest(http.MethodGet, "/settings/integrations", nil)
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

// JEV_BASE_URL takes the API host only: the client appends /v1/systemone
// itself, so the screen must show the expected format.
func TestSettingsHandler_Page_ShowsJevBaseURLHint(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := settings.NewSettingsHandler(newFakeSecretsStore())
	engine := settingsRouter(h)

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/settings/integrations", nil))

	body := rec.Body.String()
	if !strings.Contains(body, `data-testid="hint-JEV_BASE_URL"`) || !strings.Contains(body, "https://api.typesafe.ai") {
		t.Fatalf("Page does not show the JEV_BASE_URL hint https://api.typesafe.ai; body=%s", body)
	}
	// issue #274: the model alias default is shown the same way.
	if !strings.Contains(body, `data-testid="hint-JEV_MODEL"`) || !strings.Contains(body, "jev-latest") {
		t.Fatalf("Page does not show the JEV_MODEL hint jev-latest; body=%s", body)
	}
	if strings.Contains(body, `data-testid="hint-JEV_API_KEY"`) {
		t.Fatalf("Page shows a hint for JEV_API_KEY; only keys with a format to explain have one")
	}
}

// Issue #902: a field shows its Japanese label only; the key name lives in
// the field's help mark (「設定キー: …」), which is part of the DOM for screen
// readers.
func TestSettingsHandler_Page_FieldShowsJapaneseLabelAndKeyInHelp(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := settingsRouter(settings.NewSettingsHandler(newFakeSecretsStore()))

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/settings/integrations", nil))

	body := rec.Body.String()
	for _, want := range []string{
		`for="secret-input-JEV_API_KEY"`,
		`API キー`,
		`id="help-secret-JEV_API_KEY"`,
		`aria-describedby="help-secret-JEV_API_KEY"`,
		`設定キー: JEV_API_KEY`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("Page lacks %q; body=%s", want, body)
		}
	}
	if strings.Contains(body, `JEV_API_KEY（`) || strings.Contains(body, `JEV_API_KEY を削除`) {
		t.Errorf("Page shows the raw key name as the field label; body=%s", body)
	}
}

// The unset-optional-keys list only guides toward optional keys (issue #80):
// the required keys are handled by the Setup Guard redirect, never here.
func TestSettingsHandler_UnsetOptionalKeys_ListsUnsetOptionalKeysOnly(t *testing.T) {
	store := newFakeSecretsStore()
	store.values[config.KeyLunaAPIKey] = "luna-key"
	got := settings.NewSettingsHandler(store).UnsetOptionalKeys(context.Background())

	if !slices.Contains(got, config.KeySlackWebhookURL) {
		t.Fatalf("list does not include unset optional SLACK_WEBHOOK_URL: %v", got)
	}
	if slices.Contains(got, config.KeyLunaAPIKey) {
		t.Fatalf("list includes LUNA_API_KEY even though it is configured: %v", got)
	}
	for _, key := range config.RequiredSecretKeys() {
		if slices.Contains(got, key) {
			t.Fatalf("list names required key %s (unset here); Setup Guard owns that: %v", key, got)
		}
	}
}

func TestSettingsHandler_UnsetOptionalKeys_EmptyWhenEveryOptionalKeyConfigured(t *testing.T) {
	store := newFakeSecretsStore()
	for _, key := range config.OptionalSecretKeys() {
		store.values[key] = "configured"
	}
	if got := settings.NewSettingsHandler(store).UnsetOptionalKeys(context.Background()); len(got) != 0 {
		t.Fatalf("list not empty once every optional key is configured: %v", got)
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
	engine := settingsRouter(h)

	req := httptest.NewRequest(http.MethodGet, "/settings/integrations", nil)
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

// issue #70: the same per-key error must not break the unset-optional-keys
// list (the Home screen's 要対応 list and the badge on every page read it) - the
// broken optional key is reported as unset instead.
func TestSettingsHandler_UnsetOptionalKeys_StoreErrorDegradesGracefully(t *testing.T) {
	store := newFakeSecretsStore()
	for _, key := range config.OptionalSecretKeys() {
		store.values[key] = "configured"
	}
	store.getErr = errors.New("db is locked")
	store.getErrKey = config.KeySlackWebhookURL
	got := settings.NewSettingsHandler(store).UnsetOptionalKeys(context.Background())

	if !slices.Contains(got, config.KeySlackWebhookURL) {
		t.Fatalf("list does not report broken optional key as unset: %v", got)
	}
}

// TestSettingsHandler_Page_RendersOneIndependentRowPerAllowedKey covers
// issue #79: every allow-listed key (incl. the Luna/Sol/Opus/News Feed
// ones) gets its own row/form across the Settings pages (issue #805), and
// the screens have no shared form.
func TestSettingsHandler_Page_RendersOneIndependentRowPerAllowedKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := settingsRouter(settings.NewSettingsHandler(newFakeSecretsStore()))

	body := allPagesBody(t, engine)
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
