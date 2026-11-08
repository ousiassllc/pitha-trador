package layout_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/a-h/templ"
	"github.com/ousiassllc/pitha-trador/internal/web/layout"
	"github.com/ousiassllc/pitha-trador/internal/web/organisms"
)

// PR #1044 M5: the persistent reload notice needs a page slot that is empty
// (so it takes no space), sits before the header and `<main>` (first in Tab
// order, and sticky so it stays on screen while scrolling), and is not a live
// region (the toast announces; a second one would read the fact twice). The
// template pitha-htmx-errors clones must be in both shells.
func TestShells_CarryTheReloadNoticeSlot(t *testing.T) {
	slotRe := regexp.MustCompile(`<div id="reload-notice-region" class="([^"]*)"></div>`)
	for name, page := range map[string]templ.Component{
		"Shell":      layout.Shell("t", organisms.NavNone),
		"SetupShell": layout.SetupShell("t"),
	} {
		out := render(t, page)
		m := slotRe.FindStringSubmatchIndex(out)
		if m == nil {
			t.Fatalf("%s: no empty #reload-notice-region in %s", name, out)
		}
		class := out[m[2]:m[3]]
		for _, want := range []string{"sticky", "top-0", "empty:hidden", "print:hidden"} {
			if !strings.Contains(class, want) {
				t.Errorf("%s: slot class %q lacks %q", name, class, want)
			}
		}
		if main := strings.Index(out, "<main"); m[0] > main {
			t.Errorf("%s: slot must precede <main>", name)
		}
		if !strings.Contains(out, `<template id="reload-notice-template">`) || !strings.Contains(out, "data-reload-notice-reload") {
			t.Errorf("%s: no reload-notice template with its action: %s", name, out)
		}
		if strings.Contains(out[m[0]:m[1]], "aria-live") {
			t.Errorf("%s: slot must not be a live region", name)
		}
	}
	if out := render(t, layout.Shell("t", organisms.NavNone)); strings.Index(out, `id="reload-notice-region"`) > strings.Index(out, "<header") {
		t.Errorf("Shell: slot must precede the header")
	}
}
