package atoms_test

import (
	"strings"
	"testing"

	"github.com/a-h/templ"

	"github.com/ousiassllc/pitha-trador/internal/web/atoms"
)

func TestInput_DefaultIsFlexibleRowField(t *testing.T) {
	body := renderWithLabel(t, atoms.Input(atoms.InputProps{ID: "x", Name: "n"}), "")

	for _, want := range []string{`id="x"`, `name="n"`, `type="text"`, "rounded-md border border-slate-300", "flex-1", "px-3 py-1"} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q: %s", want, body)
		}
	}
}

// A stacked label needs a field that does not grow (no flex-1): Class
// replaces the default layout classes while the shared border stays.
func TestInput_ClassReplacesLayoutButKeepsSharedBorder(t *testing.T) {
	body := renderWithLabel(t, atoms.Input(atoms.InputProps{
		Name: "from", Type: "date", Class: "mt-1 px-2 py-1 text-sm",
		Attrs: templ.Attributes{"required": true, "min": "1", "data-testid": "f"},
	}), "")

	for _, want := range []string{`type="date"`, "rounded-md border border-slate-300 mt-1 px-2 py-1 text-sm", ` required`, `min="1"`, `data-testid="f"`} {
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
		ID: "s", Name: "days", Class: "bg-white px-3 py-2", Attrs: templ.Attributes{"data-testid": "d"},
	}), `<option value="1">one</option>`)

	for _, want := range []string{`<select`, `id="s"`, `name="days"`, "rounded-md border border-slate-300 bg-white px-3 py-2", `data-testid="d"`, `<option value="1">one</option></select>`} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q: %s", want, body)
		}
	}
}

func TestSelect_DefaultClass(t *testing.T) {
	body := renderWithLabel(t, atoms.Select(atoms.SelectProps{Name: "n"}), "")
	if !strings.Contains(body, "rounded-md border border-slate-300 px-3 py-1") {
		t.Errorf("default class missing: %s", body)
	}
}
