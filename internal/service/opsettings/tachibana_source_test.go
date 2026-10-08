package opsettings_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config/tachibanasource"
	"github.com/ousiassllc/pitha-trador/internal/service/opsettings"
)

func TestSave_TachibanaSourceSettingsAreStoredAsNormalizedJSONScalars(t *testing.T) {
	store := &fakeStore{rows: map[string]string{}}
	svc := newService(store)
	ctx := context.Background()
	for key, raw := range map[string]string{
		tachibanasource.KeyTachibanaCandidateSource:          "Fixed",
		tachibanasource.KeyTachibanaFixedSymbols:             "7203 6758,130a",
		tachibanasource.KeyTachibanaNightlyRunTime:           "1:30",
		tachibanasource.KeyTachibanaNightlyRatePerSecond:     "0.5",
		tachibanasource.KeyTachibanaNightlyMarkets:           "growth,prime",
		tachibanasource.KeyTachibanaEventMaxConnects:         "8",
		tachibanasource.KeyTachibanaRestQuoteMinInterval:     "90",
		tachibanasource.TachibanaScreenTopNKey("turnover"):   "15",
		tachibanasource.TachibanaScreenWeightKey("turnover"): "2.5",
	} {
		if err := svc.Save(ctx, key, raw); err != nil {
			t.Fatalf("Save %s: %v", key, err)
		}
	}
	want := map[string]string{
		tachibanasource.KeyTachibanaCandidateSource:          `"fixed"`,
		tachibanasource.KeyTachibanaFixedSymbols:             `"7203,6758,130A"`,
		tachibanasource.KeyTachibanaNightlyRunTime:           `"01:30"`,
		tachibanasource.KeyTachibanaNightlyRatePerSecond:     `0.5`,
		tachibanasource.KeyTachibanaNightlyMarkets:           `"prime,growth"`,
		tachibanasource.KeyTachibanaEventMaxConnects:         `8`,
		tachibanasource.KeyTachibanaRestQuoteMinInterval:     `90`,
		tachibanasource.TachibanaScreenTopNKey("turnover"):   `15`,
		tachibanasource.TachibanaScreenWeightKey("turnover"): `2.5`,
	}
	for key, w := range want {
		if store.rows[key] != w {
			t.Errorf("row %s = %s, want %s", key, store.rows[key], w)
		}
	}
	if v, _ := svc.Get(ctx, tachibanasource.KeyTachibanaNightlyRatePerSecond); v.Current != "0.5" || v.Default != "1" || !v.Overridden {
		t.Errorf("nightly rate = %+v", v)
	}
	if v, _ := svc.Get(ctx, tachibanasource.KeyTachibanaCandidateSource); v.Current != "fixed" || v.Default != "daily_screen" {
		t.Errorf("candidate source = %+v", v)
	}
	if v, _ := svc.Get(ctx, tachibanasource.KeyTachibanaRestQuoteRequestsPerRound); v.Current != "1" || v.Overridden {
		t.Errorf("unset rest requests = %+v, want the default", v)
	}

	got, err := svc.TachibanaSource(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got.CandidateSource != tachibanasource.TachibanaSourceFixed || !slices.Equal(got.FixedSymbols, []string{"7203", "6758", "130A"}) ||
		got.Nightly.RunTime != "01:30" || got.Nightly.MaxPerSecond != 0.5 || !slices.Equal(got.Nightly.Markets, []string{"prime", "growth"}) ||
		got.EventMaxConnectsPerDay != 8 || got.RestQuote.MinIntervalSeconds != 90 || got.RestQuote.RequestsPerRound != 1 {
		t.Errorf("loaded = %+v", got)
	}
	var turnover tachibanasource.TachibanaScreenIndicator
	for _, ind := range got.Screen {
		if ind.Name == tachibanasource.ScreenTurnover {
			turnover = ind
		}
	}
	if turnover.TopN != 15 || turnover.Weight != 2.5 {
		t.Errorf("turnover indicator = %+v", turnover)
	}
}

func TestSave_TachibanaSourceNightlyRunTimeRejectsDaytime(t *testing.T) {
	for _, raw := range []string{"08:00", "09:30", "12:00", "15:30"} {
		store := &fakeStore{rows: map[string]string{}}
		err := newService(store).Save(context.Background(), tachibanasource.KeyTachibanaNightlyRunTime, raw)
		var invalid *opsettings.InvalidValueError
		if !errors.As(err, &invalid) || len(store.rows) != 0 {
			t.Errorf("Save(run_time, %q) = %v rows=%v, want *InvalidValueError and no write", raw, err, store.rows)
		}
	}
	store := &fakeStore{rows: map[string]string{}}
	for _, raw := range []string{"15:31", "18:00", "01:00"} {
		if err := newService(store).Save(context.Background(), tachibanasource.KeyTachibanaNightlyRunTime, raw); err != nil {
			t.Errorf("Save(run_time, %q) = %v, want nil", raw, err)
		}
	}
}

func TestSave_TachibanaSourceSettingsRejectBadValues(t *testing.T) {
	for key, raw := range map[string]string{
		tachibanasource.KeyTachibanaCandidateSource:           "ranking",
		tachibanasource.KeyTachibanaNightlyRatePerSecond:      "5",
		tachibanasource.KeyTachibanaNightlyMarkets:            "nyse",
		tachibanasource.KeyTachibanaManualSymbols:             "7203.T",
		tachibanasource.KeyTachibanaEventMaxConnects:          "0",
		tachibanasource.KeyTachibanaRestQuoteMinInterval:      "1",
		tachibanasource.KeyTachibanaRestQuoteRequestsPerRound: "9",
		tachibanasource.TachibanaScreenWeightKey("volume"):    "-1",
		tachibanasource.TachibanaScreenTopNKey("volume"):      "121",
	} {
		store := &fakeStore{rows: map[string]string{}}
		err := newService(store).Save(context.Background(), key, raw)
		var invalid *opsettings.InvalidValueError
		if !errors.As(err, &invalid) || len(store.rows) != 0 {
			t.Errorf("Save(%s, %q) = %v rows=%v, want *InvalidValueError and no write", key, raw, err, store.rows)
		}
	}
}

func TestSave_TachibanaScreenKeepsAtLeastOneActiveIndicator(t *testing.T) {
	ctx := context.Background()
	store := &fakeStore{rows: map[string]string{}}
	svc := newService(store)
	inds := tachibanasource.TachibanaScreenIndicators
	for _, ind := range inds[1:] {
		if err := svc.Save(ctx, tachibanasource.TachibanaScreenTopNKey(ind), "0"); err != nil {
			t.Fatalf("turning off %s: %v", ind, err)
		}
	}
	for _, key := range []string{tachibanasource.TachibanaScreenTopNKey(inds[0]), tachibanasource.TachibanaScreenWeightKey(inds[0])} {
		before := store.rows[key]
		var invalid *opsettings.InvalidValueError
		if err := svc.Save(ctx, key, "0"); !errors.As(err, &invalid) {
			t.Errorf("Save(%s, 0) = %v, want *InvalidValueError (the last active indicator)", key, err)
		}
		if store.rows[key] != before {
			t.Errorf("%s was written despite the rejection", key)
		}
	}
	if err := svc.Save(ctx, tachibanasource.TachibanaScreenTopNKey(inds[1]), "5"); err != nil {
		t.Fatal(err)
	}
	if err := svc.Save(ctx, tachibanasource.TachibanaScreenTopNKey(inds[0]), "0"); err != nil {
		t.Errorf("another indicator is active, turning off %s must pass: %v", inds[0], err)
	}
	if err := svc.Reset(ctx, tachibanasource.TachibanaScreenTopNKey(inds[0])); err != nil {
		t.Fatal(err)
	}
}

func TestLoadTachibanaSource_MalformedRowsFallBackAndStoreErrorFails(t *testing.T) {
	ctx := context.Background()
	got, err := opsettings.LoadTachibanaSource(ctx, &fakeStore{rows: map[string]string{
		tachibanasource.KeyTachibanaCandidateSource:      `not json`,
		tachibanasource.KeyTachibanaNightlyRunTime:       `"12:00"`,
		tachibanasource.KeyTachibanaEventMaxConnects:     `99`,
		tachibanasource.KeyTachibanaFixedSymbols:         `"7203.T"`,
		tachibanasource.KeyTachibanaNightlyRatePerSecond: `0.5`,
	}})
	if err != nil {
		t.Fatalf("a malformed row must not fail loading: %v", err)
	}
	if got.CandidateSource != tachibanasource.TachibanaSourceDailyScreen || got.Nightly.RunTime != "18:00" || got.EventMaxConnectsPerDay != 10 ||
		len(got.FixedSymbols) != 0 || got.Nightly.MaxPerSecond != 0.5 {
		t.Errorf("loaded = %+v, want defaults for the damaged rows and the valid row kept", got)
	}

	unusable := map[string]string{}
	for _, ind := range tachibanasource.TachibanaScreenIndicators {
		unusable[tachibanasource.TachibanaScreenTopNKey(ind)] = `0`
	}
	got, err = opsettings.LoadTachibanaSource(ctx, &fakeStore{rows: unusable})
	if err != nil || got.Validate() != nil {
		t.Errorf("an unusable stored screening must load as the defaults: %+v, %v", got, err)
	}
	if _, err := opsettings.LoadTachibanaSource(ctx, &fakeStore{getErr: errors.New("boom")}); err == nil {
		t.Error("a store read failure must be returned")
	}
}
