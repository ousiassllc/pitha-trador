// Package pageflow tests the /settings and /setup pages (connection grouping,
// card state, Setup completion) through the real SettingsHandler over an
// in-memory SecretsStore.
package pageflow_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/web/handler/settings"
	"github.com/ousiassllc/pitha-trador/internal/web/settingspages"
)

// fakeSecretsStore is an in-memory settings.SecretsStore (a real
// internal/repository/system.SecretsRepository needs a *sql.DB).
type fakeSecretsStore struct {
	values map[string]string
	// getErr, if non-nil, makes Get fail for getErrKey (or every key when
	// getErrKey is empty).
	getErr    error
	getErrKey string
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
	f.values[key] = plaintext
	return nil
}

func (f *fakeSecretsStore) Delete(_ context.Context, key string) error {
	delete(f.values, key)
	return nil
}

// settingsRouter wires the per-key routes the way internal/router.New does.
func settingsRouter(h *settings.SettingsHandler) *gin.Engine {
	engine := gin.New()
	engine.GET("/settings", h.Page)
	for _, page := range settingspages.Pages() {
		engine.GET(page.Path(), h.SubPage(page, false))
	}
	engine.POST("/settings/:key", h.Save)
	engine.DELETE("/settings/:key", h.Delete)
	return engine
}

func postSetting(engine *gin.Engine, key string, form url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/settings/"+key, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

func deleteSetting(engine *gin.Engine, key string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodDelete, "/settings/"+key, nil)
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

// allPagesBody is the concatenated markup of every Settings page (issue
// #805), for assertions about the Settings screens as a whole.
func allPagesBody(t *testing.T, engine *gin.Engine) string {
	t.Helper()
	var body strings.Builder
	for _, page := range settingspages.Pages() {
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, page.Path(), nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s = %d, want 200", page.Path(), rec.Code)
		}
		body.WriteString(rec.Body.String())
	}
	return body.String()
}
