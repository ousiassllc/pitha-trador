package organismstest

import (
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/web/organisms/evaluation"
)

// issue #991 (S2): the h2 is followed by 24px (mb-6) and what follows has no top margin of its own.
func TestEvaluationPanel_H2HasTheH2GapBelow(t *testing.T) {
	for name, html := range map[string]string{
		"error":   renderComponent(t, evaluation.EvaluationPanel(evaluation.Panel{}, "読み取れませんでした")),
		"capital": renderComponent(t, evaluation.EvaluationPanel(evaluation.Panel{Capital: "元手 100万円"}, "")),
	} {
		if !strings.Contains(html, `<h2 id="evaluation-heading" class="mb-6 text-xl`) {
			t.Errorf("%s: h2 must be mb-6: %s", name, html)
		}
		if strings.Contains(html, `class="mt-4 rounded-md border tone-danger`) || strings.Contains(html, `class="mt-2 text-xs text-mute" data-testid="evaluation-capital"`) {
			t.Errorf("%s: body keeps its own top margin: %s", name, html)
		}
	}
}
