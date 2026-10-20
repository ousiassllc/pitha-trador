package papersettings_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/config/papersettings"
)

type mapStore struct {
	rows map[string]string
	err  error
}

func (s mapStore) Get(_ context.Context, key string) (string, bool, error) {
	v, ok := s.rows[key]
	return v, ok, s.err
}

func TestApply_Ranges(t *testing.T) {
	tests := []struct {
		key, value string
		ok         bool
	}{
		{papersettings.KeyInitialCapitalJPY, "500000", true},
		{papersettings.KeyInitialCapitalJPY, "0.5", true},
		{papersettings.KeyInitialCapitalJPY, "1000000000000", true},
		{papersettings.KeyInitialCapitalJPY, "0", false},
		{papersettings.KeyInitialCapitalJPY, "-1", false},
		{papersettings.KeyInitialCapitalJPY, "1000000000001", false},
		{papersettings.KeyMaxPositionPerSymbolPct, "100", true},
		{papersettings.KeyMaxPositionPerSymbolPct, "0.01", true},
		{papersettings.KeyMaxPositionPerSymbolPct, "0", false},
		{papersettings.KeyMaxPositionPerSymbolPct, "100.01", false},
		{papersettings.KeyMaxTotalExposurePct, "100", true},
		{papersettings.KeyMaxTotalExposurePct, "0", false},
		{papersettings.KeyMaxTotalExposurePct, "-5", false},
		{papersettings.KeyMaxTotalExposurePct, "101", false},
		{papersettings.KeyMaxTotalExposurePct, `"100"`, false},
		{papersettings.KeyMaxTotalExposurePct, "null", false},
		{"risk.paper.unknown", "1", false},
	}
	for _, tt := range tests {
		var o papersettings.Overrides
		err := papersettings.Apply(&o, tt.key, tt.value)
		if (err == nil) != tt.ok {
			t.Errorf("Apply(%s, %s) error = %v, want ok=%v", tt.key, tt.value, err, tt.ok)
		}
		if err != nil && o != (papersettings.Overrides{}) {
			t.Errorf("Apply(%s, %s) changed the overrides on error: %+v", tt.key, tt.value, o)
		}
	}
}

func TestEffective_StoredValuesOverlayYamlKeyByKey(t *testing.T) {
	base := config.RiskLimits{InitialCapital: 30_000_000, MaxPositionPerSymbolPct: 2, MaxTotalExposurePct: 20, MaxTradeLossPct: 0.25, MaxOpenPositions: 5}
	store := mapStore{rows: map[string]string{papersettings.KeyInitialCapitalJPY: "500000", papersettings.KeyMaxTotalExposurePct: "100"}}
	got, err := papersettings.Effective(context.Background(), store, base)
	if err != nil {
		t.Fatalf("Effective: %v", err)
	}
	want := base
	want.InitialCapital, want.MaxTotalExposurePct = 500_000, 100
	if got != want {
		t.Errorf("Effective = %+v, want %+v (per-symbol cap and every other limit from yaml)", got, want)
	}
}

func TestEffective_NothingStoredIsYaml(t *testing.T) {
	base := config.RiskLimits{InitialCapital: 500_000, MaxPositionPerSymbolPct: 100, MaxTotalExposurePct: 100}
	got, err := papersettings.Effective(context.Background(), mapStore{rows: map[string]string{}}, base)
	if err != nil || got != base {
		t.Errorf("Effective = %+v, %v; want %+v", got, err, base)
	}
}

func TestEffective_InvalidRowIsIgnoredAndStoreErrorIsReturned(t *testing.T) {
	base := config.RiskLimits{InitialCapital: 500_000, MaxPositionPerSymbolPct: 2, MaxTotalExposurePct: 20}
	bad := mapStore{rows: map[string]string{papersettings.KeyMaxPositionPerSymbolPct: "500", papersettings.KeyInitialCapitalJPY: "oops"}}
	got, err := papersettings.Effective(context.Background(), bad, base)
	if err != nil || got != base {
		t.Errorf("Effective with invalid rows = %+v, %v; want the yaml limits", got, err)
	}
	boom := errors.New("db down")
	if _, err := papersettings.Effective(context.Background(), mapStore{err: boom}, base); !errors.Is(err, boom) {
		t.Errorf("Effective store error = %v, want %v", err, boom)
	}
}

func TestOverlay_IsTheSameReadAsEffective(t *testing.T) {
	store := mapStore{rows: map[string]string{papersettings.KeyInitialCapitalJPY: "123456"}}
	got, err := papersettings.Overlay(store)(context.Background(), config.RiskLimits{InitialCapital: 1})
	if err != nil || got.InitialCapital != 123456 {
		t.Errorf("Overlay = %+v, %v", got, err)
	}
}
