package molecules_test

import (
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/web/molecules"
)

// issue #1014: an on/off setting is drawn as atoms.Switch (checked while the
// stored value is 1) and an unchecked switch still posts 0 through the hidden
// field that follows it; a normal setting stays a text input.
func TestSettingFieldRow_OnOffIsASwitchThatPostsZeroWhenOff(t *testing.T) {
	on := render(t, molecules.SettingFieldRow(molecules.SettingFieldRowProps{Key: "k", Label: "L", OnOff: true, Value: "1"}))
	off := render(t, molecules.SettingFieldRow(molecules.SettingFieldRowProps{Key: "k", Label: "L", OnOff: true, Value: "0"}))
	text := render(t, molecules.SettingFieldRow(molecules.SettingFieldRowProps{Key: "k", Label: "L", Value: "1"}))
	for name, html := range map[string]string{"on": on, "off": off} {
		if !strings.Contains(html, `role="switch"`) || !strings.Contains(html, `id="setting-input-k"`) {
			t.Errorf("%s: an on/off row must render a switch with the field id: %s", name, html)
		}
		hidden := strings.Index(html, `<input type="hidden" name="value" value="0">`)
		if hidden < 0 || strings.Contains(html, `type="text"`) || strings.Index(html, `role="switch"`) > hidden {
			t.Errorf("%s: no text input, and the hidden 0 must follow the switch so a checked 1 wins: %s", name, html)
		}
	}
	if !strings.Contains(on, ` checked id="setting-input-k"`) || strings.Contains(off, ` checked id="setting-input-k"`) {
		t.Errorf("the switch is checked only while the value is 1: on=%s off=%s", on, off)
	}
	if strings.Contains(text, `role="switch"`) {
		t.Errorf("a normal setting must stay a text input: %s", text)
	}
}

// A row whose current value could not be read must not offer an on/off save:
// the switch would render OFF and saving would persist an explicit 0 the
// operator never chose. The switch is disabled, no hidden 0 and no 保存 button
// are rendered, and an inline notice says why.
func TestSettingFieldRow_UnreadableOnOffCannotBeSaved(t *testing.T) {
	html := render(t, molecules.SettingFieldRow(molecules.SettingFieldRowProps{Key: "k", Label: "L", OnOff: true, Unreadable: true}))
	if !strings.Contains(html, `role="switch"`) || !strings.Contains(html, ` disabled id="setting-input-k"`) {
		t.Errorf("the switch must still be shown, disabled: %s", html)
	}
	if strings.Contains(html, `name="value" value="0"`) || strings.Contains(html, `data-testid="save-setting-k"`) {
		t.Errorf("an unreadable on/off row must post nothing and offer no save: %s", html)
	}
	if !strings.Contains(html, `data-testid="unreadable-k"`) || !strings.Contains(html, "読み込めなかった") {
		t.Errorf("the row must say the current value could not be read: %s", html)
	}

	readable := render(t, molecules.SettingFieldRow(molecules.SettingFieldRowProps{Key: "k", Label: "L", OnOff: true, Value: "0"}))
	if strings.Contains(readable, `data-testid="unreadable-k"`) || strings.Contains(readable, ` disabled id="setting-input-k"`) || !strings.Contains(readable, `data-testid="save-setting-k"`) {
		t.Errorf("a readable on/off row stays editable: %s", readable)
	}
}
