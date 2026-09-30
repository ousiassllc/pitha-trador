package router_test

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

func TestNew_ActivityRoutesServePageAndAPIWithNavLink(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := router.New()

	page := httptest.NewRecorder()
	engine.ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/activity", nil))
	if page.Code != http.StatusOK {
		t.Fatalf("GET /activity status = %d: %s", page.Code, page.Body.String())
	}
	for _, want := range []string{"<pitha-activity-feed", `data-queue="market-data"`, `href="/activity"`} {
		if !strings.Contains(page.Body.String(), want) {
			t.Fatalf("GET /activity body missing %q", want)
		}
	}

	api := httptest.NewRecorder()
	engine.ServeHTTP(api, httptest.NewRequest(http.MethodGet, "/api/v1/activity", nil))
	if api.Code != http.StatusOK {
		t.Fatalf("GET /api/v1/activity status = %d: %s", api.Code, api.Body.String())
	}
	if !strings.Contains(api.Body.String(), `"queue":"analytics"`) {
		t.Fatalf("GET /api/v1/activity body = %s, want the 8-queue snapshot", api.Body.String())
	}
}
