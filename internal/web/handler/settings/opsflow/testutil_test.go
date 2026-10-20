package opsflow_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/web/handler/settings"
	"github.com/ousiassllc/pitha-trador/internal/web/settingspages"
)

// fakeSecretsStore is an in-memory settings.SecretsStore.
type fakeSecretsStore struct{ values map[string]string }

func newFakeSecretsStore() *fakeSecretsStore {
	return &fakeSecretsStore{values: map[string]string{}}
}

func (f *fakeSecretsStore) Get(_ context.Context, key string) (string, bool, error) {
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

// settingsRouter wires the Settings page the way internal/router.New does.
func settingsRouter(h *settings.SettingsHandler) *gin.Engine {
	engine := gin.New()
	engine.GET("/settings", h.Page)
	for _, page := range settingspages.Pages() {
		engine.GET(page.Path(), h.SubPage(page, false))
	}
	return engine
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
