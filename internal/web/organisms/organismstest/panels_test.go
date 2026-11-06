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

// The KPI values are figures to line up: JetBrains Mono, tabular
// (brand-guide §2, T5), like the home P&L card.
func TestRiskAndJevPanels_FiguresAreMonoTabular(t *testing.T) {
	toxic := 0.5
	for name, html := range map[string]string{
		"risk": renderComponent(t, organisms.RiskPanel(organisms.RiskParams{AllowedPositionPct: 2, StopLossPct: 0.6, TakeProfitPct: 1.2})),
		"jev":  renderComponent(t, organisms.JevPanel(&domain.JevDecision{ToxicFlow: &toxic})),
	} {
		if !strings.Contains(html, `<dd class="text-sm text-ink font-mono tabular-nums">`) {
			t.Errorf("%s panel figures must be font-mono tabular-nums: %s", name, html)
		}
	}
}

// The off-session note leaves the toast and lives in the position panel's
// HelpMark on the 決済 column (brand-guide O3).
func TestPositionPanel_CloseNoteIsAHelpMark(t *testing.T) {
	pos := domain.Position{ID: 1, Symbol: "7203", Side: domain.PositionSideLong, Quantity: 100}
	html := renderComponent(t, organisms.PositionPanel(&pos))
	for _, want := range []string{`id="help-position-close"`, "次の立会で再度お試しください。"} {
		if !strings.Contains(html, want) {
			t.Errorf("position panel lacks %q: %s", want, html)
		}
	}
}
