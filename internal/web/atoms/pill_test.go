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
