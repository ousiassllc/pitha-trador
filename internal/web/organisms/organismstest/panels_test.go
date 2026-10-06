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
	if !strings.Contains(html, `id="position-panel"`) || !strings.Contains(html, "No open position.") {
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
