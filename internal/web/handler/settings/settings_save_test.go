package settings_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/settings"
)

func TestSettingsHandler_Save_StoresOnlyThePathKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := newFakeSecretsStore()
	store.values[config.KeyJevAPIKey] = "keep-jev"
	store.values[config.KeySlackWebhookURL] = "keep-slack"
	engine := settingsRouter(settings.NewSettingsHandler(store))

	// Extra fields for other keys in the body must be ignored.
	rec := postSetting(engine, config.KeyKabuAPIPassword, url.Values{
		"value":              {"kabu-pass"},
		config.KeyJevAPIKey:  {"attacker"},
		config.KeyLunaAPIKey: {"luna-key"},
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body=%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	want := map[string]string{
		config.KeyKabuAPIPassword: "kabu-pass",
		config.KeyJevAPIKey:       "keep-jev",
		config.KeySlackWebhookURL: "keep-slack",
	}
	if len(store.values) != len(want) {
		t.Errorf("store = %v, want exactly %v", store.values, want)
	}
	for key, value := range want {
		if store.values[key] != value {
			t.Errorf("stored %s = %q, want %q", key, store.values[key], value)
		}
	}
	body := rec.Body.String()
	if !strings.Contains(body, `data-testid="notice-`+config.KeyKabuAPIPassword+`"`) ||
		!strings.Contains(body, `data-testid="configured-`+config.KeyKabuAPIPassword+`"`) {
		t.Errorf("response is not the refreshed KABU_API_PASSWORD row with a notice; body=%s", body)
	}
	if strings.Contains(body, "kabu-pass") || strings.Contains(body, "<html") {
		t.Errorf("response leaks the value or is not a fragment; body=%s", body)
	}
}

func TestSettingsHandler_Save_StoresAIAndNewsFeedCredentials(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := newFakeSecretsStore()
	engine := settingsRouter(settings.NewSettingsHandler(store))

	want := map[string]string{
		config.KeyLunaAPIKey:     "luna-key",
		config.KeyLunaBaseURL:    "https://luna.example.com",
		config.KeyNewsFeedURL:    "https://news.example.com/feed",
		config.KeyNewsFeedAPIKey: "news-key",
		config.KeySolAPIKey:      "sol-key",
		config.KeySolBaseURL:     "https://sol.example.com",
		config.KeyOpusAPIKey:     "opus-key",
		config.KeyOpusBaseURL:    "https://opus.example.com",
	}
	for key, value := range want {
		if rec := postSetting(engine, key, url.Values{"value": {value}}); rec.Code != http.StatusOK {
			t.Fatalf("POST /settings/%s status = %d, want %d", key, rec.Code, http.StatusOK)
		}
	}
	for key, value := range want {
		if got := store.values[key]; got != value {
			t.Errorf("stored %s = %q, want %q", key, got, value)
		}
	}
}

func TestSettingsHandler_Save_EmptyValueIsRejectedAndKeepsStoredValue(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := newFakeSecretsStore()
	store.values[config.KeyJevAPIKey] = "old-value"
	engine := settingsRouter(settings.NewSettingsHandler(store))

	for name, form := range map[string]url.Values{"blank": {"value": {""}}, "missing": {}} {
		rec := postSetting(engine, config.KeyJevAPIKey, form)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want %d", name, rec.Code, http.StatusBadRequest)
		}
	}
	if store.values[config.KeyJevAPIKey] != "old-value" {
		t.Fatalf("JEV_API_KEY = %q after blank submits, want old-value kept", store.values[config.KeyJevAPIKey])
	}
}

// issue #235: surrounding whitespace pasted with a value must not be stored.
func TestSettingsHandler_Save_TrimsWhitespaceBeforeStoring(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := newFakeSecretsStore()
	engine := settingsRouter(settings.NewSettingsHandler(store))

	for key, tc := range map[string]struct{ raw, want string }{
		config.KeyJevAPIKey:       {" jev-key\n", "jev-key"},
		config.KeySlackWebhookURL: {"http://127.0.0.1:1 ", "http://127.0.0.1:1"},
	} {
		if rec := postSetting(engine, key, url.Values{"value": {tc.raw}}); rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d, want 200 (body=%s)", key, rec.Code, rec.Body.String())
		}
		if store.values[key] != tc.want {
			t.Errorf("stored %s = %q, want %q", key, store.values[key], tc.want)
		}
	}
}

// A whitespace-only value is empty after trimming: rejected like a blank
// one, keeping the stored value.
func TestSettingsHandler_Save_WhitespaceOnlyIsRejected(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := newFakeSecretsStore()
	store.values[config.KeyJevAPIKey] = "old"
	engine := settingsRouter(settings.NewSettingsHandler(store))

	if rec := postSetting(engine, config.KeyJevAPIKey, url.Values{"value": {" \n\t"}}); rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if store.values[config.KeyJevAPIKey] != "old" {
		t.Fatalf("JEV_API_KEY = %q, want old kept", store.values[config.KeyJevAPIKey])
	}
}

