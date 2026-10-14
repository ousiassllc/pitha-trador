package regimesettings_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/regimesettings"
)

type fakeStore struct {
	rows map[string]string
	err  error
}

func (f fakeStore) Get(_ context.Context, key string) (string, bool, error) {
	v, ok := f.rows[key]
	return v, ok, f.err
}

func TestDefault(t *testing.T) {
	s := regimesettings.Default()
	if s.Window != 30*time.Minute || s.WindowMinutes() != 30 || s.AutocorrThreshold != 0.2 || s.MinVolatilityBps != 0.5 || s.VWAPZThreshold != 2 || s.ABEnabled {
		t.Fatalf("Default() = %+v, want 30m / 0.2 / 0.5 bps / z 2 / A/B off", s)
	}
}

func TestApply(t *testing.T) {
	cases := []struct {
		name, key, value string
		wantErr          bool
	}{
		{"window", regimesettings.KeyWindowMinutes, "45", false},
		{"window min", regimesettings.KeyWindowMinutes, "5", false},
		{"window max", regimesettings.KeyWindowMinutes, "120", false},
		{"window below min", regimesettings.KeyWindowMinutes, "4", true},
		{"window above max", regimesettings.KeyWindowMinutes, "121", true},
		{"window fraction", regimesettings.KeyWindowMinutes, "30.5", true},
		{"autocorr", regimesettings.KeyAutocorrThreshold, "0.35", false},
		{"autocorr zero", regimesettings.KeyAutocorrThreshold, "0", true},
		{"autocorr one", regimesettings.KeyAutocorrThreshold, "1", true},
		{"autocorr negative", regimesettings.KeyAutocorrThreshold, "-0.2", true},
		{"volatility zero", regimesettings.KeyMinVolatilityBps, "0", false},
		{"volatility max", regimesettings.KeyMinVolatilityBps, "1000", false},
		{"volatility negative", regimesettings.KeyMinVolatilityBps, "-1", true},
		{"volatility above max", regimesettings.KeyMinVolatilityBps, "1000.1", true},
		{"z", regimesettings.KeyVWAPZThreshold, "1.5", false},
		{"z max", regimesettings.KeyVWAPZThreshold, "10", false},
		{"z zero", regimesettings.KeyVWAPZThreshold, "0", true},
		{"z above max", regimesettings.KeyVWAPZThreshold, "10.5", true},
		{"ab on", regimesettings.KeyABEnabled, "1", false},
		{"ab off", regimesettings.KeyABEnabled, "0", false},
		{"ab other", regimesettings.KeyABEnabled, "2", true},
		{"ab fraction", regimesettings.KeyABEnabled, "0.5", true},
		{"string", regimesettings.KeyVWAPZThreshold, `"2"`, true},
		{"unknown key", "regime.other", "1", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := regimesettings.Default()
			if err := regimesettings.Apply(&s, c.key, c.value); (err != nil) != c.wantErr {
				t.Fatalf("Apply(%s, %s) error = %v, wantErr %v", c.key, c.value, err, c.wantErr)
			}
		})
	}
}

func TestApply_SetsFieldAndTextRoundTrips(t *testing.T) {
	s := regimesettings.Default()
	for key, value := range map[string]string{
		regimesettings.KeyWindowMinutes: "45", regimesettings.KeyAutocorrThreshold: "0.35",
		regimesettings.KeyMinVolatilityBps: "1.25", regimesettings.KeyVWAPZThreshold: "1.5", regimesettings.KeyABEnabled: "1",
	} {
		if err := regimesettings.Apply(&s, key, value); err != nil {
			t.Fatalf("Apply(%s): %v", key, err)
		}
		if text, ok := s.Text(key); !ok || text != value {
			t.Errorf("Text(%s) = %q, %v, want %q", key, text, ok, value)
		}
	}
	want := regimesettings.Settings{Window: 45 * time.Minute, AutocorrThreshold: 0.35, MinVolatilityBps: 1.25, VWAPZThreshold: 1.5, ABEnabled: true}
	if s != want {
		t.Fatalf("Apply result = %+v, want %+v", s, want)
	}
	if _, ok := s.Text("regime.other"); ok {
		t.Error("Text of an unknown key reported ok")
	}
}

func TestKeysAndIsKey(t *testing.T) {
	for _, key := range regimesettings.Keys() {
		if !regimesettings.IsKey(key) {
			t.Errorf("IsKey(%s) = false for a Keys() entry", key)
		}
	}
	if regimesettings.IsKey("regime.other") || regimesettings.IsKey("calibration.barrier_tp_bps") {
		t.Error("IsKey accepted a foreign key")
	}
	if !regimesettings.IsIntegerKey(regimesettings.KeyWindowMinutes) || !regimesettings.IsIntegerKey(regimesettings.KeyABEnabled) || regimesettings.IsIntegerKey(regimesettings.KeyVWAPZThreshold) {
		t.Error("IsIntegerKey classifies the keys wrongly")
	}
}

func TestLoad(t *testing.T) {
	ctx := context.Background()

	got, err := regimesettings.Load(ctx, fakeStore{})
	if err != nil || got != regimesettings.Default() {
		t.Fatalf("Load(empty) = %+v, %v, want the defaults", got, err)
	}

	got, err = regimesettings.Load(ctx, fakeStore{rows: map[string]string{
		regimesettings.KeyABEnabled: "1", regimesettings.KeyVWAPZThreshold: "2.5",
	}})
	if err != nil || !got.ABEnabled || got.VWAPZThreshold != 2.5 || got.Window != 30*time.Minute {
		t.Fatalf("Load(stored) = %+v, %v, want A/B on, z 2.5, default window", got, err)
	}

	// A damaged row falls back to that field's default; the others still apply.
	got, err = regimesettings.Load(ctx, fakeStore{rows: map[string]string{
		regimesettings.KeyWindowMinutes: `"abc"`, regimesettings.KeyABEnabled: "1",
	}})
	if err != nil || got.Window != 30*time.Minute || !got.ABEnabled {
		t.Fatalf("Load(damaged window) = %+v, %v, want default window, A/B on", got, err)
	}

	wantErr := errors.New("db down")
	if _, err := regimesettings.Load(ctx, fakeStore{err: wantErr}); !errors.Is(err, wantErr) {
		t.Fatalf("Load(store error) = %v, want %v", err, wantErr)
	}
}
