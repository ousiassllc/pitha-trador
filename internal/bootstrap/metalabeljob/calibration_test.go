package metalabeljob

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/metalabel"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/calibration"
)

// The Calibration handler picks the OOS report up by type assertion, so the
// composite must satisfy both of its interfaces.
var (
	_ calibration.CalibrationSource = CalibrationSource{}
	_ calibration.MetaModelSource   = CalibrationSource{}
	_ calibration.LLMCutoffSource   = CalibrationSource{}
)

func TestCalibrationSource_ReportsTheNewestStoredModelWithTheCurrentCutoff(t *testing.T) {
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "pitha.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	settings := system.NewRuntimeSettingsRepository(db)
	src := NewCalibrationSource(nil, db, settings)
	ctx := context.Background()

	if _, found, err := src.Report(ctx); err != nil || found {
		t.Fatalf("no model yet: found %v, err %v, want not found", found, err)
	}

	auc := 0.6
	trained := time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)
	if _, err := metalabel.NewRepository(db).InsertModel(ctx, metalabel.Model{
		Version: "v1", Algorithm: "logistic", Status: metalabel.ModelActive, TrainedAt: trained,
		TrainStart: trained.AddDate(0, 0, -30), TrainEnd: trained.AddDate(0, 0, -7), OOSStart: trained.AddDate(0, 0, -7), OOSEnd: trained,
		LabelVersion: domain.CalibrationLabelVersionTripleBarrier, FeatureNames: []string{"ofi_short"}, ParamsJSON: `{}`,
		TrainSamples: 100, OOSSamples: 50, OOSAUC: &auc,
	}); err != nil {
		t.Fatalf("InsertModel: %v", err)
	}
	if err := settings.Set(ctx, metalabelsettings.KeyLLMCutoff, `"2026-08-01"`, time.Now()); err != nil {
		t.Fatalf("Set cutoff: %v", err)
	}

	got, found, err := src.Report(ctx)
	if err != nil || !found {
		t.Fatalf("Report = found %v, err %v", found, err)
	}
	if got.Version != "v1" || got.ActiveVersion != "v1" || got.LLMCutoff != "2026-08-01" || got.OOSAUC == nil || *got.OOSAUC != auc {
		t.Errorf("report = %+v, want v1 active, cutoff 2026-08-01, AUC %v", got, auc)
	}
}

func TestCalibrationSource_CutoffFollowsTheStoredSettings(t *testing.T) {
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "pitha.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	settings := system.NewRuntimeSettingsRepository(db)
	src := NewCalibrationSource(nil, db, settings)
	ctx := context.Background()

	got, err := src.Cutoff(ctx)
	if err != nil || got.Status != metalabelsettings.CutoffPlaceholder || got.Warning == "" {
		t.Fatalf("default cutoff = %+v, %v, want the placeholder warning", got, err)
	}
	for key, raw := range map[string]string{
		metalabelsettings.KeyLLMCutoff: `"2026-08-01"`, metalabelsettings.KeyLLMCutoffModel: `"jev-x"`,
	} {
		if err := settings.Set(ctx, key, raw, time.Now()); err != nil {
			t.Fatalf("Set %s: %v", key, err)
		}
	}
	got, err = src.Cutoff(ctx)
	if err != nil || got.Status != metalabelsettings.CutoffConfirmed || got.Warning != "" || got.Cutoff != "2026-08-01" || got.Model != "jev-x" {
		t.Fatalf("confirmed cutoff = %+v, %v", got, err)
	}
}
