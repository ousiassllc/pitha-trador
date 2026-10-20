package paperbootstrapsettings_test

import (
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config/paperbootstrapsettings"
)

func TestApply_OnlyZeroAndOne(t *testing.T) {
	for raw, want := range map[string]bool{"0": false, "1": true, "1.0": true} {
		if got, err := paperbootstrapsettings.Apply(raw); err != nil || got != want {
			t.Errorf("Apply(%q) = %v, %v, want %v", raw, got, err, want)
		}
	}
	for _, raw := range []string{"2", "-1", "0.5", `"1"`, "true", ""} {
		if _, err := paperbootstrapsettings.Apply(raw); err == nil {
			t.Errorf("Apply(%q) succeeded, want an error", raw)
		}
	}
}

func TestDefaultIsOff(t *testing.T) {
	if paperbootstrapsettings.Default || paperbootstrapsettings.Text(paperbootstrapsettings.Default) != "0" {
		t.Fatal("the bootstrap must be off by default")
	}
}
