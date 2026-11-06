package atoms_test

import (
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/web/atoms"
)

// Link arrows are Iconoir SVGs (brand-guide §6): 24 viewBox, stroke 1.5,
// currentColor, 16px, decorative.
func TestLinkArrowIcons_AreDecorativeIconoirSVGs(t *testing.T) {
	for name, body := range map[string]string{
		"arrow-right":    renderWithLabel(t, atoms.IconArrowRight(), ""),
		"nav-arrow-left": renderWithLabel(t, atoms.IconNavArrowLeft(), ""),
	} {
		for _, want := range []string{`viewBox="0 0 24 24"`, `stroke-width="1.5"`, `stroke="currentColor"`, `aria-hidden="true"`, "icon-sm"} {
			if !strings.Contains(body, want) {
				t.Errorf("%s missing %q: %s", name, want, body)
			}
		}
	}
}

// issue #974: the inline-alert icon is nudged by a 4px-scale margin (brand-guide §3), not 2px.
func TestIconWarningCircle_UsesTheSpacingScale(t *testing.T) {
	body := renderWithLabel(t, atoms.IconWarningCircle(), "")
	if !strings.Contains(body, `class="mt-1 icon-md shrink-0"`) || strings.Contains(body, "mt-0.5") {
		t.Errorf("the warning icon offset must be mt-1: %s", body)
	}
}
