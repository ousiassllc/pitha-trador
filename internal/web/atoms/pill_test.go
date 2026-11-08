package atoms_test

import (
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/web/atoms"
)

// Status pills and badges are Regular weight (brand-guide §2: SemiBold is for
// headings and buttons only) and fully rounded (§3).
func TestPills_AreRegularWeightAndFullyRounded(t *testing.T) {
	for name, body := range map[string]string{
		"Badge LONG":   renderWithLabel(t, atoms.Badge(atoms.DirectionLong), ""),
		"Badge SHORT":  renderWithLabel(t, atoms.Badge(atoms.DirectionShort), ""),
		"EntryQuality": renderWithLabel(t, atoms.EntryQualityBadge("good"), ""),
		"StatusDot":    renderWithLabel(t, atoms.StatusDot(atoms.StateRunning), ""),
	} {
		if strings.Contains(body, "font-semibold") {
			t.Errorf("%s must not be SemiBold: %s", name, body)
		}
		if !strings.Contains(body, "rounded-full") {
			t.Errorf("%s must be rounded-full: %s", name, body)
		}
	}
}

// issue #1034 (brand-guide C4): the state is told by icon + text + tone, so
// StatusDot and the LONG/SHORT Badge lead with a decorative Iconoir SVG.
func TestStatePills_LeadWithADecorativeIcon(t *testing.T) {
	for name, tc := range map[string]struct {
		body string
		text string
	}{
		"StatusDot running": {renderWithLabel(t, atoms.StatusDot(atoms.StateRunning), ""), "稼働中"},
		"StatusDot paused":  {renderWithLabel(t, atoms.StatusDot(atoms.StatePaused), ""), "新規停止中"},
		"StatusDot killed":  {renderWithLabel(t, atoms.StatusDot(atoms.StateKilled), ""), "全停止（Kill）"},
		"Badge LONG":        {renderWithLabel(t, atoms.Badge(atoms.DirectionLong), ""), "LONG"},
		"Badge SHORT":       {renderWithLabel(t, atoms.Badge(atoms.DirectionShort), ""), "SHORT"},
	} {
		svg := strings.Index(tc.body, "<svg")
		text := strings.Index(tc.body, tc.text)
		if svg < 0 || text < 0 || svg > text {
			t.Errorf("%s must put an icon before its label: %s", name, tc.body)
		}
		for _, want := range []string{`viewBox="0 0 24 24"`, `stroke-width="1.5"`, `stroke="currentColor"`, `aria-hidden="true"`, "icon-sm"} {
			if !strings.Contains(tc.body, want) {
				t.Errorf("%s icon missing %q: %s", name, want, tc.body)
			}
		}
	}
}

// Each StatusDot state gets its own icon shape (not just its own color).
func TestStatusDot_IconDiffersPerState(t *testing.T) {
	seen := map[string]atoms.State{}
	for _, st := range []atoms.State{atoms.StateRunning, atoms.StatePaused, atoms.StateKilled} {
		body := renderWithLabel(t, atoms.StatusDot(st), "")
		start, end := strings.Index(body, "<svg"), strings.Index(body, "</svg>")
		if start < 0 || end < 0 {
			t.Fatalf("%s: no icon: %s", st, body)
		}
		icon := body[start:end]
		if prev, dup := seen[icon]; dup {
			t.Errorf("%s and %s share the same icon", prev, st)
		}
		seen[icon] = st
	}
}
