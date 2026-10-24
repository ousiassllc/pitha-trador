package organismstest

import (
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/web/organisms"
)

func TestRiskPanel_ShowsThreeFigures(t *testing.T) {
	html := renderComponent(t, organisms.RiskPanel(organisms.RiskParams{AllowedPositionPct: 2, StopLossPct: 0.6, TakeProfitPct: 1.2}))
	for _, want := range []string{`id="risk-panel"`, `data-testid="risk-panel"`, "2.00%", "0.60%", "1.20%"} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %q: %s", want, html)
		}
	}
}

func TestJevPanel_NilAndDecision(t *testing.T) {
	empty := renderComponent(t, organisms.JevPanel(nil))
	if !strings.Contains(empty, `id="jev-panel"`) || strings.Contains(empty, "<dl") {
		t.Errorf("nil decision must render the panel without details: %s", empty)
	}

	regime, toxic := "trend", 0.5
	d := &domain.JevDecision{Regime: &regime, ToxicFlow: &toxic}
	html := renderComponent(t, organisms.JevPanel(d))
	for _, want := range []string{"trend", "0.50", "—"} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %q: %s", want, html)
		}
	}
}

func TestPositionPanel_NilShowsNotice(t *testing.T) {
	html := renderComponent(t, organisms.PositionPanel(nil))
	if !strings.Contains(html, `id="position-panel"`) || !strings.Contains(html, "保有中のポジションはありません。") {
		t.Errorf("unexpected: %s", html)
	}
	if strings.Contains(html, "<table") {
		t.Errorf("nil position must not render a table: %s", html)
	}
}

func TestPositionPanel_RendersRow(t *testing.T) {
	pos := &domain.Position{ID: 1, Symbol: "7203", Side: domain.PositionSideLong, Quantity: 100, EntryPrice: 100, CurrentPrice: 101, OpenedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}
	html := renderComponent(t, organisms.PositionPanel(pos))
	if !strings.Contains(html, `data-testid="position-row"`) {
		t.Errorf("position row missing: %s", html)
	}
}

func TestBacktestFoldTable_RendersInclusiveForwardPeriod(t *testing.T) {
	html := renderComponent(t, organisms.BacktestFoldTable([]organisms.BacktestFold{{
		ForwardStart: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		ForwardEnd:   time.Date(2026, 1, 8, 0, 0, 0, 0, time.UTC),
		Forward:      organisms.PerformanceSummary{TradeCount: 7, WinRate: 0.5, NetPnLPct: 1.5},
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
	html := renderComponent(t, organisms.PerformanceActualsPanel(organisms.PerformanceActuals{TradeCount: 3}, ""))
	for _, want := range []string{
		`data-metric="win_rate"`, `data-help-mark`,
		`aria-label="勝率の説明"`, `id="help-performance-actuals-win_rate"`, "Win Rate。利益が出た取引の割合です。",
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

	failed := renderComponent(t, organisms.PerformanceActualsPanel(organisms.PerformanceActuals{}, "実績の取得に失敗しました。"))
	if !strings.Contains(failed, `data-testid="actuals-error"`) || !strings.Contains(failed, "実績の取得に失敗しました。") || strings.Contains(failed, "<dl") {
		t.Errorf("a failed read must show the error instead of the metrics: %s", failed)
	}
}

func TestPerformanceSummaryPanel_ExplainsMetricsInHelpMarks(t *testing.T) {
	html := renderComponent(t, organisms.PerformanceSummaryPanel("Walk Forward", organisms.PerformanceSummary{TradeCount: 4}))
	for _, want := range []string{`data-metric="net_pnl_pct"`, `aria-label="純損益の説明"`, "Net PnL。コストと手数料をすべて引いた後のリターンの合計"} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %q: %s", want, html)
		}
	}
	if strings.Contains(html, "title=") {
		t.Errorf("metric names must not be explained through title: %s", html)
	}
}
