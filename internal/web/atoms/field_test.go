package atoms_test

import (
	"context"
	"strings"
	"testing"

	"github.com/a-h/templ"

	"github.com/ousiassllc/pitha-trador/internal/web/atoms"
)

func TestInput_DefaultIsFlexibleRowField(t *testing.T) {
	body := renderWithLabel(t, atoms.Input(atoms.InputProps{ID: "x", Name: "n"}), "")

	for _, want := range []string{`id="x"`, `name="n"`, `type="text"`, "rounded-md border border-border bg-surface disabled:cursor-not-allowed disabled:opacity-50 h-10 px-3 flex-1"} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q: %s", want, body)
		}
	}
}

// A stacked label needs a field that does not grow (no flex-1): Class
// replaces the default layout classes while the shared border stays.
func TestInput_ClassReplacesLayoutButKeepsSharedBorder(t *testing.T) {
	body := renderWithLabel(t, atoms.Input(atoms.InputProps{
		Name: "from", Type: "date", Class: "mt-1 text-sm",
		Attrs: templ.Attributes{"required": true, "min": "1", "data-testid": "f"},
	}), "")

	for _, want := range []string{`type="date"`, "rounded-md border border-border bg-surface disabled:cursor-not-allowed disabled:opacity-50 h-10 px-3 mt-1 text-sm", ` required`, `min="1"`, `data-testid="f"`} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q: %s", want, body)
		}
	}
	if strings.Contains(body, "flex-1") {
		t.Errorf("Class must replace the default flex-1: %s", body)
	}
}

func TestSelect_RendersChildrenWithSharedBorder(t *testing.T) {
	body := renderWithLabel(t, atoms.Select(atoms.SelectProps{
		ID: "s", Name: "days", Attrs: templ.Attributes{"data-testid": "d"},
	}), `<option value="1">one</option>`)

	for _, want := range []string{`<select`, `id="s"`, `name="days"`, "rounded-md border border-border bg-surface disabled:cursor-not-allowed disabled:opacity-50 h-10 px-3", `data-testid="d"`, `<option value="1">one</option></select>`} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q: %s", want, body)
		}
	}
}

// The guide's form control is 40 high with 12 side padding (S2/§4): a caller
// cannot trade it for py-* classes by passing Class.
func TestInputAndSelect_AlwaysCarryTheGuideHeightAndPadding(t *testing.T) {
	for name, body := range map[string]string{
		"Input":  renderWithLabel(t, atoms.Input(atoms.InputProps{}), ""),
		"Select": renderWithLabel(t, atoms.Select(atoms.SelectProps{}), ""),
	} {
		for _, want := range []string{"h-10", "px-3"} {
			if !strings.Contains(body, want) {
				t.Errorf("%s missing %q: %s", name, want, body)
			}
		}
		if strings.Contains(body, "py-") {
			t.Errorf("%s must not carry a vertical padding: %s", name, body)
		}
	}
}

func TestSelect_DefaultClass(t *testing.T) {
	body := renderWithLabel(t, atoms.Select(atoms.SelectProps{Name: "n"}), "")
	if !strings.Contains(body, "rounded-md border border-border bg-surface disabled:cursor-not-allowed disabled:opacity-50 h-10 px-3 focus-visible:") {
		t.Errorf("default class missing: %s", body)
	}
}

// An empty ID/Name must omit the attribute rather than render `id=""`
// (invalid HTML: an id is at least one character).
func TestInputAndSelect_OmitEmptyIDAndName(t *testing.T) {
	for name, body := range map[string]string{
		"Input":  renderWithLabel(t, atoms.Input(atoms.InputProps{}), ""),
		"Select": renderWithLabel(t, atoms.Select(atoms.SelectProps{}), ""),
	} {
		for _, bad := range []string{`id=`, `name=`} {
			if strings.Contains(body, bad) {
				t.Errorf("%s with empty ID/Name must not render %q: %s", name, bad, body)
			}
		}
	}
}

func TestInputAndSelect_RenderIDAndNameWhenGiven(t *testing.T) {
	in := renderWithLabel(t, atoms.Input(atoms.InputProps{ID: "i", Name: "n"}), "")
	sel := renderWithLabel(t, atoms.Select(atoms.SelectProps{ID: "i", Name: "n"}), "")
	for name, body := range map[string]string{"Input": in, "Select": sel} {
		for _, want := range []string{`id="i"`, `name="n"`} {
			if !strings.Contains(body, want) {
				t.Errorf("%s body missing %q: %s", name, want, body)
			}
		}
	}
}

