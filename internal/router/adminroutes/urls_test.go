package adminroutes_test

import (
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
)

// Every admin URL that existed before the hub keeps working (issue #793
// 受け入れ条件: 既存の URL はそのまま). Each row is a GET route that must be
// registered on the real router and must not answer 404 (a deleted or moved
// route would); `/admin` is the one new route.
func TestURLs_ExistingRoutesAreAlive(t *testing.T) {
	engine := newEngine(t, secrets{full: true})
	registered := map[string]bool{}
	for _, r := range engine.Routes() {
		if r.Method == http.MethodGet {
			registered[r.Path] = true
		}
	}

	tests := []struct {
		route  string // gin route pattern
		target string // concrete request
		want   int
	}{
		{"/", "/", http.StatusFound},
		{"/scanner", "/scanner", http.StatusOK},
		{"/scanner/scan", "/scanner/scan", http.StatusOK},
		{"/symbols/:symbol", "/symbols/7203", http.StatusOK},
		{"/watchlist", "/watchlist", http.StatusOK},
		{"/performance", "/performance", http.StatusOK},
		// The cost-sensitivity fragment needs its query; 400 (not 404) proves the route lives.
		{"/performance/cost-sensitivity", "/performance/cost-sensitivity", http.StatusBadRequest},
		{"/calibration", "/calibration", http.StatusOK},
		{"/activity", "/activity", http.StatusOK},
		{"/settings", "/settings", http.StatusOK},
		{"/setup", "/setup", http.StatusOK},
		{"/system/status", "/system/status", http.StatusOK},
		{"/system/secrets-status", "/system/secrets-status", http.StatusOK},
		{"/system/update-status", "/system/update-status", http.StatusOK},
		{"/system/update-panel", "/system/update-panel", http.StatusOK},
		{"/system/marketdata-status", "/system/marketdata-status", http.StatusOK},
		{"/system/daily-halt", "/system/daily-halt", http.StatusOK},
		{"/settings/mcp-panel", "/settings/mcp-panel", http.StatusOK},
		{"/admin", "/admin", http.StatusOK},
	}
	for _, tc := range tests {
		t.Run(tc.route, func(t *testing.T) {
			if !registered[tc.route] {
				t.Fatalf("GET %s is not a registered route", tc.route)
			}
			rec := get(engine, tc.target)
			if rec.Code == http.StatusNotFound {
				t.Fatalf("GET %s = 404; the URL must stay alive", tc.target)
			}
			if rec.Code != tc.want {
				t.Errorf("GET %s = %d, want %d", tc.target, rec.Code, tc.want)
			}
		})
	}
}

// `/` still lands on the Scanner, and the JSON API routes the screens call
// are still served.
func TestURLs_RootRedirectsToScannerAndAPIIsAlive(t *testing.T) {
	engine := newEngine(t, secrets{full: true})
	if loc := get(engine, "/").Header().Get("Location"); loc != "/scanner" {
		t.Errorf("GET / Location = %q, want /scanner", loc)
	}
	for _, target := range []string{"/api/v1/scanner", "/api/v1/system/status", "/api/v1/performance", "/api/v1/calibration"} {
		if rec := get(engine, target); rec.Code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200", target, rec.Code)
		}
	}
}

// /admin behaves like the other pages under the Setup Guard: while a
// required secret is unset it redirects to /setup instead of rendering; the
// hub is not an exempt path.
func TestAdmin_SetupGuardRedirectsLikeOtherPages(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := newEngine(t, secrets{full: false})
	for _, target := range []string{"/scanner", "/settings", "/admin"} {
		rec := get(engine, target)
		if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/setup" {
			t.Errorf("GET %s with setup incomplete = %d Location %q, want 302 /setup", target, rec.Code, rec.Header().Get("Location"))
		}
	}
	if rec := get(engine, "/setup"); rec.Code != http.StatusOK {
		t.Errorf("GET /setup = %d, want 200 (exempt from the guard)", rec.Code)
	}
}
