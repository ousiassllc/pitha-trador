package pages_test

import (
	"bytes"
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/a-h/templ"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/web/pages"
)

func renderPage(t *testing.T, c templ.Component) string {
	t.Helper()
	var buf bytes.Buffer
	if err := c.Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	return buf.String()
}

// currentTab returns the href of the tab marked aria-current in the `<nav
// aria-label=label>` tab bar, and the hrefs of all its tabs.
func currentTab(t *testing.T, body, label string) (current string, all []string) {
	t.Helper()
	nav := regexp.MustCompile(`(?s)<nav aria-label="` + label + `"[^>]*>.*?</nav>`).FindString(body)
	if nav == "" {
		t.Fatalf("no tab bar %q in %.400s", label, body)
	}
	for _, m := range regexp.MustCompile(`<a\b([^>]*)>`).FindAllStringSubmatch(nav, -1) {
		href := regexp.MustCompile(`\bhref="([^"]+)"`).FindStringSubmatch(m[1])[1]
		all = append(all, href)
		if strings.Contains(m[1], `aria-current="page"`) {
			current = href
		}
	}
	return current, all
}

// 候補銘柄 has the tabs 候補 (/scanner) and 監視リスト (/watchlist); each page
// marks its own tab (issue #895).
func TestCandidateTabs_ScannerAndWatchlistMarkTheirOwnTab(t *testing.T) {
	scanner := renderScannerPage(t, nil)
	watchlist := renderPage(t, pages.WatchlistPage(domain.WatchListView{}))
	for name, tc := range map[string]struct{ body, want string }{"scanner": {scanner, "/scanner"}, "watchlist": {watchlist, "/watchlist"}} {
		current, all := currentTab(t, tc.body, "候補銘柄のタブ")
		if current != tc.want || strings.Join(all, " ") != "/scanner /watchlist" {
			t.Errorf("%s: current tab %q of %v, want %q of [/scanner /watchlist]", name, current, all, tc.want)
		}
		for _, label := range []string{">候補</a>", ">監視リスト</a>"} {
			if !strings.Contains(tc.body, label) {
				t.Errorf("%s lacks the tab %s", name, label)
			}
		}
	}
	if !strings.Contains(watchlist, `href="/settings/broker"`) {
		t.Errorf("the watchlist description must link to /settings/broker")
	}
}

// The 成績 tabs are 実績, バックテスト and 予測の精度 (the Calibration page).
func TestResultTabs_CalibrationMarksItsOwnTab(t *testing.T) {
	current, all := currentTab(t, renderPage(t, pages.CalibrationPage()), "成績のタブ")
	if current != "/calibration" || strings.Join(all, " ") != "/performance /performance?tab=backtest /calibration" {
		t.Errorf("calibration: current tab %q of %v", current, all)
	}
	current, _ = currentTab(t, renderPerformancePage(t, pages.PerformanceProps{}), "成績のタブ")
	if current != "/performance" {
		t.Errorf("performance: current tab %q, want /performance", current)
	}
}
