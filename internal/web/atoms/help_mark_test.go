package atoms_test

import (
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/web/atoms"
)

func TestHelpMark_ButtonDescribesItselfWithTheHelpText(t *testing.T) {
	body := renderWithLabel(t, atoms.HelpMark(atoms.HelpMarkProps{ID: "help-x", Label: "新規エントリーの説明"}), "詳しい説明")

	for _, want := range []string{
		`type="button"`,
		`aria-label="新規エントリーの説明"`,
		`aria-describedby="help-x"`,
		`id="help-x"`,
		`role="tooltip"`,
		"詳しい説明",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q: %s", want, body)
		}
	}
}

// The tooltip is --r-sm (4px) with 6x10 padding (brand-guide §3).
func TestHelpMark_TooltipUsesTheGuideShape(t *testing.T) {
	body := renderWithLabel(t, atoms.HelpMark(atoms.HelpMarkProps{ID: "help-x", Label: "説明"}), "詳しい説明")

	for _, want := range []string{"rounded-sm", "px-2.5 py-1.5"} {
		if !strings.Contains(body, want) {
			t.Errorf("tooltip missing %q: %s", want, body)
		}
	}
	if strings.Contains(body, "p-2 text-left") {
		t.Errorf("tooltip keeps the old 8px padding: %s", body)
	}
}
