package watchsettings_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config/jevshadowsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/mcpwritesettings"
	"github.com/ousiassllc/pitha-trador/internal/config/watchsettings"
)

type fakeStore struct {
	rows map[string]string
	err  error
}

func (f fakeStore) Get(_ context.Context, key string) (string, bool, error) {
	v, ok := f.rows[key]
	return v, ok, f.err
}

// issue #1055: with nothing stored the behaviour is today's: the full kabu
// watch list, no observation symbol, the same 0.10 USD cap as the shadow.
func TestLoad_DefaultsAreTodaysBehaviour(t *testing.T) {
	got := watchsettings.Load(context.Background(), fakeStore{})
	if got.MaxWatched != 45 || watchsettings.DefaultMaxWatched != 45 || watchsettings.MaxKabuWatched != 45 {
		t.Errorf("watch size = %d (default %d, cap %d), want 45", got.MaxWatched, watchsettings.DefaultMaxWatched, watchsettings.MaxKabuWatched)
	}
	if got.ObservationEnabled || watchsettings.DefaultObservationEnabled {
		t.Error("the observation is on by default")
	}
	if len(got.Observed()) != 0 {
		t.Errorf("Observed() = %v, want none", got.Observed())
	}
	if got.ObservationCapUSD != 0.10 {
		t.Errorf("observation cap = %v, want 0.10", got.ObservationCapUSD)
	}
	// The shadow's own default is not touched by this issue.
	if jevshadowsettings.DefaultDailyCostCapUSD != 0.10 {
		t.Errorf("shadow cap default = %v, want 0.10", jevshadowsettings.DefaultDailyCostCapUSD)
	}
}

func TestParseMaxWatched(t *testing.T) {
	for raw, want := range map[string]int{"1": 1, "20": 20, "45": 45, "45.0": 45} {
		if got, err := watchsettings.ParseMaxWatched(raw); err != nil || got != want {
			t.Errorf("ParseMaxWatched(%q) = %d, %v; want %d", raw, got, err, want)
		}
	}
	for _, raw := range []string{"0", "-1", "46", "120", "1000", "45.5", `"45"`, "null", ""} {
		if _, err := watchsettings.ParseMaxWatched(raw); err == nil {
			t.Errorf("ParseMaxWatched(%q) accepted", raw)
		}
	}
	// The refusal names its reason: the broker's stream cap.
	_, err := watchsettings.ParseMaxWatched("46")
	if err == nil || !strings.Contains(err.Error(), "最大 45 件") || !strings.Contains(err.Error(), "50") {
		t.Errorf("the reason for 46 is not stated: %v", err)
	}
}

func TestClampWatched(t *testing.T) {
	for _, c := range []struct{ n, cap, want int }{{45, 45, 45}, {60, 45, 45}, {10, 45, 10}, {0, 45, 1}, {-3, 45, 1}, {200, 120, 120}, {30, 0, 30}} {
		if got := watchsettings.ClampWatched(c.n, c.cap); got != c.want {
			t.Errorf("ClampWatched(%d, %d) = %d, want %d", c.n, c.cap, got, c.want)
		}
	}
}

func TestParseEnabled(t *testing.T) {
	for raw, want := range map[string]bool{"1": true, "0": false} {
		if got, err := watchsettings.ParseEnabled(raw); err != nil || got != want {
			t.Errorf("ParseEnabled(%q) = %v, %v; want %v", raw, got, err, want)
		}
	}
	for _, raw := range []string{"2", "-1", `"1"`, "true", "", "null", "0.5"} {
		if _, err := watchsettings.ParseEnabled(raw); err == nil {
			t.Errorf("ParseEnabled(%q) accepted", raw)
		}
	}
}

func TestNormalizeSymbols(t *testing.T) {
	got, err := watchsettings.NormalizeSymbols(" 7203, 6758，285a  7203 ")
	if err != nil || got != "7203,6758,285A" {
		t.Errorf("NormalizeSymbols = %q, %v; want the distinct upper-cased codes", got, err)
	}
	for _, raw := range []string{"", " , ", "72 03!", "ABCDEFGHIJK", "1,2,3,4,5,6,7,8,9,10,11"} {
		if _, err := watchsettings.NormalizeSymbols(raw); err == nil {
			t.Errorf("NormalizeSymbols(%q) accepted", raw)
		}
	}
}

