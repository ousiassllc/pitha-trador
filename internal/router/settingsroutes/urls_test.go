package settingsroutes_test

import (
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/web/settingspages"
)

// Every admin URL that existed before the Settings pages keeps working (issue
// #793 / #805 受け入れ条件: 既存の URL はそのまま). Each row is a GET route that
// must be registered on the real router and must not answer 404 (a deleted or
// moved route would); the Settings pages are the new routes and `/admin` is
// the retired hub, now a redirect.
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
		{"/admin", "/admin", http.StatusMovedPermanently},
	}
	for _, page := range settingspages.Pages() {
		tests = append(tests, struct {
			route  string
			target string
			want   int
		}{page.Path(), page.Path(), http.StatusOK})
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

// `/admin` (the retired hub) lands on the Settings entry.
func TestAdmin_RedirectsToSettings(t *testing.T) {
	rec := get(newEngine(t, secrets{full: true}), "/admin")
	if rec.Code != http.StatusMovedPermanently || rec.Header().Get("Location") != "/settings" {
		t.Errorf("GET /admin = %d Location %q, want 301 /settings", rec.Code, rec.Header().Get("Location"))
	}
}

// The Settings hub and its pages behave like the other pages under the Setup
// Guard: while a required secret is unset they redirect to /setup instead of
// rendering; they are not exempt paths.
func TestSettings_SetupGuardRedirectsLikeOtherPages(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := newEngine(t, secrets{full: false})
	targets := []string{"/scanner", "/settings", "/admin"}
	for _, page := range settingspages.Pages() {
		targets = append(targets, page.Path())
	}
	for _, target := range targets {
		rec := get(engine, target)
		if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/setup" {
			t.Errorf("GET %s with setup incomplete = %d Location %q, want 302 /setup", target, rec.Code, rec.Header().Get("Location"))
		}
	}
	if rec := get(engine, "/setup"); rec.Code != http.StatusOK {
		t.Errorf("GET /setup = %d, want 200 (exempt from the guard)", rec.Code)
	}
}
