package organisms_test

import (
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/web/organisms"
)

func TestBacktestForm_EchoesValuesThroughSharedFieldStyle(t *testing.T) {
	html := renderComponent(t, organisms.BacktestForm(organisms.BacktestFormValues{
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
