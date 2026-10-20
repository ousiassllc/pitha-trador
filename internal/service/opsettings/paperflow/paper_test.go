// Package paperflow_test checks the Paper capital / exposure-limit settings
// saved through opsettings.Service (issue #804). It lives in its own
// directory to keep internal/service/opsettings within the linterly
// directory budget.
package paperflow_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/config/papersettings"
	"github.com/ousiassllc/pitha-trador/internal/service/opsettings"
)

func TestPaperSettings_DefaultsAreTheRiskYamlPaperValues(t *testing.T) {
	ctx := context.Background()
	svc := newService(&fakeStore{rows: map[string]string{}}).
		WithRiskReference(config.RiskLimits{InitialCapital: 30_000_000, MaxPositionPerSymbolPct: 2, MaxTotalExposurePct: 20, MaxDailyLossPct: 1})

	for key, want := range map[string]string{
		papersettings.KeyInitialCapitalJPY:       "30000000",
		papersettings.KeyMaxPositionPerSymbolPct: "2",
		papersettings.KeyMaxTotalExposurePct:     "20",
	} {
		v, err := svc.Get(ctx, key)
		if err != nil || v.Current != want || v.Default != want || v.Overridden {
			t.Errorf("Get(%s) = %+v, %v; want %s not overridden", key, v, err, want)
		}
	}
}

func TestPaperSettings_WithoutRiskYamlDefaultToTheIssue804Values(t *testing.T) {
	svc := newService(&fakeStore{rows: map[string]string{}})
	limits, err := svc.PaperLimits(context.Background())
	if err != nil || limits.InitialCapital != 500_000 || limits.MaxPositionPerSymbolPct != 100 || limits.MaxTotalExposurePct != 100 {
		t.Fatalf("PaperLimits = %+v, %v; want 500000/100/100", limits, err)
	}
}

func TestPaperSettings_SaveValidatesAndStoresThenResetRestoresYaml(t *testing.T) {
	ctx := context.Background()
	store := &fakeStore{rows: map[string]string{}}
	svc := newService(store).WithRiskReference(config.RiskLimits{InitialCapital: 30_000_000, MaxPositionPerSymbolPct: 2, MaxTotalExposurePct: 20})

	if err := svc.Save(ctx, papersettings.KeyInitialCapitalJPY, " 500000 "); err != nil {
		t.Fatalf("Save capital: %v", err)
	}
	if got := store.rows[papersettings.KeyInitialCapitalJPY]; got != "500000" {
		t.Fatalf("stored = %q", got)
	}
	limits, err := svc.PaperLimits(ctx)
	if err != nil || limits.InitialCapital != 500_000 || limits.MaxPositionPerSymbolPct != 2 {
		t.Fatalf("PaperLimits = %+v, %v; want the saved capital over yaml's other values", limits, err)
	}

	for _, tc := range []struct{ key, raw string }{
		{papersettings.KeyInitialCapitalJPY, "0"},
		{papersettings.KeyInitialCapitalJPY, "-5"},
		{papersettings.KeyMaxPositionPerSymbolPct, "0"},
		{papersettings.KeyMaxPositionPerSymbolPct, "100.1"},
		{papersettings.KeyMaxTotalExposurePct, "101"},
		{papersettings.KeyMaxTotalExposurePct, "abc"},
	} {
		var invalid *opsettings.InvalidValueError
		if err := svc.Save(ctx, tc.key, tc.raw); !errors.As(err, &invalid) {
			t.Errorf("Save(%s, %q) = %v, want *InvalidValueError", tc.key, tc.raw, err)
		}
	}
	if len(store.rows) != 1 {
		t.Errorf("rejected values were stored: %v", store.rows)
	}

	if err := svc.Reset(ctx, papersettings.KeyInitialCapitalJPY); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	if limits, _ := svc.PaperLimits(ctx); limits.InitialCapital != 30_000_000 {
		t.Errorf("after Reset capital = %v, want risk.yaml's 30000000", limits.InitialCapital)
	}
}

// The capital of the daily guard consistency check follows the Paper capital
// in force, so aligning the two on the Settings screen clears the warning.
func TestRiskReference_FollowsTheSavedPaperCapital(t *testing.T) {
	ctx := context.Background()
	svc := newService(&fakeStore{rows: map[string]string{}}).WithRiskReference(config.RiskLimits{InitialCapital: 30_000_000, MaxDailyLossPct: 1})
	if err := svc.Save(ctx, papersettings.KeyInitialCapitalJPY, "500000"); err != nil {
		t.Fatal(err)
	}
	ref, ok, err := svc.RiskReference(ctx)
	if err != nil || !ok || ref.InitialCapitalJPY != 500_000 || ref.MaxDailyLossPct != 1 {
		t.Fatalf("RiskReference = %+v, %v, %v", ref, ok, err)
	}
}

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
