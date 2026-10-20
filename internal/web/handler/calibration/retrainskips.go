package calibration

import (
	"context"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/ousiassllc/pitha-trador/internal/domain/metamodel"
)

// RetrainSkipSource supplies the newest skipped nightly retrainings (issue
// #799, parent #752) for `GET /api/v1/calibration`'s `meta_model_skips`. A
// CalibrationSource that also implements it
// (internal/bootstrap/metalabeljob.CalibrationSource) enables it.
type RetrainSkipSource interface {
	RetrainSkips(ctx context.Context) ([]metamodel.RetrainSkip, error)
}

// retrainSkipOutput mirrors docs/api/endpoints.md's `meta_model_skips[]` item.
type retrainSkipOutput struct {
	SkippedAt    time.Time `json:"skipped_at" doc:"When the nightly retraining decided to skip (UTC)."`
	ReasonCode   string    `json:"reason_code" enum:"too_few_rows_before_oos,too_few_rows_after_purge,training_data_unusable" doc:"Fixed skip code: too_few_rows_before_oos = fewer usable labeled decisions before the OOS window than fit+calibration need; too_few_rows_after_purge = the fit or calibration set is below its minimum after purge/embargo; training_data_unusable = enough rows but one-sided labels etc. Never free text."`
	UsableRows   int       `json:"usable_rows" doc:"Usable labeled decisions counted at the point the retraining gave up (before the OOS window; after purging for too_few_rows_after_purge)."`
	RequiredRows int       `json:"required_rows" doc:"Rows fit and calibration need together (the 130 of \"必要130件中 x件\")."`
}

// retrainSkips returns the response's `meta_model_skips`: nil (omitted) without
// a source, an empty list when no retraining was ever skipped.
func (h *CalibrationHandler) retrainSkips(ctx context.Context) ([]retrainSkipOutput, error) {
	src, ok := h.source.(RetrainSkipSource)
	if !ok {
		return nil, nil
	}
	skips, err := src.RetrainSkips(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError("load retrain skips failed", err)
	}
	out := make([]retrainSkipOutput, len(skips))
	for i, s := range skips {
		out[i] = retrainSkipOutput{SkippedAt: s.SkippedAt, ReasonCode: s.Code, UsableRows: s.UsableRows, RequiredRows: s.RequiredRows}
	}
	return out, nil
}
