package router_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/router"
)

func TestNew_WatchlistPageIsIdleWithoutASourceAndLinkedFromTheNav(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := router.New()
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, authorize(t, engine, httptest.NewRequest(http.MethodGet, "/watchlist", nil)))
	if body := rec.Body.String(); rec.Code != http.StatusOK || !strings.Contains(body, `data-testid="watchlist-disabled"`) || !strings.Contains(body, `href="/watchlist"`) {
		t.Fatalf("GET /watchlist = %d %s", rec.Code, body)
	}
}
