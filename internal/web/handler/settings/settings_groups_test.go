package settings_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/settings"
)

var rowKeyPattern = regexp.MustCompile(`data-testid="secret-field-row-([A-Z_]+)"`)

// rowKeys returns the sorted keys of every secret-field row in markup.
func rowKeys(markup string) []string {
	var keys []string
	for _, m := range rowKeyPattern.FindAllStringSubmatch(markup, -1) {
		keys = append(keys, m[1])
	}
	slices.Sort(keys)
	return keys
}

// modalSection returns the <dialog id="modal-<id>"> element of body.
func modalSection(t *testing.T, body, id string) string {
	t.Helper()
	start := strings.Index(body, `<dialog id="modal-`+id+`"`)
	if start < 0 {
		t.Fatalf("page has no modal-%s dialog; body=%s", id, body)
	}
	end := strings.Index(body[start:], `</dialog>`)
	if end < 0 {
		t.Fatalf("modal-%s dialog is not closed", id)
	}
	return body[start : start+end]
}

// issue #302: each connection's key, URL and model name live together in
// that connection's modal. The grouping is pinned exactly so that adding or
// moving a key is a deliberate test change; together the modals cover the
// allow-list exactly once. Luna/Sol/Opus/News Feed are optional overrides of
// their defaults (Jev / やのしん, issue #273).
func TestSettingsHandler_Page_GroupsKeysIntoOneModalPerConnection(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := settingsRouter(settings.NewSettingsHandler(newFakeSecretsStore()))

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/settings", nil))
	body := rec.Body.String()

	want := map[string][]string{
		"jev":       {config.KeyJevAPIKey, config.KeyJevBaseURL, config.KeyJevModel},
		"kabu":      {config.KeyKabuAPIPassword},
		"slack":     {config.KeySlackWebhookURL},
		"luna":      {config.KeyLunaAPIKey, config.KeyLunaBaseURL},
		"news-feed": {config.KeyNewsFeedAPIKey, config.KeyNewsFeedURL, config.KeyNewsFeedEnabled},
		"sol":       {config.KeySolAPIKey, config.KeySolBaseURL},
		"opus":      {config.KeyOpusAPIKey, config.KeyOpusBaseURL},
	}
	var all []string
	for id, keys := range want {
		slices.Sort(keys)
		if got := rowKeys(modalSection(t, body, id)); !slices.Equal(got, keys) {
			t.Errorf("modal-%s keys = %v, want %v", id, got, keys)
		}
		all = append(all, keys...)
		if !strings.Contains(body, `data-testid="settings-card-`+id+`"`) {
			t.Errorf("no list card for connection %s", id)
		}
	}
	slices.Sort(all)
	allowed := config.AllowedSecretKeys()
	slices.Sort(allowed)
	if !slices.Equal(all, allowed) {
		t.Errorf("modal keys = %v, want AllowedSecretKeys %v", all, allowed)
	}
}

// The list shows each connection's state from its main key: unset by
// default, configured once the key is stored, partial when only an override
// is stored. Luna/Sol/Opus/News Feed work through a default (issue #273), so
// with nothing stored they show "default", not "unset".
func TestSettingsHandler_Page_CardStateReflectsStoredKeys(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := newFakeSecretsStore()
	store.values[config.KeyJevAPIKey] = "jev-key"
	store.values[config.KeyLunaBaseURL] = "https://luna.example.com"
	engine := settingsRouter(settings.NewSettingsHandler(store))

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/settings", nil))
	body := rec.Body.String()

	for id, state := range map[string]string{"jev": "configured", "luna": "partial", "kabu": "unset", "slack": "unset", "sol": "default", "opus": "default", "news-feed": "default"} {
		if want := `data-testid="connection-status-` + id + `"`; !strings.Contains(body, want) {
			t.Fatalf("no status for %s", id)
		}
		re := regexp.MustCompile(`id="connection-status-` + id + `"[^>]*data-state="([a-z]+)"`)
		if m := re.FindStringSubmatch(body); m == nil || m[1] != state {
			t.Errorf("connection %s state = %v, want %s", id, m, state)
		}
	}
}

// Saving/deleting one field answers with the row plus an out-of-band status
// for its connection, so the list behind the modal updates (issue #302) -
// and never touches the connection's other fields.
func TestSettingsHandler_SaveAndDelete_RefreshConnectionStatusOutOfBand(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := newFakeSecretsStore()
	engine := settingsRouter(settings.NewSettingsHandler(store))

	rec := postSetting(engine, config.KeyLunaAPIKey, url.Values{"value": {"luna-key"}})
	body := rec.Body.String()
	if !strings.Contains(body, `data-testid="secret-field-row-`+config.KeyLunaAPIKey+`"`) {
		t.Fatalf("Save response lacks the row; body=%s", body)
	}
	if !strings.Contains(body, `id="connection-status-luna"`) || !strings.Contains(body, `hx-swap-oob="true"`) || !strings.Contains(body, `data-state="configured"`) {
		t.Fatalf("Save response lacks the configured OOB status of luna; body=%s", body)
	}
	if strings.Contains(body, config.KeyLunaBaseURL) && strings.Contains(body, `data-testid="secret-field-row-`+config.KeyLunaBaseURL) {
		t.Errorf("Save response re-rendered the sibling field; body=%s", body)
	}

	rec = deleteSetting(engine, config.KeyLunaAPIKey)
	if !strings.Contains(rec.Body.String(), `data-state="default"`) {
		t.Fatalf("Delete response lacks the default (Jev) OOB status; body=%s", rec.Body.String())
	}
}

// issues #271/#274/#273: JEV_BASE_URL and JEV_MODEL default when unset, and so
// do the Luna/Sol/Opus overrides (Jev) and the news feed keys (やのしん), so the
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
	for _, key := range []string{
		config.KeyJevBaseURL, config.KeyJevModel,
		config.KeyLunaAPIKey, config.KeyLunaBaseURL, config.KeySolAPIKey, config.KeySolBaseURL, config.KeyOpusAPIKey, config.KeyOpusBaseURL,
		config.KeyNewsFeedURL, config.KeyNewsFeedAPIKey, config.KeyNewsFeedEnabled,
	} {
		if strings.Contains(body, key) {
			t.Errorf("banner lists defaulted key %s; body=%s", key, body)
		}
	}
}
