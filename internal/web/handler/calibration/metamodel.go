package calibration

import (
	"context"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// MetaModelSource supplies the newest meta-labeling model's OOS report
// (issue #766, parent #752) for `GET /api/v1/calibration`.
// A CalibrationSource that also implements it (internal/bootstrap/metalabeljob.CalibrationSource) enables `meta_model`.
type MetaModelSource interface {
	// Report returns the newest stored model's report, or (zero, false, nil)
	// when no model was ever trained.
	Report(ctx context.Context) (domain.MetaModelReport, bool, error)
}

// metaModelCalibrationBinOutput is one point of the model's OOS reliability
// curve.
type metaModelCalibrationBinOutput struct {
	MeanPredicted float64 `json:"mean_predicted" doc:"Mean calibrated probability of the OOS samples in this probability bin (the curve's x value)."`
	ObservedRate  float64 `json:"observed_rate" doc:"Share of those samples that actually won (the curve's y value)."`
	Count         int     `json:"count" doc:"Number of OOS samples in the bin."`
}

// metaModelOutput mirrors docs/api/endpoints.md's `meta_model` object.
type metaModelOutput struct {
	Version        string                          `json:"version" doc:"Version of the newest trained model."`
	Algorithm      string                          `json:"algorithm" doc:"Learning algorithm of that model."`
	Status         string                          `json:"status" enum:"active,rejected" doc:"active: the OOS metrics passed the bar; rejected: kept for the record only (see reject_reason)."`
	RejectReason   string                          `json:"reject_reason" doc:"Why a rejected model was not adopted; empty for an active one."`
	ActiveVersion  string                          `json:"active_version" doc:"Version of the model currently in force (entries are skipped while empty). Differs from version when the newest retraining was rejected."`
	TrainedAt      time.Time                       `json:"trained_at" doc:"When the model was trained."`
	TrainStart     time.Time                       `json:"train_start" doc:"Start of the training window."`
	TrainEnd       time.Time                       `json:"train_end" doc:"End of the training window."`
	OOSStart       time.Time                       `json:"oos_start" doc:"Start of the out-of-sample window: the Jev evaluation period (never before llm_cutoff)."`
	OOSEnd         time.Time                       `json:"oos_end" doc:"End of the out-of-sample window."`
	TrainSamples   int                             `json:"train_samples" doc:"Number of training samples."`
	OOSSamples     int                             `json:"oos_samples" doc:"Number of out-of-sample samples behind the OOS metrics."`
	OOSAUC         *float64                        `json:"oos_auc" nullable:"true" doc:"Out-of-sample AUC (0.5 = no skill); null when undefined."`
	OOSEVBps       *float64                        `json:"oos_ev_bps" nullable:"true" doc:"Mean cost-adjusted EV (bps) of the OOS samples the calibrated model would take; null when undefined."`
	OOSCalibration []metaModelCalibrationBinOutput `json:"oos_calibration" doc:"OOS reliability curve of the calibrated probability."`
	LLMCutoff      string                          `json:"llm_cutoff" doc:"Settings LLM training-data cutoff (YYYY-MM-DD, UTC); the OOS evaluation uses only decisions on or after it."`
}

func metaModelResponse(r domain.MetaModelReport) *metaModelOutput {
	bins := make([]metaModelCalibrationBinOutput, len(r.OOSCalibration))
	for i, b := range r.OOSCalibration {
		bins[i] = metaModelCalibrationBinOutput{MeanPredicted: b.MeanPredicted, ObservedRate: b.ObservedRate, Count: b.Count}
	}
	return &metaModelOutput{
		Version: r.Version, Algorithm: r.Algorithm, Status: r.Status, RejectReason: r.RejectReason,
		ActiveVersion: r.ActiveVersion, TrainedAt: r.TrainedAt, TrainStart: r.TrainStart, TrainEnd: r.TrainEnd,
		OOSStart: r.OOSStart, OOSEnd: r.OOSEnd, TrainSamples: r.TrainSamples, OOSSamples: r.OOSSamples,
		OOSAUC: r.OOSAUC, OOSEVBps: r.OOSEVBps, OOSCalibration: bins, LLMCutoff: r.LLMCutoff,
	}
}

// metaModel returns the response's `meta_model`: nil (omitted from the response) without a source or a
// trained model.
func (h *CalibrationHandler) metaModel(ctx context.Context) (*metaModelOutput, error) {
	meta, ok := h.source.(MetaModelSource)
	if !ok {
		return nil, nil
	}
	report, found, err := meta.Report(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError("load meta model report failed", err)
	}
	if !found {
		return nil, nil
	}
	return metaModelResponse(report), nil
}

// LLMCutoffSource supplies the Jev training-data cutoff setting (issue #776) for
// `GET /api/v1/calibration`'s `meta_model_cutoff`. A CalibrationSource that also
// implements it (internal/bootstrap/metalabeljob.CalibrationSource) enables it.
type LLMCutoffSource interface {
	Cutoff(ctx context.Context) (metalabelsettings.CutoffInfo, error)
}

// llmCutoffOutput mirrors docs/api/endpoints.md's `meta_model_cutoff` object.
type llmCutoffOutput struct {
	LLMCutoff   string `json:"llm_cutoff" doc:"Settings metalabel.llm_cutoff (YYYY-MM-DD, UTC): the OOS evaluation uses only decisions on or after it."`
	Status      string `json:"status" enum:"placeholder,unverified,confirmed" doc:"placeholder: unset or the shipped default, so no model is adopted; unverified: a real date whose Jev model is not recorded; confirmed: a real date with the Jev model recorded."`
	Warning     string `json:"warning" doc:"Operator-facing warning; empty when confirmed."`
	Model       string `json:"model" doc:"Jev model name the cutoff was confirmed for (metalabel.llm_cutoff_model); empty when not recorded."`
	ConfirmedOn string `json:"confirmed_on" doc:"Date the operator confirmed the cutoff (YYYY-MM-DD); empty when not recorded."`
	Source      string `json:"source" doc:"Evidence of the cutoff (metalabel.llm_cutoff_source); empty when not recorded."`
}

// llmCutoff returns the response's `meta_model_cutoff`: nil (omitted) without a source.
func (h *CalibrationHandler) llmCutoff(ctx context.Context) (*llmCutoffOutput, error) {
	src, ok := h.source.(LLMCutoffSource)
	if !ok {
		return nil, nil
	}
	info, err := src.Cutoff(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError("load llm cutoff failed", err)
	}
	return &llmCutoffOutput{
		LLMCutoff: info.Cutoff, Status: info.Status, Warning: info.Warning,
		Model: info.Model, ConfirmedOn: info.ConfirmedOn, Source: info.Source,
	}, nil
}
