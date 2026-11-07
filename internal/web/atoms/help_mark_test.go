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

	for _, want := range []string{"rounded-sm", "px-[10px] py-1.5"} {
		if !strings.Contains(body, want) {
			t.Errorf("tooltip missing %q: %s", want, body)
		}
	}
	if strings.Contains(body, "p-2 text-left") || strings.Contains(body, "px-3 py-2 ") {
		t.Errorf("tooltip keeps the old 8px padding: %s", body)
	}
}

// The tooltip enters over 320ms ease-out and leaves over 240ms ease-in
// (brand-guide M5/§5): the closed state carries the exit transition and
// [data-open] the entrance one.
func TestHelpMark_TooltipFadesOutFasterThanItFadesIn(t *testing.T) {
	body := renderWithLabel(t, atoms.HelpMark(atoms.HelpMarkProps{ID: "help-x", Label: "説明"}), "詳しい説明")

	for _, want := range []string{
		"transition-[opacity,visibility] duration-fade-out ease-in",
		"data-[open]:duration-fade-in data-[open]:ease-out",
		"motion-reduce:duration-100 motion-reduce:data-[open]:duration-100",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("tooltip missing %q: %s", want, body)
		}
	}
	if strings.Contains(body, " duration-fade-in ease-out ") {
		t.Errorf("the closed state must not carry the entrance transition: %s", body)
	}
}
