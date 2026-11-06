package atoms_test

import (
	"strings"
	"testing"

	"github.com/a-h/templ"

	"github.com/ousiassllc/pitha-trador/internal/web/atoms"
)

// issue #973: the checkbox is the guide's 18px --r-sm box with a --mute
// outline, never the OS control (design guide §4 "フォーム").
func TestCheckbox_IsTheGuideBoxNotTheOSControl(t *testing.T) {
	body := renderWithLabel(t, atoms.Checkbox(atoms.CheckboxProps{Name: "kinds", Value: "trades", Attrs: templ.Attributes{"checked": true, "data-testid": "cb"}}), "")

	for _, want := range []string{
		`type="checkbox"`, `name="kinds"`, `value="trades"`, ` checked`, `data-testid="cb"`,
		"appearance-none", "h-[18px] w-[18px]", "rounded-sm", "border-mute", "bg-surface",
		"checked:bg-accent", "after:border-bone", "focus-visible:ring-focus/40", "disabled:opacity-50",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("Checkbox lacks %q: %s", want, body)
		}
	}
}

func TestCheckbox_OmitsEmptyNameAndValue(t *testing.T) {
	body := renderWithLabel(t, atoms.Checkbox(atoms.CheckboxProps{}), "")

	for _, bad := range []string{`name=`, `value=`} {
		if strings.Contains(body, bad) {
			t.Errorf("an empty Name/Value must omit %q: %s", bad, body)
		}
	}
}

// The focus border appears within 120ms (brand-guide M1: focus is instant to
// 120ms), so the checkbox must not use the 180ms `duration-base` hover timing
// (issue #997).
func TestCheckbox_FocusBorderTransitionIsAtMostFast(t *testing.T) {
	body := renderWithLabel(t, atoms.Checkbox(atoms.CheckboxProps{}), "")

	if !strings.Contains(body, "transition-[background-color,border-color] duration-fast") {
		t.Errorf("checkbox must transition background/border at duration-fast: %s", body)
	}
	if strings.Contains(body, "duration-base") {
		t.Errorf("duration-base (180ms) would slow the focus border: %s", body)
	}
}
