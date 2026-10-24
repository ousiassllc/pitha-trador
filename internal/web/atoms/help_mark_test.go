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
