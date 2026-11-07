package organismstest

import (
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/web/organisms/mcp"
)

// issue #991 (S1): the before/after comparison is a panel nested in the MCP card, so it is --bone on --r-md.
func TestExperimentDetail_IsANestedMediumRadiusPanel(t *testing.T) {
	html := renderComponent(t, mcp.ExperimentDetail(mcp.ExperimentDetailProps{ID: 7}))
	if !strings.Contains(html, `class="flex flex-col gap-2 rounded-md bg-bone p-4" data-testid="mcp-experiment-comparison"`) {
		t.Errorf("the comparison panel must be rounded-md: %s", html)
	}
}

// issue #995 (T5): the experiment ID is a figure to line up, so its column is JetBrains Mono, tabular.
func TestExperiments_IDColumnIsMonoTabular(t *testing.T) {
	html := renderComponent(t, mcp.Experiments(mcp.ExperimentsProps{Rows: []mcp.ExperimentRow{{ID: 12, Status: "closed", StatusLabel: "終了"}}}))
	if !strings.Contains(html, `<td class="px-2 py-1.5 font-mono tabular-nums">12</td>`) {
		t.Errorf("the experiment ID cell must be font-mono tabular-nums: %s", html)
	}
}
