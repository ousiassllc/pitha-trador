package jquantssettings_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config/jquantssettings"
	"github.com/ousiassllc/pitha-trador/internal/config/mcpwritesettings"
)

type fakeStore struct {
	rows map[string]string
	err  error
}

func (f fakeStore) Get(_ context.Context, key string) (string, bool, error) {
	v, ok := f.rows[key]
	return v, ok, f.err
}

// Nothing stored means OFF, nothing subscribed, a conservative cap and no purge request.
func TestLoad_DefaultsAreOffAndConservative(t *testing.T) {
	got, err := jquantssettings.Load(context.Background(), fakeStore{})
	want := jquantssettings.Settings{DailyRequestCap: 200, BackfillDays: 30}
	if err != nil || got != want {
		t.Fatalf("Load = %+v, %v; want %+v", got, err, want)
	}
	for _, key := range jquantssettings.Keys() {
		if jquantssettings.DefaultText(key) == "" {
			t.Errorf("key %s has no default text", key)
		}
	}
}

func TestLoad_StoredValuesApplyAndDamagedOnesFallBackToTheDefaults(t *testing.T) {
	ctx := context.Background()
	got, err := jquantssettings.Load(ctx, fakeStore{rows: map[string]string{
		jquantssettings.KeyEnabled: "1", jquantssettings.KeyPlanDaily: "1", jquantssettings.KeyPlanMinute: "1", jquantssettings.KeyPurgeRequested: "1",
		jquantssettings.KeyDailyRequestCap: "50", jquantssettings.KeyBackfillDays: "365",
	}})
	if err != nil || !got.Enabled || !got.PlanDaily || !got.PlanMinute || !got.PurgeRequested || got.DailyRequestCap != 50 || got.BackfillDays != 365 {
		t.Fatalf("Load = %+v, %v", got, err)
	}
	for _, bad := range []string{`"1"`, "true", "null", "", "0.5", "-1", "99999"} {
		rows := map[string]string{}
		for _, key := range jquantssettings.Keys() {
			rows[key] = bad
		}
		got, err := jquantssettings.Load(ctx, fakeStore{rows: rows})
		if err != nil || got != jquantssettings.Default() {
			t.Errorf("damaged %q: Load = %+v, %v; want the defaults", bad, got, err)
		}
	}
}

func TestParse_RangesOfTheIntegerKeys(t *testing.T) {
	for key, cases := range map[string]map[string]bool{
		jquantssettings.KeyDailyRequestCap: {"1": true, "5000": true, "0": false, "5001": false},
		jquantssettings.KeyBackfillDays:    {"1": true, "1830": true, "0": false, "1831": false},
		jquantssettings.KeyEnabled:         {"0": true, "1": true, "2": false},
	} {
		for raw, ok := range cases {
			if _, err := jquantssettings.Parse(key, raw); (err == nil) != ok {
				t.Errorf("Parse(%s, %s) err = %v, want ok=%v", key, raw, err, ok)
			}
		}
	}
}

func TestLoad_AReadFailureIsAnErrorNotAGuess(t *testing.T) {
	boom := errors.New("db locked")
	if _, err := jquantssettings.Load(context.Background(), fakeStore{err: boom}); !errors.Is(err, boom) {
		t.Fatalf("Load error = %v, want the read failure", err)
	}
}

// The Paper results MCP can write only the keys on its whitelist: none of the
// J-Quants keys (the switch, the plan flags, the cap, the window and the purge request)
// is on it, so the MCP can neither turn the adapter on nor delete its data.
func TestNoJQuantsKeyIsWritableThroughTheMCP(t *testing.T) {
	writable := mcpwritesettings.Keys()
	for _, key := range jquantssettings.Keys() {
		if slices.Contains(writable, key) {
			t.Errorf("%s is on the MCP settings_set whitelist", key)
		}
	}
}
