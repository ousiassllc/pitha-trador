package atoms_test

import (
	"context"
	"strings"
	"testing"

	"github.com/a-h/templ"

	"github.com/ousiassllc/pitha-trador/internal/web/atoms"
)

func renderWithLabel(t *testing.T, c templ.Component, label string) string {
	t.Helper()
	ctx := templ.WithChildren(context.Background(), templ.Raw(label))
	var sb strings.Builder
	if err := c.Render(ctx, &sb); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	return sb.String()
}

func TestButton_DefaultIsPrimaryTypeButton(t *testing.T) {
	body := renderWithLabel(t, atoms.Button(atoms.ButtonProps{}), "保存")

	for _, want := range []string{`type="button"`, "bg-accent text-bone", "disabled:opacity-50", "保存"} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q: %s", want, body)
		}
	}
}

func TestButton_VariantsAndSizeChangeClasses(t *testing.T) {
	tests := []struct {
		name  string
		props atoms.ButtonProps
		want  string
		not   string
	}{
		{"danger", atoms.ButtonProps{Variant: atoms.ButtonDanger}, "bg-danger text-bone", "bg-accent text-bone"},
		{"secondary", atoms.ButtonProps{Variant: atoms.ButtonSecondary}, "border-border", "bg-accent text-bone"},
		{"outline", atoms.ButtonProps{Variant: atoms.ButtonOutline}, "hover:bg-accent-soft", "bg-accent text-bone"},
		{"danger outline", atoms.ButtonProps{Variant: atoms.ButtonDangerOutline}, "border-danger/40", "bg-danger text-bone"},
		{"small", atoms.ButtonProps{Size: atoms.ButtonSmall}, "text-sm", "py-2"},
		{"medium", atoms.ButtonProps{}, "text-base", "text-sm"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := renderWithLabel(t, atoms.Button(tt.props), "x")
			if !strings.Contains(body, tt.want) {
				t.Errorf("body missing %q: %s", tt.want, body)
			}
			if strings.Contains(body, tt.not) {
				t.Errorf("body unexpectedly contains %q: %s", tt.not, body)
			}
		})
	}
}

func TestButton_TypeAndAttrs(t *testing.T) {
	body := renderWithLabel(t, atoms.Button(atoms.ButtonProps{
		Type:  "submit",
		Attrs: templ.Attributes{"data-testid": "go", "data-modal-close": true, "hx-get": "/a?b=1&c=2"},
	}), "x")

	for _, want := range []string{`type="submit"`, `data-testid="go"`, ` data-modal-close`, `hx-get="/a?b=1&amp;c=2"`} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q: %s", want, body)
		}
	}
}

func TestButtonLink_RendersAnchorWithoutType(t *testing.T) {
	body := renderWithLabel(t, atoms.ButtonLink(atoms.ButtonProps{Attrs: templ.Attributes{"href": "/scanner"}}), "続ける")

	if !strings.HasPrefix(body, "<a ") || !strings.Contains(body, `href="/scanner"`) || strings.Contains(body, "type=") {
		t.Errorf("unexpected anchor markup: %s", body)
	}
}

func TestInput_DefaultsToText(t *testing.T) {
	var sb strings.Builder
	err := atoms.Input(atoms.InputProps{ID: "i", Name: "n", Attrs: templ.Attributes{"required": true, "placeholder": "p"}}).Render(context.Background(), &sb)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`id="i"`, `name="n"`, `type="text"`, ` required`, `placeholder="p"`} {
		if !strings.Contains(sb.String(), want) {
			t.Errorf("body missing %q: %s", want, sb.String())
		}
	}
}

// The press ripple (static/src/components/ripple) is opt-in per element and
// needs the host to clip the circle, for Button and ButtonLink alike.
func TestButton_CarriesTheRippleHook(t *testing.T) {
	for name, c := range map[string]templ.Component{
		"button": atoms.Button(atoms.ButtonProps{}),
		"link":   atoms.ButtonLink(atoms.ButtonProps{Attrs: templ.Attributes{"href": "/x"}}),
	} {
		body := renderWithLabel(t, c, "x")
		for _, want := range []string{" data-ripple", "relative", "overflow-hidden"} {
			if !strings.Contains(body, want) {
				t.Errorf("%s: body missing %q: %s", name, want, body)
			}
		}
	}
}
