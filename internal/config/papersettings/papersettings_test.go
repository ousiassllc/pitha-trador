package papersettings_test

import (
	"context"
	"errors"
	"math"
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
		{papersettings.KeyMaxTradeLossPct, "1", true},
		{papersettings.KeyMaxTradeLossPct, "0.1", true},
		{papersettings.KeyMaxTradeLossPct, "3", true},
		{papersettings.KeyMaxTradeLossPct, "3.0", true},
		{papersettings.KeyMaxTradeLossPct, "0.09", false},
		{papersettings.KeyMaxTradeLossPct, "0", false},
		{papersettings.KeyMaxTradeLossPct, "3.01", false},
		{papersettings.KeyMaxTradeLossPct, "-1", false},
		{papersettings.KeyMaxTradeLossPct, `"1"`, false},
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

// Validate is the one range check shared by every writer of a Paper key (the
// Settings screen, the later MCP write whitelist and settings_get/diff): it
// takes the bounds from the exported constants, not from copies of the numbers.
func TestValidate_UsesTheExportedTradeLossRange(t *testing.T) {
	if err := papersettings.Validate(papersettings.KeyMaxTradeLossPct, papersettings.MinTradeLossPct); err != nil {
		t.Errorf("the lower bound is rejected: %v", err)
	}
	if err := papersettings.Validate(papersettings.KeyMaxTradeLossPct, papersettings.MaxTradeLossPct); err != nil {
		t.Errorf("the upper bound is rejected: %v", err)
	}
	if err := papersettings.Validate(papersettings.KeyMaxTradeLossPct, math.Nextafter(papersettings.MinTradeLossPct, 0)); err == nil {
		t.Error("just below the lower bound is accepted")
	}
	if err := papersettings.Validate(papersettings.KeyMaxTradeLossPct, math.Nextafter(papersettings.MaxTradeLossPct, 100)); err == nil {
		t.Error("just above the upper bound is accepted")
	}
	if err := papersettings.Validate(papersettings.KeyMaxTradeLossPct, math.NaN()); err == nil {
		t.Error("NaN is accepted")
	}
	if err := papersettings.Validate("risk.paper.unknown", 1); err == nil {
		t.Error("an unknown key is accepted")
	}
	if got := papersettings.Keys(); len(got) != 4 || got[3] != papersettings.KeyMaxTradeLossPct {
		t.Errorf("Keys = %v, want the four Paper keys ending in max_trade_loss_pct", got)
	}
	if papersettings.DefaultMaxTradeLossPct < papersettings.MinTradeLossPct || papersettings.DefaultMaxTradeLossPct > papersettings.MaxTradeLossPct {
		t.Errorf("the default %v is outside its own range", papersettings.DefaultMaxTradeLossPct)
	}
}

func TestEffective_TradeLossOverlay(t *testing.T) {
	base := config.RiskLimits{InitialCapital: 500_000, MaxTradeLossPct: 1}
	got, err := papersettings.Effective(context.Background(), mapStore{rows: map[string]string{papersettings.KeyMaxTradeLossPct: "2.5"}}, base)
	if err != nil || got.MaxTradeLossPct != 2.5 || got.InitialCapital != 500_000 {
		t.Errorf("Effective = %+v, %v", got, err)
	}
	got, err = papersettings.Effective(context.Background(), mapStore{rows: map[string]string{papersettings.KeyMaxTradeLossPct: "5"}}, base)
	if err != nil || got != base {
		t.Errorf("an out-of-range row must be ignored: %+v, %v", got, err)
	}
}
