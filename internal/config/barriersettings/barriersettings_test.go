package barriersettings_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/barriersettings"
)

type fakeStore struct {
	rows map[string]string
	err  error
}

func (f fakeStore) Get(_ context.Context, key string) (string, bool, error) {
	v, ok := f.rows[key]
	return v, ok, f.err
}

func TestDefault_IsValid(t *testing.T) {
	b := barriersettings.Default()
	if err := b.Validate(); err != nil {
		t.Fatalf("Default().Validate() = %v", err)
	}
	if b.TakeProfitBps != 30 || b.StopLossBps != 30 || b.Timeout != 10*time.Minute || b.CostBps != 10 || b.TimeoutMinutes() != 10 {
		t.Fatalf("Default() = %+v, want TP 30 / SL 30 / 10m / cost 10", b)
	}
}

func TestApply(t *testing.T) {
	cases := []struct {
		name, key, value string
		wantErr          bool
	}{
		{"tp", barriersettings.KeyTakeProfitBps, "45.5", false},
		{"tp max", barriersettings.KeyTakeProfitBps, "1000", false},
		{"tp zero", barriersettings.KeyTakeProfitBps, "0", true},
		{"tp negative", barriersettings.KeyTakeProfitBps, "-1", true},
		{"tp above max", barriersettings.KeyTakeProfitBps, "1000.5", true},
		{"sl", barriersettings.KeyStopLossBps, "20", false},
		{"sl zero", barriersettings.KeyStopLossBps, "0", true},
		{"sl above max", barriersettings.KeyStopLossBps, "1001", true},
		{"cost zero", barriersettings.KeyCostBps, "0", false},
		{"cost negative", barriersettings.KeyCostBps, "-0.1", true},
		{"cost above max", barriersettings.KeyCostBps, "1001", true},
		{"timeout", barriersettings.KeyTimeoutMinutes, "15", false},
		{"timeout min", barriersettings.KeyTimeoutMinutes, "1", false},
		{"timeout max", barriersettings.KeyTimeoutMinutes, "60", false},
		{"timeout zero", barriersettings.KeyTimeoutMinutes, "0", true},
		{"timeout above max", barriersettings.KeyTimeoutMinutes, "61", true},
		{"timeout fraction", barriersettings.KeyTimeoutMinutes, "7.5", true},
		{"string", barriersettings.KeyTakeProfitBps, `"30"`, true},
		{"unknown key", "calibration.other", "5", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b := barriersettings.Default()
			err := barriersettings.Apply(&b, c.key, c.value)
			if (err != nil) != c.wantErr {
				t.Fatalf("Apply(%s, %s) error = %v, wantErr %v", c.key, c.value, err, c.wantErr)
			}
		})
	}
}

func TestApply_SetsField(t *testing.T) {
	b := barriersettings.Default()
	for key, value := range map[string]string{
		barriersettings.KeyTakeProfitBps: "45.5", barriersettings.KeyStopLossBps: "22",
		barriersettings.KeyTimeoutMinutes: "15", barriersettings.KeyCostBps: "6",
	} {
		if err := barriersettings.Apply(&b, key, value); err != nil {
			t.Fatalf("Apply(%s): %v", key, err)
		}
	}
	want := barriersettings.Barriers{TakeProfitBps: 45.5, StopLossBps: 22, Timeout: 15 * time.Minute, CostBps: 6}
	if b != want {
		t.Fatalf("Apply result = %+v, want %+v", b, want)
	}
}

func TestValidate(t *testing.T) {
	ok := barriersettings.Default()
	for name, mutate := range map[string]func(*barriersettings.Barriers){
		"cost == sl":      func(b *barriersettings.Barriers) { b.CostBps = b.StopLossBps },
		"cost > sl":       func(b *barriersettings.Barriers) { b.CostBps = 40 },
		"zero tp":         func(b *barriersettings.Barriers) { b.TakeProfitBps = 0 },
		"zero sl":         func(b *barriersettings.Barriers) { b.StopLossBps = 0 },
		"negative cost":   func(b *barriersettings.Barriers) { b.CostBps = -1 },
		"zero timeout":    func(b *barriersettings.Barriers) { b.Timeout = 0 },
		"timeout too big": func(b *barriersettings.Barriers) { b.Timeout = 61 * time.Minute },
		"fractional min":  func(b *barriersettings.Barriers) { b.Timeout = 90 * time.Second },
	} {
		b := ok
		mutate(&b)
		if err := b.Validate(); err == nil {
			t.Errorf("%s: Validate() = nil, want an error", name)
		}
	}
	boundary := barriersettings.Barriers{TakeProfitBps: 1000, StopLossBps: 1000, Timeout: time.Minute, CostBps: 999.9}
	if err := boundary.Validate(); err != nil {
		t.Errorf("boundary values: Validate() = %v", err)
	}
}

func TestText(t *testing.T) {
	b := barriersettings.Default()
	for key, want := range map[string]string{
		barriersettings.KeyTakeProfitBps: "30", barriersettings.KeyStopLossBps: "30",
		barriersettings.KeyTimeoutMinutes: "10", barriersettings.KeyCostBps: "10",
	} {
		if got, ok := b.Text(key); !ok || got != want {
			t.Errorf("Text(%s) = %q, %v, want %q", key, got, ok, want)
		}
	}
	if _, ok := b.Text("nope"); ok {
		t.Error("Text(unknown) ok = true")
	}
}

func TestLoad(t *testing.T) {
	ctx := context.Background()

	got, err := barriersettings.Load(ctx, fakeStore{})
	if err != nil || got != barriersettings.Default() {
		t.Fatalf("Load(empty) = %+v, %v, want defaults", got, err)
	}

	got, err = barriersettings.Load(ctx, fakeStore{rows: map[string]string{
		barriersettings.KeyTakeProfitBps: "50", barriersettings.KeyTimeoutMinutes: "5",
	}})
	want := barriersettings.Barriers{TakeProfitBps: 50, StopLossBps: 30, Timeout: 5 * time.Minute, CostBps: 10}
	if err != nil || got != want {
		t.Fatalf("Load(partial) = %+v, %v, want %+v", got, err, want)
	}

	// A damaged / inconsistent stored set falls back to the defaults.
	for name, rows := range map[string]map[string]string{
		"unparsable":    {barriersettings.KeyTakeProfitBps: `"x"`},
		"cost >= sl":    {barriersettings.KeyCostBps: "30"},
		"out of bounds": {barriersettings.KeyTimeoutMinutes: "1000"},
	} {
		got, err = barriersettings.Load(ctx, fakeStore{rows: rows})
		if err != nil || got != barriersettings.Default() {
			t.Errorf("Load(%s) = %+v, %v, want defaults", name, got, err)
		}
	}

	boom := errors.New("boom")
	if _, err = barriersettings.Load(ctx, fakeStore{err: boom}); !errors.Is(err, boom) {
		t.Fatalf("Load(store error) = %v, want %v", err, boom)
	}
}
