package performancepanels_test

import (
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/web/organisms/performancepanels"
)

func TestBacktestForm_EchoesValuesThroughSharedFieldStyle(t *testing.T) {
	html := renderComponent(t, performancepanels.BacktestForm(performancepanels.BacktestFormValues{
		From: "2026-01-01", To: "2026-03-31", TrainingDays: 60, ValidationDays: 20, ForwardDays: 10,
	}))

	for _, want := range []string{
		`method="get" action="/performance"`,
		`type="date" name="from"`, `value="2026-01-01"`, `value="2026-03-31"`,
		`name="training_days"`, `value="60"`, `name="validation_days"`, `value="20"`, `name="forward_days"`, `value="10"`,
		`min="1"`, " required",
		"rounded-md border border-slate-300 mt-1 px-3 py-1",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %q in %s", want, html)
		}
	}
	if strings.Contains(html, "flex-1") {
		t.Errorf("stacked-label fields must not grow (flex-1): %s", html)
	}
}

// The cost sensitivity export is a second submit button of the same form, so
// the CSV is made from the conditions currently in the form (issue #779).
func TestBacktestForm_CostSensitivityExportSubmitsTheSameFields(t *testing.T) {
	html := renderComponent(t, performancepanels.BacktestForm(performancepanels.BacktestFormValues{
		From: "2026-01-01", To: "2026-03-31", TrainingDays: 60, ValidationDays: 20, ForwardDays: 10,
	}))

	formEnd := strings.Index(html, "</form>")
	button := strings.Index(html, `formaction="/performance/cost-sensitivity"`)
	if button < 0 || button > formEnd {
		t.Fatalf("the export button must live inside the form and post to the export: %s", html)
	}
	for _, want := range []string{`name="format"`, `value="csv"`, `data-testid="cost-sensitivity-export"`, `type="submit"`} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %q in %s", want, html)
		}
	}
}

// The explanation of a field is a help mark next to its name, a sibling of
// the <label> (a mark inside a label would make its text part of the name).
func TestBacktestForm_ExplainsFieldsInHelpMarksOutsideTheLabel(t *testing.T) {
	html := renderComponent(t, performancepanels.BacktestForm(performancepanels.BacktestFormValues{From: "2026-01-01", To: "2026-03-31", TrainingDays: 60, ValidationDays: 20, ForwardDays: 10}))

	for _, want := range []string{
		`<label for="backtest-forward-days">Forward 日数</label>`,
		`id="backtest-forward-days"`,
		`aria-label="Forward 日数の説明"`, `id="help-backtest-forward-days"`, "成績として評価する期間",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %q: %s", want, html)
		}
	}
}
