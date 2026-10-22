package pages_test

import (
	"bytes"
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/web/organisms"
	"github.com/ousiassllc/pitha-trador/internal/web/pages"
)

func renderPerformancePage(t *testing.T, props pages.PerformanceProps) string {
	t.Helper()
	var buf bytes.Buffer
	if err := pages.PerformancePage(props).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	return buf.String()
}

// Undefined (nil) ratios render as "—", not as 0 or "<nil>" (issue #360).
func TestPerformancePage_RendersUndefinedActualsAsDash(t *testing.T) {
	body := renderPerformancePage(t, pages.PerformanceProps{Actuals: organisms.PerformanceActuals{}})

	for _, key := range []string{"profit_factor", "sharpe_ref", "sortino_ref"} {
		if want := `data-metric="` + key + `">—<`; !strings.Contains(body, want) {
			t.Errorf("body does not contain %q", want)
		}
	}
	if want := `data-metric="total_pnl">¥0<`; !strings.Contains(body, want) {
		t.Errorf("body does not contain %q", want)
	}
}

func TestPerformancePage_RendersDefinedActualRatios(t *testing.T) {
	pf, sharpe, sortino := 2.0, -0.5, 1.25
	body := renderPerformancePage(t, pages.PerformanceProps{Actuals: organisms.PerformanceActuals{
		ProfitFactor: &pf, SharpeRef: &sharpe, SortinoRef: &sortino,
	}})

	for _, want := range []string{
		`data-metric="profit_factor">2.00<`,
		`data-metric="sharpe_ref">-0.50<`,
		`data-metric="sortino_ref">1.25<`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body does not contain %q", want)
		}
	}
}

// The 実績 tab shows the realized performance and the tab bar; the backtest
// form is on the バックテスト tab only (issue #895).
func TestPerformancePage_TabsSplitActualsFromTheBacktestForm(t *testing.T) {
	actuals := renderPerformancePage(t, pages.PerformanceProps{})
	if !strings.Contains(actuals, `data-testid="performance-actuals"`) || strings.Contains(actuals, `data-testid="backtest-form"`) {
		t.Errorf("the 実績 tab should hold the actuals and not the backtest form")
	}
	if !strings.Contains(actuals, "実績（Paper）") {
		t.Errorf("body should title the section 実績（Paper）")
	}
	backtest := renderPerformancePage(t, pages.PerformanceProps{BacktestTab: true})
	if !strings.Contains(backtest, `data-testid="backtest-form"`) || strings.Contains(backtest, `data-testid="performance-actuals"`) {
		t.Errorf("the バックテスト tab should hold the backtest form and not the actuals")
	}
	for name, body := range map[string]string{"実績": actuals, "バックテスト": backtest} {
		for _, want := range []string{`aria-label="成績のタブ"`, `href="/performance"`, `href="/performance?tab=backtest"`, `href="/calibration"`, ">予測の精度</a>"} {
			if !strings.Contains(body, want) {
				t.Errorf("%s tab lacks %q", name, want)
			}
		}
	}
	if !regexp.MustCompile(`<a[^>]*href="/performance\?tab=backtest"[^>]*aria-current="page"`).MatchString(backtest) {
		t.Errorf("the バックテスト tab is not marked current")
	}
}
