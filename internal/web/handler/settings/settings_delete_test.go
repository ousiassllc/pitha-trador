package settings_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/settings"
)

func TestSettingsHandler_SaveAndDelete_UnknownKeyIs400AndTouchesNothing(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := newFakeSecretsStore()
	store.values[config.KeyJevAPIKey] = "jev"
	engine := settingsRouter(settings.NewSettingsHandler(store))

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
	engine := settingsRouter(settings.NewSettingsHandler(store))

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
	engine := settingsRouter(settings.NewSettingsHandler(store))

	if rec := postSetting(engine, config.KeyJevAPIKey, url.Values{"value": {"x"}}); rec.Code != http.StatusInternalServerError {
		t.Errorf("POST status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	if rec := deleteSetting(engine, config.KeyJevAPIKey); rec.Code != http.StatusInternalServerError {
		t.Errorf("DELETE status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}

// Failed HTMX actions answer with an atoms.Toast fragment, which
// layout.Shell's htmx-config swaps into `#toast-region` (issue #110).
func TestSettingsHandler_SaveAndDelete_FailuresRenderToastFragment(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := newFakeSecretsStore()
	store.setErr = errors.New("db is locked")
	store.deleteErr = errors.New("db is locked")
	engine := settingsRouter(settings.NewSettingsHandler(store))

	for name, rec := range map[string]*httptest.ResponseRecorder{
		"save 500":       postSetting(engine, config.KeyJevAPIKey, url.Values{"value": {"x"}}),
		"save 400":       postSetting(engine, config.KeyJevAPIKey, url.Values{"value": {""}}),
		"save unknown":   postSetting(engine, "NOT_A_KEY", url.Values{"value": {"x"}}),
		"delete 500":     deleteSetting(engine, config.KeyJevAPIKey),
		"delete unknown": deleteSetting(engine, "NOT_A_KEY"),
	} {
		body := rec.Body.String()
		if !strings.Contains(body, `data-toast`) || !strings.Contains(body, `role="alert"`) {
			t.Errorf("%s: body = %q, want an atoms.Toast fragment", name, body)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
			t.Errorf("%s: Content-Type = %q, want text/html", name, ct)
		}
	}
}

// The row form's plain `method="post"` fallback (no JS) must not get a
// bare SecretFieldRow fragment as a whole page: it is redirected back to
// the screen it was submitted from (issue #110), after the write went
// through.
func TestSettingsHandler_NonHTMXSaveAndDelete_RedirectBackToOriginScreen(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := newFakeSecretsStore()
	engine := settingsRouter(settings.NewSettingsHandler(store))

	for _, tc := range []struct{ referer, want string }{
		{"", "/settings"},
		{"http://127.0.0.1:48080/settings", "/settings"},
		{"http://127.0.0.1:48080/setup", "/setup"},
		{"https://evil.example/anything", "/settings"},
	} {
		req := httptest.NewRequest(http.MethodPost, "/settings/"+config.KeyJevAPIKey, strings.NewReader(url.Values{"value": {"v"}}.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if tc.referer != "" {
			req.Header.Set("Referer", tc.referer)
		}
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)

		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != tc.want {
			t.Errorf("referer %q: status/Location = %d/%q, want 303/%q", tc.referer, rec.Code, rec.Header().Get("Location"), tc.want)
		}
		if store.values[config.KeyJevAPIKey] != "v" {
			t.Errorf("referer %q: JEV_API_KEY = %q, want the value stored despite the redirect", tc.referer, store.values[config.KeyJevAPIKey])
		}
	}

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/settings/"+config.KeyJevAPIKey, nil))
	if rec.Code != http.StatusSeeOther {
		t.Errorf("DELETE status = %d, want 303", rec.Code)
	}
	if _, ok := store.values[config.KeyJevAPIKey]; ok {
		t.Errorf("JEV_API_KEY still stored after DELETE")
	}
}

// The row form's plain `method="post"` fallback (no JS/htmx) used to get a
// bare Toast fragment as the whole page on failure (issue #184); it must get
// the full pages.ErrorPage instead.
func TestSettingsHandler_NonHTMXFailures_RenderFullErrorPage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := newFakeSecretsStore()
	store.deleteErr = http.ErrAbortHandler
	engine := settingsRouter(settings.NewSettingsHandler(store))

	form := url.Values{"value": {""}, "_csrf": {"token"}}
	post := httptest.NewRequest(http.MethodPost, "/settings/"+config.KeyJevAPIKey, strings.NewReader(form.Encode()))
	post.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	postRec := httptest.NewRecorder()
	engine.ServeHTTP(postRec, post)

	deleteRec := httptest.NewRecorder()
	engine.ServeHTTP(deleteRec, httptest.NewRequest(http.MethodDelete, "/settings/"+config.KeyJevAPIKey, nil))

	for name, tc := range map[string]struct {
		rec  *httptest.ResponseRecorder
		want int
	}{
		"empty value": {postRec, http.StatusBadRequest},
		"delete 500":  {deleteRec, http.StatusInternalServerError},
	} {
		body := tc.rec.Body.String()
		if tc.rec.Code != tc.want {
			t.Errorf("%s: status = %d, want %d", name, tc.rec.Code, tc.want)
		}
		if !strings.Contains(body, `data-testid="error-page"`) || !strings.Contains(body, "<html") {
			t.Errorf("%s: body = %q, want the full error page", name, body)
		}
	}
}
