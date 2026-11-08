package molecules_test

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/a-h/templ"
	"github.com/ousiassllc/pitha-trador/internal/web/molecules"
)

// PR #1044 M5: a modal makes the page inert, so Modal carries a slot for the
// persistent reload notice, between the title bar (its 「閉じる」 stays the first
// Tab stop) and the body, empty and hidden when printing.
func TestModal_HasAReloadNoticeSlotBetweenTitleBarAndBody(t *testing.T) {
	var sb strings.Builder
	ctx := templ.WithChildren(context.Background(), templ.Raw("<p>BODY</p>"))
	if err := molecules.Modal("m", "T").Render(ctx, &sb); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	html := sb.String()

	if got := strings.Count(html, "data-reload-notice-region"); got != 1 {
		t.Fatalf("Modal has %d data-reload-notice-region elements, want 1: %s", got, html)
	}
	slot := regexp.MustCompile(`<div data-reload-notice-region class="([^"]*)"></div>`).FindStringSubmatchIndex(html)
	if slot == nil {
		t.Fatalf("slot must be an empty div: %s", html)
	}
	if class := html[slot[2]:slot[3]]; !strings.Contains(class, "empty:hidden") || !strings.Contains(class, "print:hidden") {
		t.Errorf("slot class %q lacks empty:hidden / print:hidden", class)
	}
	if close, body := strings.Index(html, "data-modal-close"), strings.Index(html, "BODY"); close >= slot[0] || slot[0] >= body {
		t.Errorf("slot must sit between the 閉じる button (%d) and the body (%d), got %d", close, body, slot[0])
	}
}
