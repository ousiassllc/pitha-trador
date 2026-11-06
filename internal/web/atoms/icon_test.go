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
		for _, want := range []string{`viewBox="0 0 24 24"`, `stroke-width="1.5"`, `stroke="currentColor"`, `aria-hidden="true"`, "h-4 w-4"} {
			if !strings.Contains(body, want) {
				t.Errorf("%s missing %q: %s", name, want, body)
			}
		}
	}
}
