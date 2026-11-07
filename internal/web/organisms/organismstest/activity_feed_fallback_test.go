package organismstest

import (
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/web/organisms"
)

// The symbol column is a code, so it is JetBrains Mono like the other
// tables (brand-guide T5); static/src/components/activity-feed keeps the
// same class so SSR and Lit match.
func TestActivityFeedFallbackSymbolCellIsMono(t *testing.T) {
	html := renderComponent(t, organisms.ActivityFeedFallback(
		[]domain.ActivityEvent{{Type: domain.ActivityTypeJob, Symbol: "7203"}},
		time.Date(2026, 10, 5, 0, 30, 0, 0, time.UTC),
	))
	if !strings.Contains(html, `<td class="px-3 py-2.5 font-mono">7203</td>`) {
		t.Errorf("symbol cell is not font-mono: %s", html)
	}
}
