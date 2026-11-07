package molecules_test

import (
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/web/molecules"
)

// issue #1023 (T1/T5): the symbol code is Mono, like the other tables' code columns.
func TestPositionRow_SymbolCellIsMono(t *testing.T) {
	html := render(t, molecules.PositionRow(domain.Position{ID: 1, Symbol: "7203", Side: domain.PositionSideLong}))
	if !strings.Contains(html, `<td class="px-3 py-2.5 font-mono text-ink">7203</td>`) {
		t.Errorf("symbol cell must be font-mono: %s", html)
	}
}
