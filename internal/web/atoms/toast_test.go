package atoms_test

import (
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/web/atoms"
)

// issue #1145: the toast floats over the page, so it carries the same 1px
// --line-soft frame as the modal and the tooltip.
func TestToast_FrameIsLineSoft(t *testing.T) {
	body := renderWithLabel(t, atoms.Toast("失敗しました"), "")

	if !strings.Contains(body, "border border-line-soft") {
		t.Errorf("toast must carry `border border-line-soft`: %s", body)
	}
}