func TestValidateCap(t *testing.T) {
	for _, usd := range []float64{0.01, 0.10, 100} {
		if err := watchsettings.ValidateCap(usd); err != nil {
			t.Errorf("ValidateCap(%v): %v", usd, err)
		}
	}
	for _, usd := range []float64{0, -1, 100.01} {
		if err := watchsettings.ValidateCap(usd); err == nil {
			t.Errorf("ValidateCap(%v) accepted: the cap must never be switched off", usd)
		}
	}
}

// A stored row that no longer parses, or a failed read, can neither raise the
// watch size nor turn the observation on.
func TestLoad_FaultsFallBackToTheSafeSide(t *testing.T) {
	bad := fakeStore{rows: map[string]string{
		watchsettings.KeyMaxWatched:                 "9999",
		watchsettings.KeyObservationEnabled:         "7",
		watchsettings.KeyObservationSymbols:         `"!!"`,
		watchsettings.KeyObservationDailyCostCapUSD: "-1",
	}}
	got := watchsettings.Load(context.Background(), bad)
	if got.MaxWatched != 45 || got.ObservationEnabled || len(got.ObservationSymbols) != 0 || got.ObservationCapUSD != 0.10 {
		t.Errorf("damaged rows = %+v, want the defaults", got)
	}
	got = watchsettings.Load(context.Background(), fakeStore{err: errors.New("db locked")})
	if got.MaxWatched != 45 || got.ObservationEnabled {
		t.Errorf("a failed read = %+v, want the defaults", got)
	}
}

func TestLoad_StoredValuesApply(t *testing.T) {
	store := fakeStore{rows: map[string]string{
		watchsettings.KeyMaxWatched:                 "30",
		watchsettings.KeyObservationEnabled:         "1",
		watchsettings.KeyObservationSymbols:         `"7203,6758"`,
		watchsettings.KeyObservationDailyCostCapUSD: "0.5",
	}}
	got := watchsettings.Load(context.Background(), store)
	if got.MaxWatched != 30 || !got.ObservationEnabled || got.ObservationCapUSD != 0.5 || strings.Join(got.Observed(), ",") != "7203,6758" {
		t.Errorf("Load = %+v", got)
	}
	// The switch off hides the list.
	store.rows[watchsettings.KeyObservationEnabled] = "0"
	if got := watchsettings.Load(context.Background(), store); len(got.Observed()) != 0 {
		t.Errorf("Observed() = %v with the switch off, want none", got.Observed())
	}
}

func TestUsage_NewDayStartsFromZero(t *testing.T) {
	u := watchsettings.Usage{Date: "2026-11-10", Calls: 3, Failures: 1, CostUSD: 0.2}
	if got := u.On("2026-11-11"); got.Calls != 0 || got.CostUSD != 0 || got.Date != "2026-11-11" {
		t.Errorf("a new day = %+v, want zero counters", got)
	}
	if got := u.On("2026-11-10"); got != u {
		t.Errorf("the same day = %+v, want %+v", got, u)
	}
	enc, err := watchsettings.EncodeUsage(u)
	if err != nil {
		t.Fatal(err)
	}
	if back, err := watchsettings.ParseUsage(enc); err != nil || back != u {
		t.Errorf("round trip = %+v, %v", back, err)
	}
	if _, err := watchsettings.ParseUsage(`{"date":"x"}`); err == nil {
		t.Error("a damaged usage row parsed")
	}
}

// The observation and the watch size change which symbols are watched and
// called on Jev, so a remote client (the Paper results MCP) must not write them.
func TestKeysAreNotInTheMCPWhitelist(t *testing.T) {
	for _, key := range append(watchsettings.Keys(), watchsettings.KeyObservationUsage) {
		if mcpwritesettings.IsAllowed(key) {
			t.Errorf("%s is writable through the MCP", key)
		}
	}
}
