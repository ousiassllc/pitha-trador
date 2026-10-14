package opsettings_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config/regimesettings"
	"github.com/ousiassllc/pitha-trador/internal/service/opsettings"
)

// issue #755: the regime parameters are edited on the Settings screen.
func TestRegime_DefaultsAndSave(t *testing.T) {
	ctx := context.Background()
	store := &fakeStore{rows: map[string]string{}}
	svc := newService(store)

	for key, want := range map[string]string{
		regimesettings.KeyWindowMinutes: "30", regimesettings.KeyAutocorrThreshold: "0.2",
		regimesettings.KeyMinVolatilityBps: "0.5", regimesettings.KeyVWAPZThreshold: "2", regimesettings.KeyABEnabled: "0",
	} {
		v, err := svc.Get(ctx, key)
		if err != nil || v.Current != want || v.Default != want || v.Overridden {
			t.Fatalf("Get(%s) = %+v, %v, want %s not overridden", key, v, err, want)
		}
	}

	for key, raw := range map[string]string{
		regimesettings.KeyWindowMinutes: " 45 ", regimesettings.KeyAutocorrThreshold: "0.35",
		regimesettings.KeyVWAPZThreshold: "1.5", regimesettings.KeyABEnabled: "1",
	} {
		if err := svc.Save(ctx, key, raw); err != nil {
			t.Fatalf("Save %s: %v", key, err)
		}
	}
	if got := store.rows[regimesettings.KeyWindowMinutes]; got != "45" {
		t.Fatalf("stored window = %q, want 45 (a JSON integer)", got)
	}
	if got := store.rows[regimesettings.KeyABEnabled]; got != "1" {
		t.Fatalf("stored A/B switch = %q, want 1", got)
	}
	if v, _ := svc.Get(ctx, regimesettings.KeyAutocorrThreshold); v.Current != "0.35" || !v.Overridden || v.Default != "0.2" {
		t.Fatalf("Get after save = %+v", v)
	}

	got, err := regimesettings.Load(ctx, store)
	if err != nil || got.WindowMinutes() != 45 || got.AutocorrThreshold != 0.35 || got.VWAPZThreshold != 1.5 || !got.ABEnabled {
		t.Fatalf("Load after save = %+v, %v", got, err)
	}

	if err := svc.Reset(ctx, regimesettings.KeyABEnabled); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	if _, ok := store.rows[regimesettings.KeyABEnabled]; ok {
		t.Fatal("Reset left the row")
	}
}

func TestRegime_SaveRejectsInvalid(t *testing.T) {
	ctx := context.Background()
	store := &fakeStore{rows: map[string]string{}}
	svc := newService(store)

	for name, tc := range map[string]struct{ key, raw string }{
		"text":                 {regimesettings.KeyVWAPZThreshold, "abc"},
		"window below minimum": {regimesettings.KeyWindowMinutes, "4"},
		"window above maximum": {regimesettings.KeyWindowMinutes, "121"},
		"window not integer":   {regimesettings.KeyWindowMinutes, "30.5"},
		"autocorr zero":        {regimesettings.KeyAutocorrThreshold, "0"},
		"autocorr one":         {regimesettings.KeyAutocorrThreshold, "1"},
		"volatility negative":  {regimesettings.KeyMinVolatilityBps, "-1"},
		"z zero":               {regimesettings.KeyVWAPZThreshold, "0"},
		"z above maximum":      {regimesettings.KeyVWAPZThreshold, "10.5"},
		"switch not 0 or 1":    {regimesettings.KeyABEnabled, "2"},
	} {
		err := svc.Save(ctx, tc.key, tc.raw)
		var invalid *opsettings.InvalidValueError
		if !errors.As(err, &invalid) {
			t.Errorf("%s: Save(%s, %q) error = %v, want *InvalidValueError", name, tc.key, tc.raw, err)
		}
	}
	if len(store.rows) != 0 {
		t.Fatalf("rejected values were stored: %v", store.rows)
	}
}
