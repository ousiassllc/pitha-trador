package atoms_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/web/atoms"
)

var toastClassAttr = regexp.MustCompile(`class="([^"]*)"[^>]*data-testid="toast"`)

// issue #1145: the toast is an inverted surface painted with `bg-ink`, so it
// carries no frame, unlike the modal and the tooltip that share the page's tone.
func TestToast_HasNoFrame(t *testing.T) {
	body := renderWithLabel(t, atoms.Toast("失敗しました"), "")

	m := toastClassAttr.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("toast root not found: %s", body)
	}
	for _, class := range strings.Fields(m[1]) {
		if class == "border" || strings.HasPrefix(class, "border-") {
			t.Errorf("toast root must have no frame, found class %q: %s", class, m[1])
		}
	}
}
