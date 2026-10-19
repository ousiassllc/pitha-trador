package metalabel

import (
	"context"
	"fmt"

	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain/metamodel"
	repo "github.com/ousiassllc/pitha-trador/internal/repository/metalabel"
)

// ReportRepository is the part of the model store Reporter reads.
type ReportRepository interface {
	LatestModel(ctx context.Context) (repo.Model, bool, error)
	LatestActiveModel(ctx context.Context) (repo.Model, bool, error)
	ListRetrainSkips(ctx context.Context, limit int) ([]repo.RetrainSkip, error)
}

// RetrainSkipLimit is how many of the newest skipped retrainings the Calibration
// screen lists.
const RetrainSkipLimit = 10

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
func (r *Reporter) Report(ctx context.Context) (metamodel.Report, bool, error) {
	m, found, err := r.repo.LatestModel(ctx)
	if err != nil {
		return metamodel.Report{}, false, fmt.Errorf("metalabel: load the latest model: %w", err)
	}
	if !found {
		return metamodel.Report{}, false, nil
	}
	active, hasActive, err := r.repo.LatestActiveModel(ctx)
	if err != nil {
		return metamodel.Report{}, false, fmt.Errorf("metalabel: load the active model: %w", err)
	}
	s, err := r.settings(ctx)
	if err != nil {
		return metamodel.Report{}, false, fmt.Errorf("metalabel: read settings: %w", err)
	}

	report := reportOf(m)
	report.LLMCutoff = s.LLMCutoff
	if hasActive {
		report.ActiveVersion = active.Version
	}
	return report, true, nil
}

// Cutoff returns the LLM cutoff setting with its status and provenance
// (issue #776), whether or not any model was trained.
func (r *Reporter) Cutoff(ctx context.Context) (metalabelsettings.CutoffInfo, error) {
	s, err := r.settings(ctx)
	if err != nil {
		return metalabelsettings.CutoffInfo{}, fmt.Errorf("metalabel: read settings: %w", err)
	}
	return s.CutoffInfo(), nil
}

// RetrainSkips returns the newest skipped nightly retrainings (issue #799),
// newest first, whether or not any model was trained: a skip is exactly why
// there may be none.
func (r *Reporter) RetrainSkips(ctx context.Context) ([]metamodel.RetrainSkip, error) {
	rows, err := r.repo.ListRetrainSkips(ctx, RetrainSkipLimit)
	if err != nil {
		return nil, fmt.Errorf("metalabel: load retrain skips: %w", err)
	}
	out := make([]metamodel.RetrainSkip, len(rows))
	for i, s := range rows {
		out[i] = metamodel.RetrainSkip{SkippedAt: s.SkippedAt, Code: s.ReasonCode, UsableRows: s.UsableRows, RequiredRows: s.RequiredRows}
	}
	return out, nil
}
