// Package costflow_test checks the cost settings (issue #779) saved through
// opsettings.Service. It lives in its own directory to keep
// internal/service/opsettings within the linterly directory budget.
package costflow_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config"

	"github.com/ousiassllc/pitha-trador/internal/config/barriersettings"
	"github.com/ousiassllc/pitha-trador/internal/config/costsettings"
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

func newService(store *fakeStore) *opsettings.Service {
	return opsettings.New(store, config.StrategyConfig{}, "/data/logs")
}

func TestCost_DefaultsAndSave(t *testing.T) {
	ctx := context.Background()
	store := &fakeStore{rows: map[string]string{}}
	svc := newService(store)

	for key, want := range map[string]string{
		costsettings.KeyFeeBps: "0", costsettings.KeyFeeFixedJPY: "0",
		costsettings.KeySlippageLongContinuous: "2", costsettings.KeySlippageShortContinuous: "2",
		costsettings.KeySlippageLongOpening: "5", costsettings.KeySlippageShortClosing: "5",
		costsettings.KeyShortBorrowBps: "0", costsettings.KeyReferenceNotionalJPY: "1e+06",
	} {
		v, err := svc.Get(ctx, key)
		if err != nil || v.Current != want || v.Default != want || v.Overridden {
			t.Fatalf("Get(%s) = %+v, %v, want %s not overridden", key, v, err, want)
		}
	}
	if err := svc.Save(ctx, costsettings.KeyFeeBps, " 1.5 "); err != nil {
		t.Fatalf("Save fee: %v", err)
	}
	if got := store.rows[costsettings.KeyFeeBps]; got != "1.5" {
		t.Fatalf("stored fee = %q, want 1.5", got)
	}
	if v, _ := svc.Get(ctx, costsettings.KeyFeeBps); v.Current != "1.5" || !v.Overridden {
		t.Fatalf("Get after save = %+v", v)
	}
	if err := svc.Reset(ctx, costsettings.KeyFeeBps); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	if _, ok := store.rows[costsettings.KeyFeeBps]; ok {
		t.Fatal("Reset left the row")
	}
}

func TestCost_SaveRejectsInvalid(t *testing.T) {
	ctx := context.Background()
	store := &fakeStore{rows: map[string]string{}}
	svc := newService(store)
	for name, tc := range map[string]struct{ key, raw string }{
		"text":                 {costsettings.KeyFeeBps, "abc"},
		"fee negative":         {costsettings.KeyFeeBps, "-1"},
		"fixed above max":      {costsettings.KeyFeeFixedJPY, "100001"},
		"slippage negative":    {costsettings.KeySlippageShortClosing, "-0.5"},
		"reference zero":       {costsettings.KeyReferenceNotionalJPY, "0"},
		"label cost above SL":  {costsettings.KeyFeeBps, "10"}, // +20 bps round trip: label cost 30 >= SL 30
		"short borrow too big": {costsettings.KeyShortBorrowBps, "20"},
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

// A barrier setting is rejected when, under the stored cost settings, the label
// cost would reach the stop-loss width.
func TestCost_BarrierSaveChecksTheLabelCost(t *testing.T) {
	ctx := context.Background()
	store := &fakeStore{rows: map[string]string{}}
	svc := newService(store)
	if err := svc.Save(ctx, costsettings.KeyFeeBps, "5"); err != nil { // label cost 10 + 10 = 20
		t.Fatalf("Save fee: %v", err)
	}
	var invalid *opsettings.InvalidValueError
	if err := svc.Save(ctx, barriersettings.KeyStopLossBps, "20"); !errors.As(err, &invalid) { // SL 20 <= label cost 20
		t.Fatalf("Save SL 20 error = %v, want *InvalidValueError", err)
	}
	if err := svc.Save(ctx, barriersettings.KeyStopLossBps, "25"); err != nil {
		t.Fatalf("Save SL 25: %v", err)
	}
}

// Reset is checked like Save: with SL 60 and a 15 bps fee (label cost 10 + 30 =
// 40) the default SL of 30 would sit below the label cost, so resetting SL is
// rejected and the stored SL stays. Resetting the fee, which only lowers the
// cost, still works.
func TestCost_ResetChecksTheEffectiveCombination(t *testing.T) {
	ctx := context.Background()
	store := &fakeStore{rows: map[string]string{}}
	svc := newService(store)
	if err := svc.Save(ctx, barriersettings.KeyStopLossBps, "60"); err != nil {
		t.Fatalf("Save SL: %v", err)
	}
	if err := svc.Save(ctx, costsettings.KeyFeeBps, "15"); err != nil {
		t.Fatalf("Save fee: %v", err)
	}

	var invalid *opsettings.InvalidValueError
	if err := svc.Reset(ctx, barriersettings.KeyStopLossBps); !errors.As(err, &invalid) {
		t.Fatalf("Reset SL error = %v, want *InvalidValueError", err)
	}
	if got := store.rows[barriersettings.KeyStopLossBps]; got != "60" {
		t.Fatalf("stored SL after rejected Reset = %q, want 60", got)
	}

	if err := svc.Reset(ctx, costsettings.KeyFeeBps); err != nil {
		t.Fatalf("Reset fee: %v", err)
	}
	if err := svc.Reset(ctx, barriersettings.KeyStopLossBps); err != nil {
		t.Fatalf("Reset SL after the fee is back at its default: %v", err)
	}
	if len(store.rows) != 0 {
		t.Fatalf("rows left after both Resets: %v", store.rows)
	}
}
