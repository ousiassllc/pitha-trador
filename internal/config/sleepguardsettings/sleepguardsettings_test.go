package sleepguardsettings_test

import (
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config/mcpwritesettings"
	"github.com/ousiassllc/pitha-trador/internal/config/sleepguardsettings"
)

// issue #863: the sleep guard changes how the PC behaves, so the Paper results
// MCP can never write it, and it is off unless the Settings screen stored 1.
func TestSleepGuardSwitchIsNotWhitelistedAndDefaultsOff(t *testing.T) {
	if mcpwritesettings.IsAllowed(sleepguardsettings.Key) {
		t.Fatal("power.sleep_guard_enabled is writable through the MCP")
	}
	for _, k := range mcpwritesettings.Keys() {
		if k == sleepguardsettings.Key {
			t.Fatal("power.sleep_guard_enabled is in the MCP whitelist")
		}
	}
	if sleepguardsettings.Default {
		t.Fatal("the sleep guard is on by default")
	}
}

func TestApply(t *testing.T) {
	for raw, want := range map[string]bool{"1": true, "0": false, "1.0": true} {
		if got, err := sleepguardsettings.Apply(raw); err != nil || got != want {
			t.Errorf("Apply(%q) = %v, %v; want %v", raw, got, err, want)
		}
	}
	for _, raw := range []string{"2", "-1", `"1"`, "true", "", "null", "0.5"} {
		if _, err := sleepguardsettings.Apply(raw); err == nil {
			t.Errorf("Apply(%q) accepted", raw)
		}
	}
}
