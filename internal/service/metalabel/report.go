package metalabel

import (
	"context"
	"fmt"

	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	repo "github.com/ousiassllc/pitha-trador/internal/repository/metalabel"
)

// ReportRepository is the part of the model store Reporter reads.
type ReportRepository interface {
	LatestModel(ctx context.Context) (repo.Model, bool, error)
	LatestActiveModel(ctx context.Context) (repo.Model, bool, error)
}

// Reporter builds the Calibration screen's view of the newest meta-labeling
// model (issue #766): its OOS AUC, reliability curve and EV, and the Jev
// evaluation period.
type Reporter struct {
	repo     ReportRepository
	settings func(context.Context) (metalabelsettings.Settings, error)
}

// NewReporter returns a Reporter reading models from r and the LLM cutoff from
// settings (at every call, so a Settings change shows without a restart).
func NewReporter(r ReportRepository, settings func(context.Context) (metalabelsettings.Settings, error)) *Reporter {
	return &Reporter{repo: r, settings: settings}
}

// Report returns the newest stored model's report, or (zero, false, nil) when
// no model was ever trained. The newest model may be a rejected one: its
// metrics are what the last retraining produced, and ActiveVersion names the
// model actually in force.
func (r *Reporter) Report(ctx context.Context) (domain.MetaModelReport, bool, error) {
	m, found, err := r.repo.LatestModel(ctx)
	if err != nil {
		return domain.MetaModelReport{}, false, fmt.Errorf("metalabel: load the latest model: %w", err)
	}
	if !found {
		return domain.MetaModelReport{}, false, nil
	}
	active, hasActive, err := r.repo.LatestActiveModel(ctx)
	if err != nil {
		return domain.MetaModelReport{}, false, fmt.Errorf("metalabel: load the active model: %w", err)
	}
	s, err := r.settings(ctx)
	if err != nil {
		return domain.MetaModelReport{}, false, fmt.Errorf("metalabel: read settings: %w", err)
	}

	bins := make([]domain.MetaModelCalibrationBin, len(m.OOSCalibration))
	for i, b := range m.OOSCalibration {
		bins[i] = domain.MetaModelCalibrationBin{MeanPredicted: b.MeanPredicted, ObservedRate: b.ObservedRate, Count: b.Count}
	}
	report := domain.MetaModelReport{
		Version: m.Version, Algorithm: m.Algorithm, Status: m.Status, RejectReason: m.RejectReason,
		TrainedAt: m.TrainedAt, TrainStart: m.TrainStart, TrainEnd: m.TrainEnd, OOSStart: m.OOSStart, OOSEnd: m.OOSEnd,
		TrainSamples: m.TrainSamples, OOSSamples: m.OOSSamples,
		OOSAUC: m.OOSAUC, OOSEVBps: m.OOSExpectedValueBps, OOSCalibration: bins,
		LLMCutoff: s.LLMCutoff,
	}
	if hasActive {
		report.ActiveVersion = active.Version
	}
	return report, true, nil
}
