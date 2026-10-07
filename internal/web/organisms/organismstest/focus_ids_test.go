package organismstest

import (
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/web/organisms"
)

// htmx restores focus after a swap only to a same-id element (issue #676),
// so every button a swap replaces carries a stable id in the fragment the
// handlers return (the same templ components render the first paint and
// the swap response).

func assertContains(t *testing.T, html string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(html, want) {
			t.Errorf("missing %q: %s", want, html)
		}
	}
}

func TestScanPanel_ClosedButtonsHaveStableIDs(t *testing.T) {
	html := renderComponent(t, organisms.ScanPanel(organisms.ScanPanelView{}))
	// scan-open vanishes once the list opens: focus moves to the list heading.
	assertContains(t, html, `id="scan-open"`, `data-focus-after-swap="scan-list-heading"`, `id="scan-refresh"`)
	if strings.Contains(html, `id="scan-list-heading"`) {
		t.Error("the list heading belongs to the opened panel only")
	}
}

func TestScanPanel_OpenControlsHaveStableIDs(t *testing.T) {
	v := organisms.ScanPanelView{
		HasCycle: true,
		Open:     true,
		Page:     domain.ScanPage{Page: 2, Pages: 3, Total: 120},
	}
	html := renderComponent(t, organisms.ScanPanel(v))
	assertContains(t, html,
		`id="scan-refresh"`, `id="scan-filter-submit"`, `id="scan-prev"`, `id="scan-next"`,
		`id="scan-list-heading"`, `tabindex="-1"`, `id="scan-total"`,
		`data-focus-after-swap="scan-total"`)
	if strings.Contains(html, `id="scan-open"`) {
		t.Error("scan-open is rendered only while the list is closed")
	}
}

func TestScanPanel_UniverseImportButtonHasStableIDAndFocusTarget(t *testing.T) {
	empty := renderComponent(t, organisms.ScanPanel(organisms.ScanPanelView{UniverseEmpty: true}))
	assertContains(t, empty, `id="scan-universe-import"`, `data-focus-after-swap="scan-universe-imported"`)

	imported := renderComponent(t, organisms.ScanPanel(organisms.ScanPanelView{UniverseImported: 3707}))
	assertContains(t, imported, `id="scan-universe-imported"`, `tabindex="-1"`)
}

func TestUpdatePanel_CheckButtonHasStableID(t *testing.T) {
	html := renderComponent(t, organisms.UpdatePanel(organisms.UpdatePanelProps{CurrentVersion: "1.0.0"}))
	assertContains(t, html, `id="update-check-button"`)
}
