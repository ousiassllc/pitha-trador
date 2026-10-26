package performancepanels_test

import (
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/web/organisms/performancepanels"
)

func TestBacktestFoldTable_RendersInclusiveForwardPeriod(t *testing.T) {
	html := renderComponent(t, performancepanels.BacktestFoldTable([]performancepanels.BacktestFold{{
		ForwardStart: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		ForwardEnd:   time.Date(2026, 1, 8, 0, 0, 0, 0, time.UTC),
		Forward:      performancepanels.PerformanceSummary{TradeCount: 7, WinRate: 0.5, NetPnLPct: 1.5},
	}}))
	for _, want := range []string{`data-testid="backtest-folds"`, "2026-01-01 – 2026-01-07", "50.0%", "1.50%"} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %q: %s", want, html)
		}
	}
}

// Each metric's explanation is in a help mark beside its name, not in a
// `title` tooltip or a development note; a failed read stays on screen.
func TestPerformanceActualsPanel_ExplainsMetricsInHelpMarks(t *testing.T) {
	html := renderComponent(t, performancepanels.PerformanceActualsPanel(performancepanels.PerformanceActuals{TradeCount: 3}, ""))
	for _, want := range []string{
		`data-metric="win_rate"`, `data-help-mark`,
		`aria-label="勝率の説明"`, `id="help-performance-actuals-win_rate"`, "利益が出た取引の割合です。",
		"年率換算はしていません",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %q: %s", want, html)
		}
	}
	if strings.Contains(html, "title=") {
		t.Errorf("metric names must not be explained through title: %s", html)
	}
	if n := strings.Count(html, `id="help-performance-actuals`); n != 12 {
		t.Errorf("help marks = %d, want 12 (panel + 11 metrics)", n)
	}

	failed := renderComponent(t, performancepanels.PerformanceActualsPanel(performancepanels.PerformanceActuals{}, "実績の取得に失敗しました。"))
	if !strings.Contains(failed, `data-testid="actuals-error"`) || !strings.Contains(failed, "実績の取得に失敗しました。") || strings.Contains(failed, "<dl") {
		t.Errorf("a failed read must show the error instead of the metrics: %s", failed)
	}
}

func TestPerformanceSummaryPanel_ExplainsMetricsInHelpMarks(t *testing.T) {
	html := renderComponent(t, performancepanels.PerformanceSummaryPanel("Walk Forward", performancepanels.PerformanceSummary{TradeCount: 4}))
	for _, want := range []string{`data-metric="net_pnl_pct"`, `aria-label="純損益の説明"`, "コストと手数料をすべて引いた後のリターンの合計"} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %q: %s", want, html)
		}
	}
	if strings.Contains(html, "title=") {
		t.Errorf("metric names must not be explained through title: %s", html)
	}
}
