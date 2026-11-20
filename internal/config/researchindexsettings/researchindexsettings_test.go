package researchindexsettings_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config/researchindexsettings"
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
// off with the default codes; only a stored 1 turns it on.
func TestLoad_DefaultsAreOffAndInvalidStoredValuesCountAsDefaults(t *testing.T) {
	def := []string{"101", "151"}
	tests := []struct {
		name        string
		rows        map[string]string
		wantEnabled bool
		wantSymbols []string
	}{
		{"nothing stored", nil, false, def},
		{"stored 0", map[string]string{researchindexsettings.KeyEnabled: "0"}, false, def},
		{"stored 1", map[string]string{researchindexsettings.KeyEnabled: "1"}, true, def},
		{"damaged string", map[string]string{researchindexsettings.KeyEnabled: `"yes"`}, false, def},
		{"damaged number", map[string]string{researchindexsettings.KeyEnabled: "2"}, false, def},
		{"null", map[string]string{researchindexsettings.KeyEnabled: "null"}, false, def},
		{"one code", map[string]string{researchindexsettings.KeySymbols: `"101"`}, false, []string{"101"}},
		{"damaged codes", map[string]string{researchindexsettings.KeyEnabled: "1", researchindexsettings.KeySymbols: `"1 0 1"`}, true, def},
		{"codes not a string", map[string]string{researchindexsettings.KeySymbols: `5`}, false, def},
	}
	for _, tt := range tests {
		got, err := researchindexsettings.Load(context.Background(), fakeStore{rows: tt.rows})
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
	if _, err := researchindexsettings.Load(context.Background(), fakeStore{err: boom}); !errors.Is(err, boom) {
		t.Fatalf("Load = %v, want the store error", err)
	}
}

func TestNormalize(t *testing.T) {
	for in, want := range map[string]string{"101": "101", " 101 , 151 ": "101,151"} {
		got, err := researchindexsettings.Normalize(in)
		if err != nil || got != want {
			t.Errorf("Normalize(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for name, in := range map[string]string{
		"blank":     "",
		"space":     "1 01",
		"too long":  "123456789",
		"duplicate": "101,101",
		"empty one": "101,",
		"six codes": "101,102,103,104,105,106",
	} {
		if got, err := researchindexsettings.Normalize(in); err == nil {
			t.Errorf("%s: Normalize(%q) = %q, want an error", name, in, got)
		}
	}
	if _, err := researchindexsettings.Normalize("101,102,103,104,105"); err != nil {
		t.Errorf("five codes: %v", err)
	}
}

func TestParseEnabled(t *testing.T) {
	for in, want := range map[string]bool{"0": false, "1": true, "1.0": true} {
		if got, err := researchindexsettings.ParseEnabled(in); err != nil || got != want {
			t.Errorf("ParseEnabled(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, in := range []string{"2", "-1", "null", `"1"`, "true", ""} {
		if _, err := researchindexsettings.ParseEnabled(in); err == nil {
			t.Errorf("ParseEnabled(%q) accepted", in)
		}
	}
}

func TestKeys(t *testing.T) {
	if !researchindexsettings.IsKey("research.index.enabled") || !researchindexsettings.IsKey("research.index.symbols") || researchindexsettings.IsKey("research.fx.enabled") {
		t.Fatal("IsKey does not match Keys")
	}
	if researchindexsettings.Text(true) != "1" || researchindexsettings.Text(false) != "0" {
		t.Fatal("Text is not 1/0")
	}
}
