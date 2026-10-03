package apiroutes_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/router"
	"github.com/ousiassllc/pitha-trador/internal/web/insightapi"
)

func TestNew_RegistersInsightAPIRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := router.New(router.WithInsightProvider(insightapi.StaticProvider{}))

	for _, path := range []string{
		"/api/v1/signals", "/api/v1/signals/7203", "/api/v1/symbols/7203/decisions", "/api/v1/performance",
	} {
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s = %d, want %d (body=%s)", path, rec.Code, http.StatusOK, rec.Body.String())
		}
	}

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/openapi.json", nil))
	for _, want := range []string{"/signals/{symbol}", "/performance", "/symbols/{symbol}/decisions"} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Fatalf("openapi.json missing path %q", want)
		}
	}
}
