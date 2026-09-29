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
	// getErr, if non-nil, makes Get fail for getErrKey (or every key when
	// getErrKey is empty).
	getErr    error
	getErrKey string
	setErr    error
	deleteErr error
}

func newFakeSecretsStore() *fakeSecretsStore {
	return &fakeSecretsStore{values: map[string]string{}}
}

func (f *fakeSecretsStore) Get(_ context.Context, key string) (string, bool, error) {
	if f.getErr != nil && (f.getErrKey == "" || f.getErrKey == key) {
		return "", false, f.getErr
	}
	value, ok := f.values[key]
	return value, ok, nil
}

func (f *fakeSecretsStore) Set(_ context.Context, key, plaintext string) error {
	if f.setErr != nil {
		return f.setErr
	}
	f.values[key] = plaintext
	return nil
}

func (f *fakeSecretsStore) Delete(_ context.Context, key string) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	delete(f.values, key)
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
	h := handler.NewSettingsHandler(store)
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
// Settings) - the broken required key is reported as missing instead.
func TestSettingsHandler_Status_StoreErrorDegradesGracefully(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := newFakeSecretsStore()
	store.values[config.KeyJevAPIKey] = "jev-key"
	store.values[config.KeyJevBaseURL] = "https://jev.example.com"
	store.values[config.KeyKabuAPIPassword] = "kabu-pass"
	store.getErr = errors.New("db is locked")
	store.getErrKey = config.KeyKabuAPIPassword
	h := handler.NewSettingsHandler(store)
	engine := gin.New()
	engine.GET("/system/secrets-status", h.Status)

	req := httptest.NewRequest(http.MethodGet, "/system/secrets-status", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (a broken key must not 500 the banner)", rec.Code, http.StatusOK)
	}
	if !strings.Contains(rec.Body.String(), config.KeyKabuAPIPassword) {
		t.Fatalf("banner does not report broken required key as missing; body=%s", rec.Body.String())
	}
}

func TestStaticSecretsStore_EverythingUnsetAndWritesAreNoOps(t *testing.T) {
	store := handler.StaticSecretsStore{}
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

// settingsRouter wires the per-key routes the way internal/router.New does.
func settingsRouter(h *handler.SettingsHandler) *gin.Engine {
	engine := gin.New()
	engine.GET("/settings", h.Page)
	engine.POST("/settings/:key", h.Save)
	engine.DELETE("/settings/:key", h.Delete)
	return engine
}

func postSetting(engine *gin.Engine, key string, form url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/settings/"+key, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

func deleteSetting(engine *gin.Engine, key string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/settings/"+key, nil))
	return rec
}

// TestSettingsHandler_Page_RendersOneIndependentRowPerAllowedKey covers
// issue #79: every allow-listed key (incl. the Luna/Sol/Opus/News Feed
// ones) gets its own row/form, and the screen has no shared form.
func TestSettingsHandler_Page_RendersOneIndependentRowPerAllowedKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := settingsRouter(handler.NewSettingsHandler(newFakeSecretsStore()))

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

func TestSettingsHandler_Save_StoresOnlyThePathKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := newFakeSecretsStore()
	store.values[config.KeyJevAPIKey] = "keep-jev"
	store.values[config.KeySlackWebhookURL] = "keep-slack"
	engine := settingsRouter(handler.NewSettingsHandler(store))

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
	engine := settingsRouter(handler.NewSettingsHandler(store))

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
	engine := settingsRouter(handler.NewSettingsHandler(store))

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

func TestSettingsHandler_SaveAndDelete_UnknownKeyIs400AndTouchesNothing(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := newFakeSecretsStore()
	store.values[config.KeyJevAPIKey] = "jev"
	engine := settingsRouter(handler.NewSettingsHandler(store))

	for _, key := range []string{"NOT_A_KEY", "jev_api_key", "PITHA_ENCRYPTION_KEY"} {
		if rec := postSetting(engine, key, url.Values{"value": {"x"}}); rec.Code != http.StatusBadRequest {
			t.Errorf("POST /settings/%s status = %d, want %d", key, rec.Code, http.StatusBadRequest)
		}
		if rec := deleteSetting(engine, key); rec.Code != http.StatusBadRequest {
			t.Errorf("DELETE /settings/%s status = %d, want %d", key, rec.Code, http.StatusBadRequest)
		}
	}
	if len(store.values) != 1 || store.values[config.KeyJevAPIKey] != "jev" {
		t.Fatalf("store = %v after rejected requests, want it untouched", store.values)
	}
}

func TestSettingsHandler_Delete_RemovesOnlyThePathKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := newFakeSecretsStore()
	store.values[config.KeyJevAPIKey] = "jev"
	store.values[config.KeyLunaAPIKey] = "luna"
	engine := settingsRouter(handler.NewSettingsHandler(store))

	rec := deleteSetting(engine, config.KeyLunaAPIKey)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body=%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	if _, ok := store.values[config.KeyLunaAPIKey]; ok {
		t.Errorf("LUNA_API_KEY still stored after DELETE")
	}
	if store.values[config.KeyJevAPIKey] != "jev" {
		t.Errorf("JEV_API_KEY = %q after deleting LUNA_API_KEY, want jev", store.values[config.KeyJevAPIKey])
	}
	body := rec.Body.String()
	if strings.Contains(body, `data-testid="configured-`+config.KeyLunaAPIKey+`"`) || strings.Contains(body, `data-testid="delete-`+config.KeyLunaAPIKey+`"`) {
		t.Errorf("refreshed row still shows LUNA_API_KEY as configured/deletable; body=%s", body)
	}
}

func TestSettingsHandler_SaveAndDelete_StoreErrorIs500(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := newFakeSecretsStore()
	store.setErr = errors.New("db is locked")
	store.deleteErr = errors.New("db is locked")
	engine := settingsRouter(handler.NewSettingsHandler(store))

	if rec := postSetting(engine, config.KeyJevAPIKey, url.Values{"value": {"x"}}); rec.Code != http.StatusInternalServerError {
		t.Errorf("POST status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	if rec := deleteSetting(engine, config.KeyJevAPIKey); rec.Code != http.StatusInternalServerError {
		t.Errorf("DELETE status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}
