package mcpsettings

import (
	"strings"

	"github.com/ousiassllc/pitha-trador/internal/config/mcpwritesettings"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/settings"
)

// settingNames are the names the Settings screen gives the settings an
// experiment may vary (the MCP-changeable ones): the screen never shows their
// keys, so these are what the form offers for the name of a setting.
func settingNames() []string {
	keys := mcpwritesettings.Keys()
	names := make([]string, len(keys))
	for i, key := range keys {
		names[i] = settings.FieldLabel(key)
	}
	return names
}

// settingKey is the key of the setting the form's name stands for: the name the
// Settings screen gives it, or the key itself (what the MCP and the API use).
// Anything else is returned as typed, for the experiment rules to refuse.
func settingKey(name string) string {
	name = strings.TrimSpace(name)
	for _, key := range mcpwritesettings.Keys() {
		if settings.FieldLabel(key) == name {
			return key
		}
	}
	return name
}
