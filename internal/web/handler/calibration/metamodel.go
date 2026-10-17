package calibration

import (
	"context"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain/metamodel"
)

// MetaModelSource supplies the newest meta-labeling model's OOS report
// (issue #766, parent #752) for `GET /api/v1/calibration`.
// A CalibrationSource that also implements it (internal/bootstrap/metalabeljob.CalibrationSource) enables `meta_model`.
type MetaModelSource interface {
	// Report returns the newest stored model's report, or (zero, false, nil)
	// when no model was ever trained.
	Report(ctx context.Context) (metamodel.Report, bool, error)
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
	// How the model was judged (issue #780).
	OOSTakes   int               `json:"oos_takes" doc:"Number of OOS signals the calibrated model took (the sample behind oos_ev_bps)."`
	OOSDays    int               `json:"oos_days" doc:"Number of JST trading days the OOS samples span (the bootstrap's resampling units)."`
	OOSEVCI    *intervalOutput   `json:"oos_ev_ci,omitempty" doc:"Cluster-bootstrap confidence interval of oos_ev_bps (same-day, same-symbol decisions resampled together); omitted when undefined."`
	OOSAUCCI   *intervalOutput   `json:"oos_auc_ci,omitempty" doc:"Cluster-bootstrap confidence interval of oos_auc; omitted when undefined."`
	Criteria   *criteriaOutput   `json:"criteria,omitempty" doc:"The adoption bar this model was judged against; omitted for a model stored before issue #780."`
	Comparison *comparisonOutput `json:"comparison,omitempty" doc:"The comparison with the model in force on the same OOS rows; omitted when there was none."`
	Gate       *gateOutput       `json:"gate,omitempty" doc:"The change-rate limit that blocked an otherwise adoptable model; omitted when none did."`
	Forward    *forwardOutput    `json:"forward,omitempty" doc:"Forward (holdout) evaluation of this model on decisions made after it was trained, separate from the adoption criteria; omitted until evaluated."`
}

func metaModelResponse(r metamodel.Report) *metaModelOutput {
	bins := make([]metaModelCalibrationBinOutput, len(r.OOSCalibration))
	for i, b := range r.OOSCalibration {
		bins[i] = metaModelCalibrationBinOutput{MeanPredicted: b.MeanPredicted, ObservedRate: b.ObservedRate, Count: b.Count}
	}
	return &metaModelOutput{
		OOSTakes: r.OOSTakes, OOSDays: r.OOSDays, OOSEVCI: intervalResponse(r.OOSEVCI), OOSAUCCI: intervalResponse(r.OOSAUCCI),
		Criteria: criteriaResponse(r.Criteria), Comparison: comparisonResponse(r.Comparison), Gate: gateResponse(r.Gate), Forward: forwardResponse(r.Forward),
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
