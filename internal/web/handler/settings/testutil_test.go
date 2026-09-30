package settings_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/web/handler/settings"
)

// fakeSecretsStore is a configurable settings.SecretsStore for tests,
// backed by an in-memory map instead of a real
// internal/repository/system.SecretsRepository (which needs a *sql.DB).
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

// settingsRouter wires the per-key routes the way internal/router.New does.
func settingsRouter(h *settings.SettingsHandler) *gin.Engine {
	engine := gin.New()
	engine.GET("/settings", h.Page)
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
