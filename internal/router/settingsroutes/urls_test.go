package settingsroutes_test

import (
	"net/http"
	"strings"
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
		// `/` is the Home screen since issue #896 (it used to redirect to /scanner), and its card fragments are alive.
		{"/", "/", http.StatusOK},
		{"/home/trade-status", "/home/trade-status", http.StatusOK},
		{"/home/pnl", "/home/pnl", http.StatusOK},
		{"/home/positions", "/home/positions", http.StatusOK},
		{"/home/attention", "/home/attention", http.StatusOK},
		{"/home/details", "/home/details", http.StatusOK},
		{"/home/attention-badge", "/home/attention-badge", http.StatusOK},
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
		{"/system/update-panel", "/system/update-panel", http.StatusOK},
		{"/settings/mcp-panel", "/settings/mcp-panel", http.StatusOK},
		{"/admin", "/admin", http.StatusMovedPermanently},
		// 資産ごとの予算枠 is part of リスク・資金 since issue #895.
		{"/settings/asset-buckets", "/settings/asset-buckets", http.StatusMovedPermanently},
		{"/order-proposals", "/order-proposals", http.StatusOK},
		// The old `/performance#backtest` bookmark and the new backtest tab are the same route.
		{"/performance", "/performance?tab=backtest", http.StatusOK},
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

// The header's banner fragments were retired with the Home screen (issues #896,
// #901): their routes are gone, the 要対応 badge and list replaced them.
func TestURLs_RetiredBannerFragmentsAnswer404(t *testing.T) {
	engine := newEngine(t, secrets{full: true})
	for _, target := range []string{
		"/system/secrets-status", "/system/update-status", "/system/marketdata-status",
		"/system/daily-halt", "/system/health-alerts",
	} {
		t.Run(target, func(t *testing.T) {
			if rec := get(engine, target); rec.Code != http.StatusNotFound {
				t.Errorf("GET %s = %d, want 404", target, rec.Code)
			}
		})
	}
}

// `/` is the Home screen (issue #896) and no longer redirects; the Scanner stays at
// `/scanner`, and the JSON API routes the screens call are still served.
func TestURLs_RootIsHomeScannerStaysAndAPIIsAlive(t *testing.T) {
	engine := newEngine(t, secrets{full: true})
	if rec := get(engine, "/"); rec.Code != http.StatusOK || rec.Header().Get("Location") != "" {
		t.Errorf("GET / = %d Location %q, want 200 (Home) with no redirect", rec.Code, rec.Header().Get("Location"))
	}
	if rec := get(engine, "/scanner"); rec.Code != http.StatusOK {
		t.Errorf("GET /scanner = %d, want 200", rec.Code)
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

// The retired 予算枠 page answers 301 to リスク・資金, and the pre-#805 hash
// links onto it (`#modal-asset-buckets`) are forwarded by the hub's route map.
func TestAssetBucketsPage_RedirectsToRisk(t *testing.T) {
	engine := newEngine(t, secrets{full: true})
	rec := get(engine, "/settings/asset-buckets")
	if rec.Code != http.StatusMovedPermanently || rec.Header().Get("Location") != "/settings/risk" {
		t.Errorf("GET /settings/asset-buckets = %d Location %q, want 301 /settings/risk", rec.Code, rec.Header().Get("Location"))
	}
	hub := get(engine, "/settings").Body.String()
	if !strings.Contains(hub, "&#34;modal-asset-buckets&#34;:&#34;/settings/risk&#34;") {
		t.Errorf("hub route map does not forward #modal-asset-buckets to /settings/risk")
	}
}
