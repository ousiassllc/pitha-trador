package opsettings_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config/barriersettings"
	"github.com/ousiassllc/pitha-trador/internal/service/opsettings"
)

func TestTripleBarrier_DefaultsAndSave(t *testing.T) {
	ctx := context.Background()
	store := &fakeStore{rows: map[string]string{}}
	svc := newService(store)

	for key, want := range map[string]string{
		barriersettings.KeyTakeProfitBps: "30", barriersettings.KeyStopLossBps: "30",
		barriersettings.KeyTimeoutMinutes: "10", barriersettings.KeyCostBps: "10",
	} {
		v, err := svc.Get(ctx, key)
		if err != nil || v.Current != want || v.Default != want || v.Overridden {
			t.Fatalf("Get(%s) = %+v, %v, want %s not overridden", key, v, err, want)
		}
	}

	if err := svc.Save(ctx, barriersettings.KeyTakeProfitBps, " 45.5 "); err != nil {
		t.Fatalf("Save tp: %v", err)
	}
	if got := store.rows[barriersettings.KeyTakeProfitBps]; got != "45.5" {
		t.Fatalf("stored tp = %q, want 45.5", got)
	}
	if err := svc.Save(ctx, barriersettings.KeyTimeoutMinutes, "15"); err != nil {
		t.Fatalf("Save timeout: %v", err)
	}
	v, _ := svc.Get(ctx, barriersettings.KeyTakeProfitBps)
	if v.Current != "45.5" || !v.Overridden || v.Default != "30" {
		t.Fatalf("Get after save = %+v", v)
	}

	if err := svc.Reset(ctx, barriersettings.KeyTakeProfitBps); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	if _, ok := store.rows[barriersettings.KeyTakeProfitBps]; ok {
		t.Fatal("Reset left the row")
	}
}

func TestTripleBarrier_SaveRejectsInvalid(t *testing.T) {
	ctx := context.Background()
	store := &fakeStore{rows: map[string]string{}}
	svc := newService(store)

	for name, tc := range map[string]struct{ key, raw string }{
		"text":                  {barriersettings.KeyTakeProfitBps, "abc"},
		"tp zero":               {barriersettings.KeyTakeProfitBps, "0"},
		"tp above maximum":      {barriersettings.KeyTakeProfitBps, "1001"},
		"sl negative":           {barriersettings.KeyStopLossBps, "-5"},
		"cost negative":         {barriersettings.KeyCostBps, "-1"},
		"cost above maximum":    {barriersettings.KeyCostBps, "1001"},
		"timeout not integer":   {barriersettings.KeyTimeoutMinutes, "7.5"},
		"timeout zero":          {barriersettings.KeyTimeoutMinutes, "0"},
		"timeout above maximum": {barriersettings.KeyTimeoutMinutes, "61"},
		"cost >= default sl":    {barriersettings.KeyCostBps, "30"},
		"sl <= default cost":    {barriersettings.KeyStopLossBps, "10"},
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
