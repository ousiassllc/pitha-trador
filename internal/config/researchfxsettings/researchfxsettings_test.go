package researchfxsettings_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config/researchfxsettings"
)

type fakeStore struct {
	rows map[string]string
	err  error
}

func (f fakeStore) Get(_ context.Context, key string) (string, bool, error) {
	v, ok := f.rows[key]
	return v, ok, f.err
}

// The record is opt-in: nothing stored, a stored 0 and any damaged value all mean
// off with the default pair; only a stored 1 turns it on.
func TestLoad_DefaultsAreOffAndInvalidStoredValuesCountAsDefaults(t *testing.T) {
	tests := []struct {
		name        string
		rows        map[string]string
		wantEnabled bool
		wantSymbols []string
	}{
		{"nothing stored", nil, false, []string{"USD_JPY"}},
		{"stored 0", map[string]string{researchfxsettings.KeyEnabled: "0"}, false, []string{"USD_JPY"}},
		{"stored 1", map[string]string{researchfxsettings.KeyEnabled: "1"}, true, []string{"USD_JPY"}},
		{"damaged string", map[string]string{researchfxsettings.KeyEnabled: `"yes"`}, false, []string{"USD_JPY"}},
		{"damaged number", map[string]string{researchfxsettings.KeyEnabled: "2"}, false, []string{"USD_JPY"}},
		{"null", map[string]string{researchfxsettings.KeyEnabled: "null"}, false, []string{"USD_JPY"}},
		{"two pairs", map[string]string{researchfxsettings.KeySymbols: `"USD_JPY, EUR_USD"`}, false, []string{"USD_JPY", "EUR_USD"}},
		{"damaged pairs", map[string]string{researchfxsettings.KeyEnabled: "1", researchfxsettings.KeySymbols: `"usd/jpy"`}, true, []string{"USD_JPY"}},
		{"pairs not a string", map[string]string{researchfxsettings.KeySymbols: `5`}, false, []string{"USD_JPY"}},
	}
	for _, tt := range tests {
		got, err := researchfxsettings.Load(context.Background(), fakeStore{rows: tt.rows})
		if err != nil {
			t.Fatalf("%s: Load: %v", tt.name, err)
		}
		if got.Enabled != tt.wantEnabled || !slices.Equal(got.Symbols, tt.wantSymbols) {
			t.Errorf("%s: Load = %+v, want enabled %v symbols %v", tt.name, got, tt.wantEnabled, tt.wantSymbols)
		}
	}
}

func TestLoad_AStoreFailureIsAnError(t *testing.T) {
	boom := errors.New("boom")
	if _, err := researchfxsettings.Load(context.Background(), fakeStore{err: boom}); !errors.Is(err, boom) {
		t.Fatalf("Load = %v, want the store error", err)
	}
}

func TestNormalize(t *testing.T) {
	for in, want := range map[string]string{"USD_JPY": "USD_JPY", " USD_JPY , EUR_USD ": "USD_JPY,EUR_USD"} {
		got, err := researchfxsettings.Normalize(in)
		if err != nil || got != want {
			t.Errorf("Normalize(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for name, in := range map[string]string{
		"blank":     "",
		"lowercase": "usd_jpy",
		"slash":     "USD/JPY",
		"duplicate": "USD_JPY,USD_JPY",
		"empty one": "USD_JPY,",
		"six pairs": strings.Join([]string{"USD_JPY", "EUR_USD", "EUR_JPY", "GBP_JPY", "AUD_JPY", "NZD_JPY"}, ","),
	} {
		if got, err := researchfxsettings.Normalize(in); err == nil {
			t.Errorf("%s: Normalize(%q) = %q, want an error", name, in, got)
		}
	}
	if _, err := researchfxsettings.Normalize("USD_JPY,EUR_USD,EUR_JPY,GBP_JPY,AUD_JPY"); err != nil {
		t.Errorf("five pairs: %v", err)
	}
}

func TestParseEnabled(t *testing.T) {
	for in, want := range map[string]bool{"0": false, "1": true, "1.0": true} {
		if got, err := researchfxsettings.ParseEnabled(in); err != nil || got != want {
			t.Errorf("ParseEnabled(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, in := range []string{"2", "-1", "null", `"1"`, "true", ""} {
		if _, err := researchfxsettings.ParseEnabled(in); err == nil {
			t.Errorf("ParseEnabled(%q) accepted", in)
		}
	}
}

func TestKeys(t *testing.T) {
	if !researchfxsettings.IsKey("research.fx.enabled") || !researchfxsettings.IsKey("research.fx.symbols") || researchfxsettings.IsKey("research.labels.enabled") {
		t.Fatal("IsKey does not match Keys")
	}
	if researchfxsettings.Text(true) != "1" || researchfxsettings.Text(false) != "0" {
		t.Fatal("Text is not 1/0")
	}
}