func TestSwitch_IsACheckboxSwitchWithGuideTrackAndKnob(t *testing.T) {
	body := renderWithLabel(t, atoms.Switch(atoms.SwitchProps{Name: "enabled", Attrs: templ.Attributes{"checked": true, "data-testid": "sw"}}), "有効にする")
	for _, want := range []string{
		`type="checkbox"`, `role="switch"`, `name="enabled"`, `value="on"`, ` checked`, `data-testid="sw"`,
		"peer sr-only", "h-5 w-9 shrink-0 rounded-full", "after:h-3.5 after:w-3.5", "peer-checked:bg-accent", "peer-checked:after:bg-bone",
		"peer-focus-visible:ring-focus/40", "bg-accent-soft", "text-[15px]", "有効にする",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("Switch lacks %q: %s", want, body)
		}
	}
	if strings.Contains(body, "font-medium") || strings.Contains(body, "font-semibold") {
		t.Errorf("the switch label must be Regular: %s", body)
	}
	// Disabled form parts are opacity .5 (brand-guide §4), the label with the track (issue #961).
	if n := strings.Count(body, "peer-disabled:opacity-50"); n != 2 || strings.Contains(body, "opacity-40") {
		t.Errorf("track and label should both be peer-disabled:opacity-50: %s", body)
	}
	// Tailwind's default easing differs from the guide's --ease-out (brand-guide M1, issue #1016).
	for _, want := range []string{"transition-colors duration-base ease-out", "after:transition-transform after:duration-base after:ease-out"} {
		if !strings.Contains(body, want) {
			t.Errorf("Switch track and knob must ease with --ease-out, lacks %q: %s", want, body)
		}
	}
}

func TestInlineAlert_HasIconHeadingAndBodyInTheDangerTone(t *testing.T) {
	var sb strings.Builder
	if err := atoms.InlineAlert("入力を確認してください", "数値で入力してください").Render(context.Background(), &sb); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`role="alert"`, "data-inline-alert", "tone-danger", "<svg", `aria-hidden="true"`, "入力を確認してください", "数値で入力してください"} {
		if !strings.Contains(sb.String(), want) {
			t.Errorf("InlineAlert lacks %q: %s", want, sb.String())
		}
	}
}

// Input and Select draw the guide's focus state (brand-guide C3): --focus
// border and a 2px --ring, instead of the UA's default ring.
func TestInputAndSelect_DrawFocusRing(t *testing.T) {
	for name, body := range map[string]string{
		"Input":  renderWithLabel(t, atoms.Input(atoms.InputProps{Name: "n"}), ""),
		"Select": renderWithLabel(t, atoms.Select(atoms.SelectProps{Name: "n"}), ""),
	} {
		for _, want := range []string{"focus-visible:border-focus", "focus-visible:outline-none", "focus-visible:ring-2", "focus-visible:ring-focus/40"} {
			if !strings.Contains(body, want) {
				t.Errorf("%s missing %q: %s", name, want, body)
			}
		}
	}
}

func TestTextarea_RendersChildrenWithSharedBorderAndFocusRing(t *testing.T) {
	body := renderWithLabel(t, atoms.Textarea(atoms.TextareaProps{
		Name: "memo", Attrs: templ.Attributes{"rows": "2", "required": true, "data-testid": "t"},
	}), "text")

	for _, want := range []string{
		`<textarea`, `name="memo"`, `rows="2"`, ` required`, `data-testid="t"`, "rounded-md border border-border bg-surface disabled:cursor-not-allowed disabled:opacity-50 p-3",
		"focus-visible:border-focus", "focus-visible:outline-none", "focus-visible:ring-2", "focus-visible:ring-focus/40", `>text</textarea>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("Textarea lacks %q: %s", want, body)
		}
	}
	if strings.Contains(body, ` id=`) {
		t.Errorf("an empty ID must not render id=: %s", body)
	}
	// brand-guide S2/O8: padding 12 on every side (issue #1027/#1029); neither py-1 nor py-2.5.
	if !strings.Contains(body, ` p-3`) || strings.Contains(body, "py-1") || strings.Contains(body, "py-2.5") {
		t.Errorf("Textarea padding must be exactly p-3: %s", body)
	}
}

// The alert's exit is app.css's `.htmx-swapping > [data-inline-alert]`: the
// marker attribute that rule (and pitha-htmx-errors) key on must stay on the
// alert's root, and the entrance animation stays `alert-in`.
func TestInlineAlert_RootCarriesTheMarkerTheExitAnimationKeysOn(t *testing.T) {
	var sb strings.Builder
	if err := atoms.InlineAlert("入力を確認してください", "数値で入力してください").Render(context.Background(), &sb); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(sb.String(), `<div class="alert-in `) || !strings.Contains(strings.SplitN(sb.String(), ">", 2)[0], " data-inline-alert") {
		t.Errorf("the alert root must be a div with alert-in and data-inline-alert: %s", sb.String())
	}
}
