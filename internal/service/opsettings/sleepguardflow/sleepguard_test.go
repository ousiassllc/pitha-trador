// Package sleepguardflow_test checks the sleep guard switch (issue #863) saved
// through opsettings.Service. It lives in its own directory to keep
// internal/service/opsettings within the linterly directory budget.
package sleepguardflow_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/config/sleepguardsettings"
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

// The switch is off by default, stored as the JSON integer 1 / 0, and read
// back by the same Source the sleep guard uses, so a Save applies without a
// restart.
func TestSleepGuardSwitch_DefaultOffSaveAndReset(t *testing.T) {
	ctx := context.Background()
	store := &fakeStore{rows: map[string]string{}}
	svc := opsettings.New(store, config.StrategyConfig{}, "/data/logs")
	source := sleepguardsettings.Source{Store: store}

	v, err := svc.Get(ctx, sleepguardsettings.Key)
	if err != nil || v.Current != "0" || v.Default != "0" || v.Overridden {
		t.Fatalf("default = %+v, %v; want 0, not overridden", v, err)
	}
	if on, err := source.SleepGuardEnabled(ctx); err != nil || on {
		t.Fatalf("nothing stored: enabled = %v, %v; want off", on, err)
	}

	if err := svc.Save(ctx, sleepguardsettings.Key, " 1 "); err != nil {
		t.Fatalf("Save 1: %v", err)
	}
	if got := store.rows[sleepguardsettings.Key]; got != "1" {
		t.Fatalf("stored = %q, want the JSON integer 1", got)
	}
	if on, err := source.SleepGuardEnabled(ctx); err != nil || !on {
		t.Fatalf("after Save 1: enabled = %v, %v; want on", on, err)
	}

	if err := svc.Reset(ctx, sleepguardsettings.Key); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	if on, _ := source.SleepGuardEnabled(ctx); on {
		t.Fatal("after Reset the guard is still on")
	}
}

func TestSleepGuardSwitch_RejectsOtherValues(t *testing.T) {
	svc := opsettings.New(&fakeStore{rows: map[string]string{}}, config.StrategyConfig{}, "/data/logs")
	for _, raw := range []string{"2", "-1", "on", "0.5"} {
		var invalid *opsettings.InvalidValueError
		if err := svc.Save(context.Background(), sleepguardsettings.Key, raw); !errors.As(err, &invalid) {
			t.Errorf("Save(%q) = %v, want an *InvalidValueError", raw, err)
		}
	}
}

// A stored row that no longer parses counts as off (the guard never holds the
// PC awake on a damaged setting).
func TestSleepGuardSource_DamagedRowCountsAsOff(t *testing.T) {
	store := &fakeStore{rows: map[string]string{sleepguardsettings.Key: `"yes"`}}
	if on, err := (sleepguardsettings.Source{Store: store}).SleepGuardEnabled(context.Background()); err != nil || on {
		t.Fatalf("damaged row: enabled = %v, %v; want off", on, err)
	}
}
