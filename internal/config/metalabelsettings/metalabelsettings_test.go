package metalabelsettings_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
)

type fakeStore struct {
	rows map[string]string
	err  error
}

func (f fakeStore) Get(_ context.Context, key string) (string, bool, error) {
	if f.err != nil {
		return "", false, f.err
	}
	v, ok := f.rows[key]
	return v, ok, nil
}

func TestDefault_IsValidAndShown(t *testing.T) {
	d := metalabelsettings.Default()
	if err := d.Validate(); err != nil {
		t.Fatalf("Default().Validate() = %v", err)
	}
	for key, want := range map[string]string{
		metalabelsettings.KeyModel: "logistic_regression", metalabelsettings.KeyCalibration: "platt",
		metalabelsettings.KeyRetrainTime: "18:00", metalabelsettings.KeyMinEVBps: "0",
		metalabelsettings.KeyLLMCutoff: "2026-01-01",
	} {
		if got, ok := d.Text(key); !ok || got != want {
			t.Errorf("Text(%s) = %q, %v, want %q", key, got, ok, want)
		}
	}
	if _, ok := d.Text("metalabel.unknown"); ok {
		t.Error("Text accepted an unknown key")
	}
	if len(metalabelsettings.Keys()) != 8 {
		t.Errorf("Keys = %v", metalabelsettings.Keys())
	}
}

func TestNormalize(t *testing.T) {
	for _, tc := range []struct{ key, raw, want string }{
		{metalabelsettings.KeyModel, " Logistic_Regression ", "logistic_regression"},
		{metalabelsettings.KeyCalibration, "ISOTONIC", "isotonic"},
		{metalabelsettings.KeyRetrainTime, "1:05", "01:05"},
		{metalabelsettings.KeyRetrainTime, "18:00", "18:00"},
		{metalabelsettings.KeyRetrainTime, "01:30", "01:30"},
		{metalabelsettings.KeyRetrainTime, "16:30", "16:30"},
		{metalabelsettings.KeyRetrainTime, "07:59", "07:59"},
		{metalabelsettings.KeyLLMCutoff, " 2025-06-30 ", "2025-06-30"},
	} {
		got, err := metalabelsettings.Normalize(tc.key, tc.raw)
		if err != nil || got != tc.want {
			t.Errorf("Normalize(%s, %q) = %q, %v, want %q", tc.key, tc.raw, got, err, tc.want)
		}
	}
}

func TestNormalize_Rejects(t *testing.T) {
	for name, tc := range map[string]struct{ key, raw string }{
		"unknown model":       {metalabelsettings.KeyModel, "lightgbm"},
		"empty model":         {metalabelsettings.KeyModel, ""},
		"unknown calibration": {metalabelsettings.KeyCalibration, "beta"},
		"time with seconds":   {metalabelsettings.KeyRetrainTime, "18:00:00"},
		"time hour 24":        {metalabelsettings.KeyRetrainTime, "24:00"},
		"time minute 60":      {metalabelsettings.KeyRetrainTime, "18:60"},
		"time text":           {metalabelsettings.KeyRetrainTime, "evening"},
		"time empty":          {metalabelsettings.KeyRetrainTime, ""},
		"time in session":     {metalabelsettings.KeyRetrainTime, "10:00"},
		"window start":        {metalabelsettings.KeyRetrainTime, "08:00"},
		"window last minute":  {metalabelsettings.KeyRetrainTime, "16:29"},
		"number key":          {metalabelsettings.KeyMinEVBps, "1"},
		"cutoff text":         {metalabelsettings.KeyLLMCutoff, "last year"},
		"cutoff with time":    {metalabelsettings.KeyLLMCutoff, "2025-06-30T00:00:00Z"},
		"cutoff impossible":   {metalabelsettings.KeyLLMCutoff, "2025-02-30"},
		"cutoff empty":        {metalabelsettings.KeyLLMCutoff, ""},
	} {
		if got, err := metalabelsettings.Normalize(tc.key, tc.raw); err == nil {
			t.Errorf("%s: Normalize(%s, %q) = %q, want an error", name, tc.key, tc.raw, got)
		}
	}
}

