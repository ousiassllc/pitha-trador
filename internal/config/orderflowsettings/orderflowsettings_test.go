package orderflowsettings_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/orderflowsettings"
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
	w := orderflowsettings.Default()
	if err := w.Validate(); err != nil {
		t.Fatalf("Default().Validate() = %v", err)
	}
	if w.Short != 10*time.Second || w.Mid != 30*time.Second || w.Long != 60*time.Second {
		t.Fatalf("Default() = %+v, want 10s/30s/60s", w)
	}
}

func TestApply(t *testing.T) {
	cases := []struct {
		name, key, value string
		wantErr          bool
	}{
		{"short", orderflowsettings.KeyShort, "5", false},
		{"max", orderflowsettings.KeyLong, "600", false},
		{"zero", orderflowsettings.KeyShort, "0", true},
		{"negative", orderflowsettings.KeyShort, "-3", true},
		{"too wide", orderflowsettings.KeyLong, "601", true},
		{"fraction", orderflowsettings.KeyMid, "1.5", true},
		{"string", orderflowsettings.KeyMid, `"30"`, true},
		{"unknown key", "orderflow.other", "5", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := orderflowsettings.Default()
			err := orderflowsettings.Apply(&w, c.key, c.value)
			if (err != nil) != c.wantErr {
				t.Fatalf("Apply(%s, %s) error = %v, wantErr %v", c.key, c.value, err, c.wantErr)
			}
		})
	}
}

func TestValidate_Ordering(t *testing.T) {
	s := time.Second
	for name, w := range map[string]orderflowsettings.Windows{
		"short == mid":   {Short: 30 * s, Mid: 30 * s, Long: 60 * s},
		"mid > long":     {Short: 10 * s, Mid: 90 * s, Long: 60 * s},
		"zero":           {Short: 0, Mid: 30 * s, Long: 60 * s},
		"beyond maximum": {Short: 10 * s, Mid: 30 * s, Long: orderflowsettings.MaxWindow + s},
	} {
		if err := w.Validate(); err == nil {
			t.Errorf("%s: Validate() = nil, want an error", name)
		}
	}
	if err := (orderflowsettings.Windows{Short: s, Mid: 2 * s, Long: orderflowsettings.MaxWindow}).Validate(); err != nil {
		t.Errorf("boundary values: Validate() = %v", err)
	}
}

func TestSeconds(t *testing.T) {
	w := orderflowsettings.Default()
	for key, want := range map[string]int{orderflowsettings.KeyShort: 10, orderflowsettings.KeyMid: 30, orderflowsettings.KeyLong: 60} {
		if got, ok := w.Seconds(key); !ok || got != want {
			t.Errorf("Seconds(%s) = %d, %v, want %d", key, got, ok, want)
		}
	}
	if _, ok := w.Seconds("nope"); ok {
		t.Error("Seconds(unknown) ok = true")
	}
}

func TestLoad(t *testing.T) {
	ctx := context.Background()

	got, err := orderflowsettings.Load(ctx, fakeStore{})
	if err != nil || got != orderflowsettings.Default() {
		t.Fatalf("Load(empty) = %+v, %v, want defaults", got, err)
	}

	got, err = orderflowsettings.Load(ctx, fakeStore{rows: map[string]string{
		orderflowsettings.KeyShort: "5", orderflowsettings.KeyLong: "120",
	}})
	want := orderflowsettings.Windows{Short: 5 * time.Second, Mid: 30 * time.Second, Long: 120 * time.Second}
	if err != nil || got != want {
		t.Fatalf("Load(partial) = %+v, %v, want %+v", got, err, want)
	}

	// A damaged / inconsistent stored set falls back to the defaults.
	for name, rows := range map[string]map[string]string{
		"unparsable":    {orderflowsettings.KeyShort: `"x"`},
		"out of order":  {orderflowsettings.KeyShort: "45"},
		"out of bounds": {orderflowsettings.KeyLong: "100000"},
	} {
		got, err = orderflowsettings.Load(ctx, fakeStore{rows: rows})
		if err != nil || got != orderflowsettings.Default() {
			t.Errorf("Load(%s) = %+v, %v, want defaults", name, got, err)
		}
	}

	boom := errors.New("boom")
	if _, err = orderflowsettings.Load(ctx, fakeStore{err: boom}); !errors.Is(err, boom) {
		t.Fatalf("Load(store error) = %v, want %v", err, boom)
	}
}
