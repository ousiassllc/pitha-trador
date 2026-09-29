package middleware_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/web/middleware"
)

type stubSecrets struct {
	values map[string]string
	err    error
}

func (s stubSecrets) Get(_ context.Context, key string) (string, bool, error) {
	if s.err != nil {
		return "", false, s.err
	}
	v, ok := s.values[key]
	return v, ok, nil
}

func serve(t *testing.T, store middleware.SecretsReader, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(middleware.SetupGuard(store, []string{"A", "B"}))
	engine.NoRoute(func(c *gin.Context) { c.String(http.StatusOK, "reached") })
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec
}

func TestSetupGuard_Exemptions(t *testing.T) {
	unset := stubSecrets{values: map[string]string{"A": "x"}}
	for _, tc := range []struct {
		method, path string
		redirected   bool
	}{
		{http.MethodGet, "/setup", false},
		{http.MethodGet, "/static/dist/css/app.css", false},
		{http.MethodPost, "/settings/A", false},
		{http.MethodDelete, "/settings/A", false},
		{http.MethodGet, "/settings/A", true},
		{http.MethodGet, "/settings", true},
		{http.MethodGet, "/staticfoo", true},
		{http.MethodGet, "/scanner", true},
	} {
		rec := serve(t, unset, tc.method, tc.path)
		if got := rec.Code == http.StatusFound && rec.Header().Get("Location") == "/setup"; got != tc.redirected {
			t.Errorf("%s %s redirected=%v (code %d), want %v", tc.method, tc.path, got, rec.Code, tc.redirected)
		}
	}
}

func TestSetupGuard_PassesThroughWhenAllRequiredKeysSet(t *testing.T) {
	rec := serve(t, stubSecrets{values: map[string]string{"A": "x", "B": "y"}}, http.MethodGet, "/scanner")
	if rec.Code != http.StatusOK || rec.Body.String() != "reached" {
		t.Fatalf("GET /scanner = %d %q, want handler reached", rec.Code, rec.Body.String())
	}
}

// A store error must not let requests through as if setup were done.
func TestSetupGuard_StoreErrorCountsAsUnset(t *testing.T) {
	rec := serve(t, stubSecrets{err: errors.New("db is locked")}, http.MethodGet, "/scanner")
	if rec.Code != http.StatusFound {
		t.Fatalf("GET /scanner with unreadable store = %d, want 302", rec.Code)
	}
}
