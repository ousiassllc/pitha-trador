// Package fromsplit derives the model's windows for package independence from
// the training package's own walk-forward split, so the independence boundary
// uses the real definitions of train end, calibration end, OOS end, LabelEnd
// and embargo instead of a copy.
package fromsplit

import (
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/manifest"
	"github.com/ousiassllc/pitha-trador/internal/service/metalabel/training"
)

// Windows returns the model windows of s (the walk-forward fold the model was
// trained on) with cfg's embargo. EvalFrom and EvalTo are left for the caller:
// the evaluation window is pre-registered separately.
//
// CalibrationEnd is the decision time of the newest calibration row and
// LabelEnd the latest LabelEnd among the fit, calibration and original OOS
// rows (every row the model used or was tested on).
func Windows(s training.Split, cfg training.WalkForwardConfig) manifest.Windows {
	w := manifest.Windows{
		TrainStart: s.TrainStart, TrainEnd: s.TrainEnd,
		OOSStart: s.OOSStart, OOSEnd: s.OOSEnd, LLMCutoff: s.LLMCutoff,
		EmbargoSeconds: int64(cfg.Embargo / time.Second),
	}
	for _, r := range s.Calibrate {
		if r.DecidedAt.After(w.CalibrationEnd) {
			w.CalibrationEnd = r.DecidedAt
		}
	}
	for _, rows := range [][]training.Row{s.Fit, s.Calibrate, s.OOS} {
		for _, r := range rows {
			if r.LabelEnd.After(w.LabelEnd) {
				w.LabelEnd = r.LabelEnd
			}
		}
	}
	return w
}
