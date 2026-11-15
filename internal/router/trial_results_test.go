package router_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/router"
)

// The お試し tab of 成績 is routed read-only, and every 成績 page links to it.
func TestNew_TrialResultsTabIsReadOnlyAndLinkedFromPerformance(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := router.New()
	get := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec
	}
	if rec := get("/performance/trial"); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `data-testid="trial-empty"`) {
		t.Fatalf("GET /performance/trial = %d: %s", rec.Code, rec.Body.String())
	}
	for _, path := range []string{"/performance", "/performance?tab=backtest", "/calibration", "/performance/trial"} {
		if body := get(path).Body.String(); !strings.Contains(body, `href="/performance/trial"`) {
			t.Errorf("%s has no お試し tab", path)
		}
	}
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, httptest.NewRequest(method, "/performance/trial", nil))
		if rec.Code == http.StatusOK {
			t.Errorf("%s /performance/trial = 200, want it refused", method)
		}
	}
}
