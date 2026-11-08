package molecules_test

import (
	"regexp"
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

// PR #1044 M5: a modal makes the page's reload-notice slot inert, so the
// confirmation dialog carries its own (hidden when printing, empty until the
// notice moves in) for the persistent reload notice, like molecules.Modal.
func TestConfirmDialog_HasItsOwnReloadNoticeSlot(t *testing.T) {
	html := render(t, molecules.ConfirmDialog())
	if got := strings.Count(html, "data-reload-notice-region"); got != 1 {
		t.Fatalf("ConfirmDialog has %d data-reload-notice-region elements, want 1: %s", got, html)
	}
	slot := regexp.MustCompile(`<div data-reload-notice-region class="([^"]*)"></div>`).FindStringSubmatchIndex(html)
	if slot == nil {
		t.Fatalf("slot must be an empty div: %s", html)
	}
	if class := html[slot[2]:slot[3]]; !strings.Contains(class, "empty:hidden") || !strings.Contains(class, "print:hidden") {
		t.Errorf("slot class %q lacks empty:hidden / print:hidden", class)
	}
	if slot[0] > strings.Index(html, "</dialog>") || slot[0] < strings.Index(html, "<dialog") {
		t.Errorf("slot must sit inside the <dialog>: %s", html)
	}
}
