package scanner_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// issue #408: a reason with 0 symbols is normally hidden from the select,
// but while it is the applied filter it must stay as the selected option, or
// the UI would read "すべて" while the result stays narrowed.
func TestScannerHandler_ScanView_AppliedReasonStaysSelectedAtZeroCount(t *testing.T) {
	src := scanSource{cycle: scanFixture(), ok: true}
	zero := domain.ScreenReasonMaxPrice // absent from scanFixture
	if scanFixture().Summary().ByReason[zero] != 0 {
		t.Fatal("fixture must have no symbols for the zero-count reason")
	}

	_, body := getScan(t, src, "/scanner/scan?reason="+zero.Code(), true)
	selected := regexp.MustCompile(`<option value="` + zero.Code() + `" selected[^>]*>[^<]*（0）</option>`)
	if !selected.MatchString(body) {
		t.Errorf("applied zero-count reason must render as a selected option with （0）:\n%s", body)
	}
	if strings.Contains(body, `<option value="min_price" selected`) {
		t.Error("only the applied reason may be selected")
	}
	if strings.Contains(body, `value="`+domain.ScreenReasonMinTurnover.Code()+`"`) {
		t.Error("other zero-count reasons must stay hidden")
	}

	_, body = getScan(t, src, "/scanner/scan", true)
	if strings.Contains(body, `value="`+zero.Code()+`"`) {
		t.Error("zero-count reason must stay hidden when not applied")
	}
}

// issue #410: one user action must produce one request, and focus must
// survive the #scan-panel swap. Enter in the search box fires only submit
// (no change), the selects fire change, and every control has an id so htmx
// can restore focus after the swap. The trigger uses `from:#id` rather than
// an event filter because htmx runs with allowEval:false.
func TestScannerHandler_ScanView_FilterFormTriggersOncePerActionAndKeepsFocusableIDs(t *testing.T) {
	_, body := getScan(t, scanSource{cycle: scanFixture(), ok: true}, "/scanner/scan", true)
	for _, want := range []string{
		`hx-trigger="submit, change from:#scan-status, change from:#scan-reason"`,
		`id="scan-q"`, `id="scan-status" name="status"`, `id="scan-reason" name="reason"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("filter form missing %q", want)
		}
	}
	if strings.Contains(body, `hx-trigger="change, submit"`) {
		t.Error("form-level change trigger fires on the search box blur and duplicates submit")
	}
}
