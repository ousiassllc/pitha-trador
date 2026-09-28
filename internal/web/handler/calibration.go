package handler

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/web/pages"
)

// CalibrationSource supplies Calibration's aggregated evaluation metrics
// (functional.md §4.12, FR-CAL-2/3) for `GET /api/v1/calibration`. An
// interface here keeps this handler testable without a real
// internal/service/calibration.Service (which itself requires a SQLite
// database) wired in. *internal/service/calibration.Service implements
// this directly.
type CalibrationSource interface {
	Metrics(ctx context.Context) (domain.CalibrationMetrics, error)
}

// StaticCalibrationSource is a fixed CalibrationSource, used as
// internal/router.New()'s default until a real
// internal/service/calibration.Service is wired in (mirrors
// StaticCandidateSource's role for ScannerHandler).
type StaticCalibrationSource struct {
	Metrics_ domain.CalibrationMetrics
}

func (s StaticCalibrationSource) Metrics(context.Context) (domain.CalibrationMetrics, error) {
	return s.Metrics_, nil
}

// CalibrationHandler implements `GET /api/v1/calibration`
// (docs/api/endpoints.md).
type CalibrationHandler struct {
	source CalibrationSource
}

// NewCalibrationHandler returns a CalibrationHandler backed by source.
func NewCalibrationHandler(source CalibrationSource) *CalibrationHandler {
	return &CalibrationHandler{source: source}
}

// calibrationBucketOutput mirrors docs/api/endpoints.md's `buckets[]`
// item shape.
type calibrationBucketOutput struct {
	Range              string  `json:"range" doc:"Confidence band, e.g. \"0.50-0.60\"."`
	DirectionAccuracy  float64 `json:"direction_accuracy" doc:"Share of labeled decisions in this band whose predicted direction matched the realized future return."`
	AvgFutureReturnPct float64 `json:"avg_future_return_pct" doc:"Average direction-adjusted future return (%) among labeled decisions in this band."`
}

// CalibrationAPIOutput is the Huma response body for
// `GET /api/v1/calibration` (docs/api/endpoints.md).
type CalibrationAPIOutput struct {
	Body struct {
		Buckets                  []calibrationBucketOutput `json:"buckets"`
		BrierScore               float64                   `json:"brier_score" doc:"Mean squared error between confidence and realized outcome (0=perfect, 0.25=random-guess baseline)."`
		LogLoss                  float64                   `json:"log_loss" doc:"Mean binary cross-entropy between confidence and realized outcome."`
		ExpectedCalibrationError float64                   `json:"expected_calibration_error" doc:"Weighted average gap between each bucket's confidence and observed accuracy."`
	}
}

// APICalibration implements `GET /api/v1/calibration`
// (docs/api/endpoints.md): every confidence bucket's direction accuracy
// and average future return, plus Brier Score, Log Loss, and Expected
// Calibration Error (functional.md FR-CAL-2/3).
func (h *CalibrationHandler) APICalibration(ctx context.Context, _ *struct{}) (*CalibrationAPIOutput, error) {
	metrics, err := h.source.Metrics(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError("load calibration metrics failed", err)
	}

	out := &CalibrationAPIOutput{}
	out.Body.Buckets = make([]calibrationBucketOutput, len(metrics.Buckets))
	for i, b := range metrics.Buckets {
		out.Body.Buckets[i] = calibrationBucketOutput{
			Range:              b.Range,
			DirectionAccuracy:  b.DirectionAccuracy,
			AvgFutureReturnPct: b.AvgFutureReturnPct,
		}
	}
	out.Body.BrierScore = metrics.BrierScore
	out.Body.LogLoss = metrics.LogLoss
	out.Body.ExpectedCalibrationError = metrics.ExpectedCalibrationError
	return out, nil
}

// Page implements `GET /calibration` (docs/api/endpoints.md §3): the
// Calibration page, embedding the `pitha-calibration-heatmap` island
// (functional.md §5.4, components/overview.md §5.3). Unlike
// ScannerHandler.Page there is no HX-Request fragment variant - the
// reliability curve/heatmap is drawn entirely client-side by
// pitha-calibration-heatmap itself (which fetches
// GET /api/v1/calibration on its own), so this handler needs no
// CalibrationSource dependency to render.
func (h *CalibrationHandler) Page(c *gin.Context) {
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Status(http.StatusOK)
	_ = pages.CalibrationPage().Render(c.Request.Context(), c.Writer)
}
