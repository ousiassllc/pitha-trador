package organismstest

import (
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/web/organisms/mcp"
)

// issue #974: the before/after comparison is a --bone card on --r-lg, like the experiments section around it.
func TestExperimentDetail_IsALargeRadiusCard(t *testing.T) {
	html := renderComponent(t, mcp.ExperimentDetail(mcp.ExperimentDetailProps{ID: 7}))
	if !strings.Contains(html, `class="flex flex-col gap-2 rounded-lg bg-bone p-4" data-testid="mcp-experiment-comparison"`) {
		t.Errorf("the comparison panel must be rounded-lg: %s", html)
	}
}
