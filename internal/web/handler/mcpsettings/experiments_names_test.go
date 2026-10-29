package mcpsettings

import (
	"slices"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config/mcpwritesettings"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/settings"
)

// The form's name of a setting and its key must correspond one to one:
// settingKey finds the key by comparing names, so two settings sharing a
// name (or a name equal to another setting's key) would silently turn one
// into the other. This pins the assumption on the real list of settings the
// MCP may change.
func TestSettingNamesAndKeysAreOneToOne(t *testing.T) {
	keys := mcpwritesettings.Keys()
	if len(keys) == 0 {
		t.Fatal("mcpwritesettings.Keys() is empty")
	}
	names := settingNames()
	if len(names) != len(keys) {
		t.Fatalf("%d names for %d keys", len(names), len(keys))
	}
	for i, key := range keys {
		name := names[i]
		if name != settings.FieldLabel(key) {
			t.Errorf("settingNames()[%d] = %q, want the Settings screen's name %q of %s", i, name, settings.FieldLabel(key), key)
		}
		if got := settingKey(name); got != key {
			t.Errorf("settingKey(%q) = %q, want %q", name, got, key)
		}
		if got := settingKey(key); got != key {
			t.Errorf("settingKey(%q) = %q, want the key itself", key, got)
		}
		if got := settingKey("  " + name + "\t"); got != key {
			t.Errorf("settingKey(padded %q) = %q, want %q", name, got, key)
		}
		if n := countOf(names, name); n != 1 {
			t.Errorf("name %q is given to %d settings, want exactly 1", name, n)
		}
		if slices.Contains(keys, name) {
			t.Errorf("name %q of %s is also a setting key", name, key)
		}
	}
}

func TestSettingKeyReturnsUnknownNamesAsTyped(t *testing.T) {
	for _, in := range []string{"", "no_such_setting", "その他の設定"} {
		if got := settingKey(in); got != in {
			t.Errorf("settingKey(%q) = %q, want it as typed", in, got)
		}
	}
	if got := settingKey("  no_such_setting "); got != "no_such_setting" {
		t.Errorf("settingKey(padded unknown) = %q, want it trimmed", got)
	}
}

func countOf(list []string, s string) int {
	n := 0
	for _, v := range list {
		if v == s {
			n++
		}
	}
	return n
}
