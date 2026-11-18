package disclosuresrcsettings_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config/disclosuresrcsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/mcpwritesettings"
)

// The on/off switches, the caps and the retention of the extra disclosure sources
// are changed on the Settings screen only: none of them is on the Paper results
// MCP's settings_set whitelist, like jev.trial.enabled.
func TestNoDisclosureSourceKeyIsMCPWritable(t *testing.T) {
	writable := mcpwritesettings.Keys()
	for _, key := range disclosuresrcsettings.Keys() {
		if slices.Contains(writable, key) {
			t.Errorf("%s is on the MCP write whitelist", key)
		}
	}
	for _, key := range writable {
		if strings.HasPrefix(key, disclosuresrcsettings.KeyPrefix) {
			t.Errorf("the MCP write whitelist holds %s, a disclosure source key", key)
		}
	}
}
