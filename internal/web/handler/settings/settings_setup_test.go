package settings_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/settings"
)

func TestSettingsHandler_SetupPage_ShowsRequiredConnectionsAndOptionalSlack(t *testing.T) {
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
	// issue #302: Setup is the Settings connection list restricted to Jev,
	// kabuステーション and Slack - each as a card opening a modal with the
	// connection's fields, the required ones marked 必須 while unset.
	for _, id := range []string{"jev", "kabu", "slack"} {
		if !strings.Contains(body, `data-testid="settings-card-`+id+`"`) || !strings.Contains(body, `<dialog id="modal-`+id+`"`) {
			t.Errorf("Setup lacks the %s card/modal", id)
		}
	}
	if !strings.Contains(body, `data-testid="connection-status-kabu"`) || !strings.Contains(body, ">必須<") {
		t.Errorf("Setup does not flag the unset required kabu connection as 必須; body=%s", body)
	}
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
		t.Errorf("Setup offers optional %s; Luna/Sol/Opus/News Feed are configured later on Settings", config.KeyLunaAPIKey)
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

// issue #325: Save/Delete from /setup carry the recomputed completion
// message OOB so it follows the required badges; /settings requests don't.
func TestSettingsHandler_SaveAndDelete_FromSetupUpdateCompletionMessage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := newFakeSecretsStore()
	engine := settingsRouter(settings.NewSettingsHandler(store))

	do := func(method, key, value, referer string) string {
		var body *strings.Reader
		if method == http.MethodPost {
			body = strings.NewReader(url.Values{"value": {value}}.Encode())
		} else {
			body = strings.NewReader("")
		}
		req := httptest.NewRequest(method, "/settings/"+key, body)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("HX-Request", "true")
		req.Header.Set("Referer", referer)
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s %s = %d, want 200 (body=%s)", method, key, rec.Code, rec.Body.String())
		}
		return rec.Body.String()
	}
	const oobIncomplete = `id="setup-status"`

	first := do(http.MethodPost, config.KeyJevAPIKey, "jev", "http://localhost/setup")
	if !strings.Contains(first, oobIncomplete) || !strings.Contains(first, `hx-swap-oob="true"`) || !strings.Contains(first, `data-testid="setup-incomplete"`) || strings.Contains(first, `data-testid="setup-complete"`) {
		t.Errorf("first required save from /setup must carry the incomplete message; body=%s", first)
	}

	second := do(http.MethodPost, config.KeyKabuAPIPassword, "kabu", "http://localhost/setup")
	if !strings.Contains(second, oobIncomplete) || !strings.Contains(second, `data-testid="setup-complete"`) || strings.Contains(second, `data-testid="setup-incomplete"`) {
		t.Errorf("second required save from /setup must carry the complete message; body=%s", second)
	}

	deleted := do(http.MethodDelete, config.KeyKabuAPIPassword, "", "http://localhost/setup")
	if !strings.Contains(deleted, `data-testid="setup-incomplete"`) || strings.Contains(deleted, `data-testid="setup-complete"`) {
		t.Errorf("deleting a required key from /setup must revert to the incomplete message; body=%s", deleted)
	}

	fromSettings := do(http.MethodPost, config.KeyKabuAPIPassword, "kabu", "http://localhost/settings")
	if strings.Contains(fromSettings, oobIncomplete) {
		t.Errorf("a /settings save must not carry the Setup completion message; body=%s", fromSettings)
	}
}
