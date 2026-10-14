package calibration_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/calibration"
)

type metaSource struct {
	report domain.MetaModelReport
	found  bool
	err    error
}

func (s metaSource) Report(context.Context) (domain.MetaModelReport, bool, error) {
	return s.report, s.found, s.err
}

// sourceWithMeta is a CalibrationSource that also reports a meta model, the
// way internal/bootstrap/metalabeljob.CalibrationSource does.
type sourceWithMeta struct {
	calibration.StaticCalibrationSource
	metaSource
}

func getMetaCalibration(t *testing.T, source calibration.CalibrationSource) (int, []byte) {
	t.Helper()
	_, api := humatest.New(t)
	huma.Get(api, "/api/v1/calibration", calibration.NewCalibrationHandler(source).APICalibration)
	resp := api.Get("/api/v1/calibration")
	return resp.Code, resp.Body.Bytes()
}

// Issue #766: the response carries the newest model's OOS AUC, calibration
// curve and EV, plus the Jev evaluation period.
func TestCalibrationHandler_APICalibration_MetaModel(t *testing.T) {
	auc, ev := 0.61, 3.5
	code, raw := getMetaCalibration(t, sourceWithMeta{metaSource: metaSource{found: true, report: domain.MetaModelReport{
		Version: "v3", Algorithm: "logistic", Status: "rejected", RejectReason: "oos auc below bar", ActiveVersion: "v2",
		TrainedAt:  time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC),
		TrainStart: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), TrainEnd: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
		OOSStart: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), OOSEnd: time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC),
		TrainSamples: 300, OOSSamples: 120, OOSAUC: &auc, OOSEVBps: &ev,
		OOSCalibration: []domain.MetaModelCalibrationBin{{MeanPredicted: 0.55, ObservedRate: 0.5, Count: 40}},
		LLMCutoff:      "2026-09-01",
	}}})
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", code, raw)
	}
	var body struct {
		MetaModel map[string]any `json:"meta_model"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("Unmarshal: %v (body=%s)", err, raw)
	}
	m := body.MetaModel
	for key, want := range map[string]any{
		"version": "v3", "algorithm": "logistic", "status": "rejected", "reject_reason": "oos auc below bar",
		"active_version": "v2", "oos_start": "2026-10-01T00:00:00Z", "oos_end": "2026-10-14T00:00:00Z",
		"train_samples": 300.0, "oos_samples": 120.0, "oos_auc": 0.61, "oos_ev_bps": 3.5, "llm_cutoff": "2026-09-01",
	} {
		if m[key] != want {
			t.Errorf("meta_model.%s = %v, want %v", key, m[key], want)
		}
	}
	bins, _ := m["oos_calibration"].([]any)
	if len(bins) != 1 {
		t.Fatalf("oos_calibration = %v, want one bin", m["oos_calibration"])
	}
	bin, _ := bins[0].(map[string]any)
	if bin["mean_predicted"] != 0.55 || bin["observed_rate"] != 0.5 || bin["count"] != 40.0 {
		t.Errorf("bin = %v, want 0.55/0.5/40", bin)
	}
}

func TestCalibrationHandler_APICalibration_MetaModelUndefinedMetricsAreNull(t *testing.T) {
	_, raw := getMetaCalibration(t, sourceWithMeta{metaSource: metaSource{found: true, report: domain.MetaModelReport{Version: "v1", Status: "rejected"}}})
	var body struct {
		MetaModel map[string]any `json:"meta_model"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("Unmarshal: %v (body=%s)", err, raw)
	}
	for _, key := range []string{"oos_auc", "oos_ev_bps"} {
		if v, ok := body.MetaModel[key]; !ok || v != nil {
			t.Errorf("meta_model.%s = %v (present %v), want null", key, v, ok)
		}
	}
	if bins, ok := body.MetaModel["oos_calibration"].([]any); !ok || len(bins) != 0 {
		t.Errorf("oos_calibration = %v, want an empty array", body.MetaModel["oos_calibration"])
	}
}

func TestCalibrationHandler_APICalibration_MetaModelOmittedWithoutModelOrSource(t *testing.T) {
	for name, source := range map[string]calibration.CalibrationSource{
		"plain source": calibration.StaticCalibrationSource{},
		"no model yet": sourceWithMeta{},
	} {
		t.Run(name, func(t *testing.T) {
			code, raw := getMetaCalibration(t, source)
			if code != http.StatusOK {
				t.Fatalf("status = %d, want 200 (body=%s)", code, raw)
			}
			var body map[string]any
			if err := json.Unmarshal(raw, &body); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			if v, ok := body["meta_model"]; ok {
				t.Errorf("meta_model = %v, want it omitted", v)
			}
		})
	}
}

func TestCalibrationHandler_APICalibration_MetaModelSourceErrorIs500(t *testing.T) {
	code, _ := getMetaCalibration(t, sourceWithMeta{metaSource: metaSource{err: errors.New("boom")}})
	if code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", code)
	}
}