// Invalid values are a 400 and never reach the store - in particular an
// invalid JEV_BASE_URL must not satisfy the required-key check that
// releases the Setup Guard.
func TestSettingsHandler_Save_InvalidValuesAre400AndStoreUntouched(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct{ name, key, value string }{
		{"not a url", config.KeyJevBaseURL, "notaurl"},
		{"ftp scheme", config.KeySlackWebhookURL, "ftp://x"},
		{"javascript scheme", config.KeySlackWebhookURL, "javascript:alert(1)"},
		{"no host", config.KeyLunaBaseURL, "https:///path"},
		{"newline in api key", config.KeyJevAPIKey, "ab\ncd"},
		{"tab in password", config.KeyKabuAPIPassword, "ab\tcd"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := newFakeSecretsStore()
			engine := settingsRouter(settings.NewSettingsHandler(store))

			rec := postSetting(engine, tc.key, url.Values{"value": {tc.value}})
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (body=%s)", rec.Code, rec.Body.String())
			}
			if len(store.values) != 0 {
				t.Fatalf("store = %v, want untouched", store.values)
			}
			if body := rec.Body.String(); !strings.Contains(body, `data-toast`) || !strings.Contains(body, `role="alert"`) {
				t.Errorf("HTMX body = %q, want an atoms.Toast fragment", body)
			}
			if strings.Contains(rec.Body.String(), tc.value) {
				t.Errorf("error response echoes the rejected value: %q", rec.Body.String())
			}

			// Non-JS form post: full error page, not a bare fragment.
			req := httptest.NewRequest(http.MethodPost, "/settings/"+tc.key, strings.NewReader(url.Values{"value": {tc.value}}.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			plain := httptest.NewRecorder()
			engine.ServeHTTP(plain, req)
			if plain.Code != http.StatusBadRequest || !strings.Contains(plain.Body.String(), `data-testid="error-page"`) ||
				!strings.Contains(plain.Body.String(), "<html") {
				t.Errorf("non-HTMX: status = %d body = %q, want 400 full error page", plain.Code, plain.Body.String())
			}
			if len(store.values) != 0 {
				t.Fatalf("store = %v after non-HTMX post, want untouched", store.values)
			}
		})
	}
}

// issue #273: NEWS_FEED_ENABLED switches News Ingest off; only on/off are
// accepted (stored lower-cased), anything else is a 400 that stores nothing.
func TestSettingsHandler_Save_NewsFeedEnabledAcceptsOnlyOnOff(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := newFakeSecretsStore()
	engine := settingsRouter(settings.NewSettingsHandler(store))

	if rec := postSetting(engine, config.KeyNewsFeedEnabled, url.Values{"value": {" OFF "}}); rec.Code != http.StatusOK {
		t.Fatalf("POST off status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := store.values[config.KeyNewsFeedEnabled]; got != "off" {
		t.Fatalf("stored NEWS_FEED_ENABLED = %q, want off", got)
	}
	if rec := postSetting(engine, config.KeyNewsFeedEnabled, url.Values{"value": {"maybe"}}); rec.Code != http.StatusBadRequest {
		t.Fatalf("POST maybe status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if got := store.values[config.KeyNewsFeedEnabled]; got != "off" {
		t.Fatalf("invalid value changed the store to %q", got)
	}
}

// issue #695: a successful HTMX Save/Delete fires secretsStatusChanged so
// Header's #config-banner refetches the unset-optional-keys list; failures
// and the non-HTMX redirect fire nothing.
func TestSettingsHandler_SaveAndDelete_FireSecretsStatusChanged(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := newFakeSecretsStore()
	engine := settingsRouter(settings.NewSettingsHandler(store))

	if got := postSetting(engine, config.KeySlackWebhookURL, url.Values{"value": {"https://hooks.example/x"}}).Header().Get("HX-Trigger"); got != "secretsStatusChanged" {
		t.Errorf("Save HX-Trigger = %q, want secretsStatusChanged", got)
	}
	if got := deleteSetting(engine, config.KeySlackWebhookURL).Header().Get("HX-Trigger"); got != "secretsStatusChanged" {
		t.Errorf("Delete HX-Trigger = %q, want secretsStatusChanged", got)
	}

	if rec := postSetting(engine, config.KeySlackWebhookURL, url.Values{"value": {""}}); rec.Code != http.StatusBadRequest || rec.Header().Get("HX-Trigger") != "" {
		t.Errorf("rejected Save status/HX-Trigger = %d/%q, want 400 and none", rec.Code, rec.Header().Get("HX-Trigger"))
	}

	req := httptest.NewRequest(http.MethodPost, "/settings/"+config.KeySlackWebhookURL, strings.NewReader(url.Values{"value": {"v"}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	if got := rec.Header().Get("HX-Trigger"); got != "" {
		t.Errorf("non-HTMX redirect HX-Trigger = %q, want none", got)
	}
}
