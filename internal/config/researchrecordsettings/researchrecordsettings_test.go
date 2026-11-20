package researchrecordsettings_test

import (
	"slices"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config/researchrecordsettings"
)

// Every key of both records is found, told apart as switch or symbol list, given
// its own default and checked by its own rules.
func TestRecordsAreDispatchedToTheirOwnSettings(t *testing.T) {
	want := []string{"research.fx.enabled", "research.fx.symbols", "research.index.enabled", "research.index.symbols"}
	if got := researchrecordsettings.Keys(); !slices.Equal(got, want) {
		t.Fatalf("Keys = %v, want %v", got, want)
	}
	for _, key := range want {
		if !researchrecordsettings.IsKey(key) {
			t.Errorf("IsKey(%s) = false", key)
		}
		if researchrecordsettings.IsSwitchKey(key) == researchrecordsettings.IsSymbolsKey(key) {
			t.Errorf("%s is both or neither of switch and symbols", key)
		}
	}
	if researchrecordsettings.IsKey("research.labels.enabled") || researchrecordsettings.IsSwitchKey("research.labels.enabled") || researchrecordsettings.IsSymbolsKey("watch.observation_symbols") {
		t.Error("a key of another setting was claimed")
	}
	for key, def := range map[string]string{"research.fx.enabled": "0", "research.fx.symbols": "USD_JPY", "research.index.enabled": "0", "research.index.symbols": "101,151"} {
		if got := researchrecordsettings.DefaultText(key); got != def {
			t.Errorf("DefaultText(%s) = %q, want %q", key, got, def)
		}
	}
	if got, err := researchrecordsettings.Normalize("research.index.symbols", " 101 , 151 "); err != nil || got != "101,151" {
		t.Errorf("index Normalize = %q, %v", got, err)
	}
	if _, err := researchrecordsettings.Normalize("research.fx.symbols", "101"); err == nil {
		t.Error("a pair list took an index code")
	}
	if _, err := researchrecordsettings.Normalize("research.index.symbols", "USD_JPY"); err == nil {
		t.Error("an index list took a pair name")
	}
	if err := researchrecordsettings.Validate("research.index.enabled", "1"); err != nil {
		t.Errorf("index switch 1: %v", err)
	}
	if err := researchrecordsettings.Validate("research.index.enabled", "2"); err == nil {
		t.Error("index switch 2 accepted")
	}
	if err := researchrecordsettings.Validate("research.fx.symbols", `"USD_JPY"`); err != nil {
		t.Errorf("fx symbols: %v", err)
	}
}
