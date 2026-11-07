package molecules_test

import (
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/web/molecules"
)

// Guide M3: the colour transition is dropped under prefers-reduced-motion
// (issue #1032), like checkbox / switch.
func TestThemeToggle_DropsTransitionUnderReducedMotion(t *testing.T) {
	html := render(t, molecules.ThemeToggle())
	if !strings.Contains(html, "motion-reduce:transition-none") {
		t.Errorf("ThemeToggle must carry motion-reduce:transition-none: %s", html)
	}
}
