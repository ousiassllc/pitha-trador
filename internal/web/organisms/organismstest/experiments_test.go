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
