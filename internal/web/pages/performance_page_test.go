package pages_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/insight"
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
	body := renderPerformancePage(t, pages.PerformanceProps{Actuals: insight.Performance{}})

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
	body := renderPerformancePage(t, pages.PerformanceProps{Actuals: insight.Performance{
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

func TestPerformancePage_ActualsAppearBeforeBacktestForm(t *testing.T) {
	body := renderPerformancePage(t, pages.PerformanceProps{})

	actuals := strings.Index(body, `data-testid="performance-actuals"`)
	form := strings.Index(body, `data-testid="backtest-form"`)
	if actuals < 0 || form < 0 || actuals > form {
		t.Fatalf("actuals at %d, form at %d; want actuals before form", actuals, form)
	}
	if !strings.Contains(body, "実績（Paper）") {
		t.Errorf("body should title the section 実績（Paper）")
	}
}
