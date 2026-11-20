package pages_test

import (
	"bytes"
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/a-h/templ"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/web/organisms/trialresults"
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

// The 成績 tabs are 実績, バックテスト, 予測の精度 (the Calibration page) and お試し
// (the model-comparison trial's results).
func TestResultTabs_EachPageMarksItsOwnTab(t *testing.T) {
	const all = "/performance /performance?tab=backtest /calibration /performance/trial"
	for name, tc := range map[string]struct{ body, want string }{
		"calibration": {renderPage(t, pages.CalibrationPage()), "/calibration"},
		"performance": {renderPerformancePage(t, pages.PerformanceProps{}), "/performance"},
		"backtest":    {renderPerformancePage(t, pages.PerformanceProps{BacktestTab: true}), "/performance?tab=backtest"},
		"trial":       {renderPage(t, pages.TrialResultsPage(trialresults.Props{Empty: true})), "/performance/trial"},
	} {
		current, got := currentTab(t, tc.body, "成績のタブ")
		if current != tc.want || strings.Join(got, " ") != all {
			t.Errorf("%s: current tab %q of %v, want %q of [%s]", name, current, got, tc.want, all)
		}
		if !strings.Contains(tc.body, ">お試し</a>") {
			t.Errorf("%s lacks the お試し tab", name)
		}
	}
}

// The heatmap and the exit-alignment sections each start 48px below the
// block above (tab bar / heatmap): the Lit hosts have no margin of their own
// (brand-guide §3 S5; issue #968).
func TestCalibrationPage_LitSectionsAreSeparatedByTheSectionGap(t *testing.T) {
	body := renderPage(t, pages.CalibrationPage())
	for _, want := range []string{`<pitha-calibration-heatmap class="mt-12" `, `<pitha-exit-alignment class="mt-12" `} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q: %s", want, body)
		}
	}
}

// issue #991 (S2): tabs are pills: 40px high (min-h-10), 8px apart (gap-2),
// 16px on the sides (px-4), no underline and no bar border.
func TestTabNav_FollowsTheMenuItemRecipe(t *testing.T) {
	body := renderPage(t, pages.PerformancePage(pages.PerformanceProps{}))
	if !strings.Contains(body, `class="mt-6 flex flex-wrap gap-2 print:hidden" data-testid="tab-nav"`) {
		t.Errorf("tab bar must be gap-2 without a border: %.600s", body)
	}
	if !strings.Contains(body, `inline-flex min-h-10 items-center rounded-pill px-4 text-item font-semibold text-ink `) || strings.Contains(body, "border-b-2") {
		t.Errorf("tab must be a min-h-10 px-4 pill without an underline: %.600s", body)
	}
}
