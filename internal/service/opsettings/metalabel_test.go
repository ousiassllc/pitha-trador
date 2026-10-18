package opsettings_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
	"github.com/ousiassllc/pitha-trador/internal/service/opsettings"
)

func TestMetaLabel_DefaultsSaveAndReset(t *testing.T) {
	ctx := context.Background()
	store := &fakeStore{rows: map[string]string{}}
	svc := newService(store)

	for key, want := range map[string]string{
		metalabelsettings.KeyModel: "logistic_regression", metalabelsettings.KeyCalibration: "platt",
		metalabelsettings.KeyRetrainTime: "18:00", metalabelsettings.KeyMinEVBps: "0",
		metalabelsettings.KeyLLMCutoff: "2026-01-01",
	} {
		v, err := svc.Get(ctx, key)
		if err != nil || v.Current != want || v.Default != want || v.Overridden {
			t.Fatalf("Get(%s) = %+v, %v, want %s not overridden", key, v, err, want)
		}
	}

	for key, tc := range map[string]struct{ raw, stored, shown string }{
		metalabelsettings.KeyModel:       {" Logistic_Regression ", `"logistic_regression"`, "logistic_regression"},
		metalabelsettings.KeyCalibration: {"isotonic", `"isotonic"`, "isotonic"},
		metalabelsettings.KeyRetrainTime: {"2:30", `"02:30"`, "02:30"},
		metalabelsettings.KeyMinEVBps:    {" 4.5 ", `4.5`, "4.5"},
		metalabelsettings.KeyLLMCutoff:   {" 2025-09-30 ", `"2025-09-30"`, "2025-09-30"},
	} {
		if err := svc.Save(ctx, key, tc.raw); err != nil {
			t.Fatalf("Save(%s, %q): %v", key, tc.raw, err)
		}
		if got := store.rows[key]; got != tc.stored {
			t.Errorf("stored %s = %q, want %q", key, got, tc.stored)
		}
		if v, err := svc.Get(ctx, key); err != nil || v.Current != tc.shown || !v.Overridden {
			t.Errorf("Get(%s) after save = %+v, %v, want %s overridden", key, v, err, tc.shown)
		}
	}

	// What the Settings screen stored is accepted by the start-up/use-time load.
	got, err := metalabelsettings.Load(ctx, store)
	if err != nil || got.Calibration != "isotonic" || got.RetrainTime != "02:30" || got.MinEVBps != 4.5 || got.LLMCutoff != "2025-09-30" {
		t.Fatalf("Load after Save = %+v, %v", got, err)
	}

	if err := svc.Reset(ctx, metalabelsettings.KeyMinEVBps); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	if _, ok := store.rows[metalabelsettings.KeyMinEVBps]; ok {
		t.Fatal("Reset left the row")
	}
}

func TestMetaLabel_SaveRejectsInvalid(t *testing.T) {
	ctx := context.Background()
	store := &fakeStore{rows: map[string]string{}}
	svc := newService(store)

	for name, tc := range map[string]struct{ key, raw string }{
		"unknown model":       {metalabelsettings.KeyModel, "lightgbm"},
		"unknown calibration": {metalabelsettings.KeyCalibration, "beta"},
		"time text":           {metalabelsettings.KeyRetrainTime, "evening"},
		"time minute 60":      {metalabelsettings.KeyRetrainTime, "18:60"},
		"time in session":     {metalabelsettings.KeyRetrainTime, "10:00"},
		"min EV text":         {metalabelsettings.KeyMinEVBps, "abc"},
		"min EV negative":     {metalabelsettings.KeyMinEVBps, "-1"},
		"min EV above max":    {metalabelsettings.KeyMinEVBps, "1001"},
		"min EV NaN":          {metalabelsettings.KeyMinEVBps, "NaN"},
		"min EV infinite":     {metalabelsettings.KeyMinEVBps, "Inf"},
		"cutoff text":         {metalabelsettings.KeyLLMCutoff, "last year"},
		"cutoff impossible":   {metalabelsettings.KeyLLMCutoff, "2025-13-01"},
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

// issue #776: the LLM cutoff row carries a warning while it is the placeholder
// (or has no recorded Jev model) and none once both are stored.
func TestMetaLabel_LLMCutoffWarning(t *testing.T) {
	ctx := context.Background()
	store := &fakeStore{rows: map[string]string{}}
	svc := newService(store)
	warning := func() string {
		v, err := svc.Get(ctx, metalabelsettings.KeyLLMCutoff)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		return v.Warning
	}

	if warning() == "" {
		t.Error("the placeholder cutoff has no warning")
	}
	if err := svc.Save(ctx, metalabelsettings.KeyLLMCutoff, "2026-09-01"); err != nil {
		t.Fatal(err)
	}
	if warning() == "" {
		t.Error("a real cutoff with no recorded Jev model has no warning")
	}
	if err := svc.Save(ctx, metalabelsettings.KeyLLMCutoffModel, "jev-x"); err != nil {
		t.Fatal(err)
	}
	if w := warning(); w != "" {
		t.Errorf("confirmed cutoff still warns: %s", w)
	}
}
