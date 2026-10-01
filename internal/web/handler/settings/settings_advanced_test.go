package settings_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/settings"
)

// advancedSection returns the Settings page's 「詳細設定（任意）」 <details>
// element (opening tag included) and the markup before it.
func advancedSection(t *testing.T, body string) (before, section string) {
	t.Helper()
	start := strings.Index(body, `<details`)
	end := strings.Index(body, `</details>`)
	if start < 0 || end < start {
		t.Fatalf("Settings page has no <details> 詳細設定 section; body=%s", body)
	}
	return body[:start], body[start : end+len(`</details>`)]
}

// issue #272: the override keys (URLs, model, update token) live in a
// collapsed 詳細設定 section; only the credentials stay up front.
func TestSettingsHandler_Page_GroupsOverridesIntoCollapsedAdvancedSection(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := settingsRouter(settings.NewSettingsHandler(newFakeSecretsStore()))

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/settings", nil))
	before, section := advancedSection(t, rec.Body.String())

	if !strings.Contains(section, "詳細設定（任意）") {
		t.Errorf("advanced section lacks its 詳細設定（任意） summary: %s", section)
	}
	if strings.Contains(section[:strings.Index(section, ">")], " open") {
		t.Errorf("advanced section is open although no override is stored: %s", section[:strings.Index(section, ">")])
	}
	for _, key := range []string{
		config.KeyJevBaseURL, config.KeyJevModel, config.KeyLunaBaseURL, config.KeyNewsFeedURL,
		config.KeySolBaseURL, config.KeyOpusBaseURL, config.KeyUpdateGitHubToken,
	} {
		row := `data-testid="secret-field-row-` + key + `"`
		if !strings.Contains(section, row) || strings.Contains(before, row) {
			t.Errorf("%s is not (only) inside the 詳細設定 section", key)
		}
	}
	for _, key := range []string{
		config.KeyJevAPIKey, config.KeyKabuAPIPassword, config.KeySlackWebhookURL,
		config.KeyLunaAPIKey, config.KeyNewsFeedAPIKey, config.KeySolAPIKey, config.KeyOpusAPIKey,
	} {
		row := `data-testid="secret-field-row-` + key + `"`
		if !strings.Contains(before, row) || strings.Contains(section, row) {
			t.Errorf("%s is not shown outside the 詳細設定 section", key)
		}
	}
	if got := strings.Count(before, `data-testid="secret-field-row-`); got >= len(config.AllowedSecretKeys())/2+1 {
		t.Errorf("%d rows visible up front, want clearly fewer than the %d keys", got, len(config.AllowedSecretKeys()))
	}
}

// A stored override keeps the section open so it is not hidden, and its own
// delete button (the existing DELETE /settings/:key) reverts it to default.
func TestSettingsHandler_Page_AdvancedSectionOpensForStoredOverrideAndDeleteRevertsToDefault(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := newFakeSecretsStore()
	store.values[config.KeyJevBaseURL] = "https://jev.example.com"
	engine := settingsRouter(settings.NewSettingsHandler(store))

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/settings", nil))
	_, section := advancedSection(t, rec.Body.String())
	if open := section[:strings.Index(section, ">")]; !strings.Contains(open, " open") {
		t.Errorf("advanced section is collapsed although JEV_BASE_URL is stored: %s", open)
	}
	if !strings.Contains(section, `data-testid="delete-`+config.KeyJevBaseURL+`"`) {
		t.Errorf("stored JEV_BASE_URL has no delete button; section=%s", section)
	}

	rec = deleteSetting(engine, config.KeyJevBaseURL)
	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE status = %d, want %d", rec.Code, http.StatusOK)
	}
	if _, ok := store.values[config.KeyJevBaseURL]; ok {
		t.Errorf("JEV_BASE_URL still stored after DELETE")
	}
}

// issues #271/#274: JEV_BASE_URL and JEV_MODEL default when unset, so the
// optional-keys banner must not nag about them.
func TestSettingsHandler_Status_DoesNotNagAboutDefaultedKeys(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := settings.NewSettingsHandler(newFakeSecretsStore())
	engine := gin.New()
	engine.GET("/system/secrets-status", h.Status)

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/system/secrets-status", nil))

	body := rec.Body.String()
	if !strings.Contains(body, config.KeySlackWebhookURL) {
		t.Fatalf("banner does not list unset SLACK_WEBHOOK_URL; body=%s", body)
	}
	for _, key := range []string{config.KeyJevBaseURL, config.KeyJevModel} {
		if strings.Contains(body, key) {
			t.Errorf("banner lists defaulted key %s; body=%s", key, body)
		}
	}
}
