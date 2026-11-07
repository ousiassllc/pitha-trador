package molecules_test

import (
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/web/molecules"
)

// issue #1035 (O3): a modal <dialog> makes #toast-region inert, so the shared
// confirmation dialog carries its own toast region (hidden when printing) just
// like molecules.Modal.
func TestConfirmDialog_HasItsOwnToastRegion(t *testing.T) {
	html := render(t, molecules.ConfirmDialog())
	if got := strings.Count(html, "data-toast-region"); got != 1 {
		t.Fatalf("ConfirmDialog has %d data-toast-region elements, want 1: %s", got, html)
	}
	start := strings.Index(html, "data-toast-region")
	tag := html[start : start+strings.Index(html[start:], ">")]
	if !strings.Contains(tag, "print:hidden") {
		t.Errorf("toast region lacks print:hidden: %s", tag)
	}
	if start > strings.Index(html, "</dialog>") {
		t.Errorf("toast region must sit inside the <dialog>: %s", html)
	}
}