func TestApply(t *testing.T) {
	s := metalabelsettings.Default()
	for key, raw := range map[string]string{
		metalabelsettings.KeyModel: `"logistic_regression"`, metalabelsettings.KeyCalibration: `"isotonic"`,
		metalabelsettings.KeyRetrainTime: `"21:30"`, metalabelsettings.KeyMinEVBps: `2.5`,
		metalabelsettings.KeyLLMCutoff: `"2025-09-30"`,
	} {
		if err := metalabelsettings.Apply(&s, key, raw); err != nil {
			t.Fatalf("Apply(%s, %s): %v", key, raw, err)
		}
	}
	if s.Calibration != "isotonic" || s.RetrainTime != "21:30" || s.MinEVBps != 2.5 || s.LLMCutoff != "2025-09-30" {
		t.Fatalf("settings = %+v", s)
	}
	if err := s.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestApply_RejectsInvalidStoredValues(t *testing.T) {
	for name, tc := range map[string]struct{ key, raw string }{
		"unknown key":          {"metalabel.unknown", `"x"`},
		"model not a string":   {metalabelsettings.KeyModel, `1`},
		"unknown model":        {metalabelsettings.KeyModel, `"lightgbm"`},
		"non-canonical case":   {metalabelsettings.KeyCalibration, `"Platt"`},
		"non-canonical time":   {metalabelsettings.KeyRetrainTime, `"9:00"`},
		"time in session":      {metalabelsettings.KeyRetrainTime, `"12:00"`},
		"min EV string":        {metalabelsettings.KeyMinEVBps, `"1"`},
		"min EV negative":      {metalabelsettings.KeyMinEVBps, `-0.5`},
		"min EV above max":     {metalabelsettings.KeyMinEVBps, `1000.1`},
		"min EV not json":      {metalabelsettings.KeyMinEVBps, `abc`},
		"time not json":        {metalabelsettings.KeyRetrainTime, `18:00`},
		"cutoff not a date":    {metalabelsettings.KeyLLMCutoff, `"yesterday"`},
		"cutoff not canonical": {metalabelsettings.KeyLLMCutoff, `"2025-6-30"`},
	} {
		s := metalabelsettings.Default()
		if err := metalabelsettings.Apply(&s, tc.key, tc.raw); err == nil {
			t.Errorf("%s: Apply(%s, %s) succeeded", name, tc.key, tc.raw)
		}
		if s != metalabelsettings.Default() {
			t.Errorf("%s: a rejected value changed the settings: %+v", name, s)
		}
	}
	s := metalabelsettings.Default()
	if err := metalabelsettings.Apply(&s, metalabelsettings.KeyMinEVBps, `1000`); err != nil {
		t.Errorf("min EV at the maximum rejected: %v", err)
	}
}

func TestValidate_NamesEveryBadKey(t *testing.T) {
	err := metalabelsettings.Settings{Model: "x", Calibration: "y", RetrainTime: "10:00", MinEVBps: -1, LLMCutoff: "soon",
		LLMCutoffModel: "a\nb", LLMCutoffConfirmedOn: "x", LLMCutoffSource: "a\tb"}.Validate()
	if err == nil {
		t.Fatal("Validate accepted bad settings")
	}
	for _, key := range metalabelsettings.Keys() {
		if !strings.Contains(err.Error(), key) {
			t.Errorf("error does not name %s: %v", key, err)
		}
	}
	if (metalabelsettings.Settings{Model: "logistic_regression", Calibration: "platt", RetrainTime: "9:00", MinEVBps: 0}).Validate() == nil {
		t.Error("Validate accepted a non-canonical time")
	}
}

func TestLoad(t *testing.T) {
	ctx := context.Background()

	got, err := metalabelsettings.Load(ctx, fakeStore{rows: map[string]string{}})
	if err != nil || got != metalabelsettings.Default() {
		t.Fatalf("empty store: %+v, %v, want the defaults", got, err)
	}

	got, err = metalabelsettings.Load(ctx, fakeStore{rows: map[string]string{
		metalabelsettings.KeyCalibration: `"isotonic"`, metalabelsettings.KeyMinEVBps: `3`,
	}})
	if err != nil || got.Calibration != "isotonic" || got.MinEVBps != 3 || got.RetrainTime != "18:00" {
		t.Fatalf("stored rows over defaults: %+v, %v", got, err)
	}

	// One bad row discards the whole stored set: never a half-applied one.
	got, err = metalabelsettings.Load(ctx, fakeStore{rows: map[string]string{
		metalabelsettings.KeyCalibration: `"isotonic"`, metalabelsettings.KeyRetrainTime: `"12:00"`,
	}})
	if err != nil || got != metalabelsettings.Default() {
		t.Fatalf("invalid stored set: %+v, %v, want the defaults", got, err)
	}

	boom := errors.New("db down")
	if _, err := metalabelsettings.Load(ctx, fakeStore{err: boom}); !errors.Is(err, boom) {
		t.Fatalf("store failure: %v, want %v", err, boom)
	}
}

func TestLLMCutoffTime(t *testing.T) {
	s := metalabelsettings.Default()
	s.LLMCutoff = "2025-09-30"
	got, err := s.LLMCutoffTime()
	if err != nil || !got.Equal(time.Date(2025, 9, 30, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("LLMCutoffTime = %v, %v, want 2025-09-30T00:00Z", got, err)
	}
	s.LLMCutoff = "not a date"
	if _, err := s.LLMCutoffTime(); err == nil {
		t.Error("LLMCutoffTime accepted a malformed date")
	}
}
