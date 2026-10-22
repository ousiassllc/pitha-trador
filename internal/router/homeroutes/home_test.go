package homeroutes_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/router"
)

// `GET /` is the Home screen (issue #896), not a redirect: it renders the top
// row, marks ホーム as the current menu entry and registers the card fragments
// the 30 s refresh fetches.
func TestRootRouteServesTheHomeScreen(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := router.New()

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET / = %d, want 200 (Home, no redirect)", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "" {
		t.Errorf("GET / redirects to %q", loc)
	}
	body := rec.Body.String()
	for _, want := range []string{"home-trade-status-card", "home-pnl-card", "home-positions-card", "home-attention-section", `aria-current="page"`} {
		if !strings.Contains(body, want) {
			t.Errorf("Home lacks %s", want)
		}
	}
	for _, path := range []string{"/home/trade-status", "/home/pnl", "/home/positions", "/home/attention", "/home/details", "/home/attention-badge"} {
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200", path, rec.Code)
		}
	}
}

// The Home screen is read-only (issue #896): nothing but GET is routed at `/`
// or under `/home/`.
func TestHomeRegistersNoWriteRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, r := range router.New().Routes() {
		if (r.Path == "/" || strings.HasPrefix(r.Path, "/home/")) && r.Method != http.MethodGet && r.Method != http.MethodHead {
			t.Errorf("%s %s: Home must register GET routes only", r.Method, r.Path)
		}
	}
}
