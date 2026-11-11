// Package watchflow_test checks the ranking watch size and observation
// settings (issue #1055) saved through opsettings.Service. It lives in its own
// directory to keep internal/service/opsettings within the linterly directory
// budget.
package watchflow_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/config/watchsettings"
	"github.com/ousiassllc/pitha-trador/internal/service/opsettings"
)

type fakeStore struct{ rows map[string]string }

func (f *fakeStore) Get(_ context.Context, key string) (string, bool, error) {
	v, ok := f.rows[key]
	return v, ok, nil
}

func (f *fakeStore) Set(_ context.Context, key, value string, _ time.Time) error {
	f.rows[key] = value
	return nil
}

func (f *fakeStore) Delete(_ context.Context, key string) error {
	delete(f.rows, key)
	return nil
}

func newService() (*opsettings.Service, *fakeStore) {
	store := &fakeStore{rows: map[string]string{}}
	return opsettings.New(store, config.StrategyConfig{}, "/data/logs"), store
}

// The defaults are today's behaviour: the full kabu watch list, the
// observation off with no symbol, the same 0.10 USD cap as the shadow.
func TestWatchSettings_DefaultsAreTodaysBehaviour(t *testing.T) {
	svc, _ := newService()
	for key, want := range map[string]string{
		watchsettings.KeyMaxWatched:                 "45",
		watchsettings.KeyObservationEnabled:         "0",
		watchsettings.KeyObservationSymbols:         "",
		watchsettings.KeyObservationDailyCostCapUSD: "0.1",
	} {
		v, err := svc.Get(context.Background(), key)
		if err != nil || v.Current != want || v.Default != want || v.Overridden {
			t.Errorf("%s = %+v, %v; want current/default %q, not overridden", key, v, err, want)
		}
	}
}

func TestWatchSettings_SaveWithinTheBrokerCapAndReset(t *testing.T) {
	ctx := context.Background()
	svc, store := newService()
	for _, raw := range []string{"1", "30", " 45 "} {
		if err := svc.Save(ctx, watchsettings.KeyMaxWatched, raw); err != nil {
			t.Fatalf("Save(%q): %v", raw, err)
		}
	}
	if got := store.rows[watchsettings.KeyMaxWatched]; got != "45" {
		t.Fatalf("stored = %q, want the JSON integer 45", got)
	}
	if err := svc.Save(ctx, watchsettings.KeyMaxWatched, "30"); err != nil {
		t.Fatal(err)
	}
	if got := watchsettings.Load(ctx, store).MaxWatched; got != 30 {
		t.Errorf("the application reads %d, want 30", got)
	}
	if err := svc.Reset(ctx, watchsettings.KeyMaxWatched); err != nil {
		t.Fatal(err)
	}
	if got := watchsettings.Load(ctx, store).MaxWatched; got != 45 {
		t.Errorf("after Reset the application reads %d, want the default 45", got)
	}
}

// A value above the broker's stream cap (kabu 45 = 50 slots - 5 for REST) is
// refused with its reason and the stored row is untouched.
func TestWatchSettings_RejectsAboveTheStreamCap(t *testing.T) {
	ctx := context.Background()
	svc, store := newService()
	if err := svc.Save(ctx, watchsettings.KeyMaxWatched, "30"); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{"46", "50", "120", "100000", "0", "-5", "30.5", "abc"} {
		err := svc.Save(ctx, watchsettings.KeyMaxWatched, raw)
		var invalid *opsettings.InvalidValueError
		if !errors.As(err, &invalid) {
			t.Errorf("Save(%q) = %v, want an *InvalidValueError", raw, err)
			continue
		}
		if raw == "46" && !strings.Contains(invalid.Reason, "最大 45 件") {
			t.Errorf("the reason for 46 = %q, want the stream cap", invalid.Reason)
		}
	}
	if got := store.rows[watchsettings.KeyMaxWatched]; got != "30" {
		t.Errorf("a refused value changed the row: %q", got)
	}
}

func TestWatchSettings_ObservationSaveAndValidation(t *testing.T) {
	ctx := context.Background()
	svc, store := newService()
	if err := svc.Save(ctx, watchsettings.KeyObservationSymbols, "7203, 6758"); err != nil {
		t.Fatalf("Save symbols: %v", err)
	}
	if got := store.rows[watchsettings.KeyObservationSymbols]; got != `"7203,6758"` {
		t.Errorf("stored symbols = %q", got)
	}
	if v, _ := svc.Get(ctx, watchsettings.KeyObservationSymbols); v.Current != "7203,6758" {
		t.Errorf("Get symbols = %+v", v)
	}
	if err := svc.Save(ctx, watchsettings.KeyObservationEnabled, "1"); err != nil {
		t.Fatalf("Save switch: %v", err)
	}
	if got := watchsettings.Load(ctx, store); strings.Join(got.Observed(), ",") != "7203,6758" {
		t.Errorf("Observed() = %v", got.Observed())
	}
	for key, raw := range map[string]string{
		watchsettings.KeyObservationEnabled:         "2",
		watchsettings.KeyObservationSymbols:         "1,2,3,4,5,6,7,8,9,10,11",
		watchsettings.KeyObservationDailyCostCapUSD: "0",
	} {
		var invalid *opsettings.InvalidValueError
		if err := svc.Save(ctx, key, raw); !errors.As(err, &invalid) {
			t.Errorf("Save(%s, %q) = %v, want an *InvalidValueError", key, raw, err)
		}
	}
	if err := svc.Save(ctx, watchsettings.KeyObservationDailyCostCapUSD, "0.25"); err != nil {
		t.Errorf("Save cap: %v", err)
	}
	if err := svc.Save(ctx, watchsettings.KeyObservationDailyCostCapUSD, "100.5"); err == nil {
		t.Error("a cap above the bound was saved")
	}
}

// The watch settings are managed keys, and the day's usage record the
// application writes is not one: the Settings screen can neither show nor
// edit it.
func TestWatchSettings_KeysAreManaged(t *testing.T) {
	for _, key := range watchsettings.Keys() {
		if !opsettings.IsKnownKey(key) {
			t.Errorf("%s is not a managed Settings key", key)
		}
	}
	if opsettings.IsKnownKey(watchsettings.KeyObservationUsage) {
		t.Error("the usage record is editable on the Settings screen")
	}
}
