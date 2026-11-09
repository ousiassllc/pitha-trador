package loader

import (
	"context"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
	repo "github.com/ousiassllc/pitha-trador/internal/repository/metalabel"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/manifest"
)

// DefaultEmbargo is the walk-forward embargo the nightly retraining uses
// (training.DefaultWalkForwardConfig().Embargo). The model row does not store
// it, and this package must not import the training package (nothing that
// trains is reachable from the CLI), so the value is copied here and a test
// pins it to the training package's. A larger embargo only shrinks the
// independent set, so it is the safe direction.
const DefaultEmbargo = 30 * time.Minute

// Windows are the model's own windows rebuilt from its stored row and the
// stored labels, plus the notes on what could not be read exactly. The
// evaluation window is left for the caller.
type Windows struct {
	manifest.Windows
	Notes []string
}

// ModelWindows rebuilds the windows of m for the manifest:
//
//   - TrainStart, TrainEnd, OOSStart and OOSEnd are the stored columns.
//     TrainEnd is the latest label end among the fit and calibration rows
//     (training.Split.TrainEnd), so it is never before a calibration row's
//     decision time. The calibration rows themselves are not stored with the
//     model, so CalibrationEnd (defined in manifest.Windows as the newest
//     calibration row's decision time) is set to TrainEnd, an upper bound of it:
//     it can only make the independence boundary later (safe). The manifest
//     notes it.
//   - LabelEnd is the latest label end among the stored labeled decisions
//     decided in [TrainStart, OOSEnd) whose label was complete by OOSEnd (the
//     rows the split could have used), at least TrainEnd. The query stops at
//     OOSEnd: nothing from the evaluation window is read.
//   - The embargo is embargo; the LLM cutoff is read from the copy's
//     runtime_settings (never later than OOSStart).
func (s *Source) ModelWindows(ctx context.Context, m repo.Model, embargo time.Duration) (Windows, error) {
	w := Windows{Windows: manifest.Windows{
		TrainStart: m.TrainStart, TrainEnd: m.TrainEnd, CalibrationEnd: m.TrainEnd,
		OOSStart: m.OOSStart, OOSEnd: m.OOSEnd, LabelEnd: m.TrainEnd,
		EmbargoSeconds: int64(embargo / time.Second),
	}}
	rows, err := repo.ListTrainingRowsFrom(ctx, s.db, m.TrainStart, m.OOSEnd, repo.TrainingScope{})
	if err != nil {
		return Windows{}, err
	}
	for _, r := range rows {
		if end := r.LabelEnd(); !end.After(m.OOSEnd) && end.After(w.LabelEnd) {
			w.LabelEnd = end
		}
	}
	w.Notes = append(w.Notes,
		"calibration_end is train_end (the stored train_end is the latest label end of the fit and calibration rows)",
		fmt.Sprintf("embargo %s is the walk-forward default; the model row does not store it", embargo),
		"label_end is the latest label end of the stored labeled decisions in [train_start, oos_end) completed by oos_end")
	cutoff, err := s.llmCutoff(ctx)
	switch {
	case err != nil:
		w.LLMCutoff = m.OOSStart
		w.Notes = append(w.Notes, "llm_cutoff could not be read from the copy's runtime_settings; oos_start is used (the cutoff is never later than oos_start, so independence is not affected)")
	default:
		w.LLMCutoff = cutoff
		w.Notes = append(w.Notes, "llm_cutoff is the copy's runtime_settings value, which may differ from the one in force when the model was trained; independence does not depend on it (oos_start is never before the cutoff)")
	}
	return w, nil
}

func (s *Source) llmCutoff(ctx context.Context) (time.Time, error) {
	settings, err := metalabelsettings.Load(ctx, system.NewRuntimeSettingsRepository(s.db))
	if err != nil {
		return time.Time{}, err
	}
	return settings.LLMCutoffTime()
}
