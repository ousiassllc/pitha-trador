package calibration_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"

	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain/metamodel"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/calibration"
)

type metaSource struct {
	report metamodel.Report
	found  bool
	err    error
}

func (s metaSource) Report(context.Context) (metamodel.Report, bool, error) {
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
	code, raw := getMetaCalibration(t, sourceWithMeta{metaSource: metaSource{found: true, report: metamodel.Report{
		Version: "v3", Algorithm: "logistic", Status: "rejected", RejectReason: "oos auc below bar", ActiveVersion: "v2",
		TrainedAt:  time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC),
		TrainStart: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), TrainEnd: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
		OOSStart: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), OOSEnd: time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC),
		TrainSamples: 300, OOSSamples: 120, OOSAUC: &auc, OOSEVBps: &ev,
		OOSCalibration: []metamodel.CalibrationBin{{MeanPredicted: 0.55, ObservedRate: 0.5, Count: 40}},
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
	_, raw := getMetaCalibration(t, sourceWithMeta{metaSource: metaSource{found: true, report: metamodel.Report{Version: "v1", Status: "rejected"}}})
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

type cutoffSource struct {
	calibration.StaticCalibrationSource
	info metalabelsettings.CutoffInfo
	err  error
}

func (s cutoffSource) Cutoff(context.Context) (metalabelsettings.CutoffInfo, error) {
	return s.info, s.err
}

// Issue #776: the cutoff setting's status and warning are reported even before
// any model was trained, and a source error is a 500.
func TestCalibrationHandler_APICalibration_MetaModelCutoff(t *testing.T) {
	code, raw := getMetaCalibration(t, cutoffSource{info: metalabelsettings.CutoffInfo{
		Cutoff: "2026-01-01", Status: "placeholder", Warning: "仮置きのままです",
		Model: "jev-x", ConfirmedOn: "2026-10-17", Source: "vendor docs",
	}})
	if code != http.StatusOK {
		t.Fatalf("status = %d (body=%s)", code, raw)
	}
	var body struct {
		MetaModel       map[string]any `json:"meta_model"`
		MetaModelCutoff map[string]any `json:"meta_model_cutoff"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("Unmarshal: %v (body=%s)", err, raw)
	}
	if body.MetaModel != nil {
		t.Errorf("meta_model = %v, want omitted without a model", body.MetaModel)
	}
	for key, want := range map[string]any{
		"llm_cutoff": "2026-01-01", "status": "placeholder", "warning": "仮置きのままです",
		"model": "jev-x", "confirmed_on": "2026-10-17", "source": "vendor docs",
	} {
		if body.MetaModelCutoff[key] != want {
			t.Errorf("meta_model_cutoff.%s = %v, want %v", key, body.MetaModelCutoff[key], want)
		}
	}

	if code, _ := getMetaCalibration(t, cutoffSource{err: errors.New("boom")}); code != http.StatusInternalServerError {
		t.Errorf("source error: status = %d, want 500", code)
	}
	_, raw = getMetaCalibration(t, calibration.StaticCalibrationSource{})
	if strings.Contains(string(raw), "meta_model_cutoff") {
		t.Errorf("meta_model_cutoff present without a source: %s", raw)
	}
}

// Issue #780: the response carries how the model was judged: OOS counts, the
// confidence intervals, the criteria, the comparison, a blocking gate and the
// forward evaluation; a model stored before that has nulls instead.
func TestCalibrationHandler_APICalibration_MetaModelAdoptionRecord(t *testing.T) {
	ev := 4.0
	code, raw := getMetaCalibration(t, sourceWithMeta{metaSource: metaSource{found: true, report: metamodel.Report{
		Version: "v4", Status: "rejected", RejectReason: "model_swap_rate_limited",
		OOSTakes: 600, OOSDays: 6, OOSEVCI: &metamodel.Interval{Low: 0.5, High: 7}, OOSAUCCI: &metamodel.Interval{Low: 0.51, High: 0.57},
		Criteria:   &metamodel.Criteria{MinOOSSamples: 1084, MinTakes: 542, MinOOSDays: 5, MinAUCLower: 0.5, Alpha: 0.01, Resamples: 2000},
		Comparison: &metamodel.Comparison{ActiveVersion: "v2", Comparable: true, PairedSamples: 1200, DiffBps: 1.5, DiffCI: &metamodel.Interval{Low: 0.2, High: 2.8}},
		Gate:       &metamodel.Gate{Reason: "model_swap_rate_limited", Detail: "2 of 5 trading days", Until: "2026-10-21T15:00:00Z"},
		Forward:    metamodel.Forward{EvaluatedAt: "2026-10-20T09:00:00Z", From: "2026-10-14T09:00:00Z", Samples: 300, Takes: 100, Days: 3, EVBps: &ev, EVCI: &metamodel.Interval{Low: -1, High: 9}},
	}}})
	if code != http.StatusOK {
		t.Fatalf("status = %d (body=%s)", code, raw)
	}
	var body struct {
		MetaModel map[string]any `json:"meta_model"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("Unmarshal: %v (body=%s)", err, raw)
	}
	m := body.MetaModel
	if m["oos_takes"] != float64(600) || m["oos_days"] != float64(6) {
		t.Errorf("oos_takes/oos_days = %v/%v", m["oos_takes"], m["oos_days"])
	}
	for key, want := range map[string]map[string]any{
		"oos_ev_ci": {"low": 0.5, "high": float64(7)}, "oos_auc_ci": {"low": 0.51, "high": 0.57},
		"criteria":   {"min_oos_samples": float64(1084), "min_takes": float64(542), "min_oos_days": float64(5), "alpha": 0.01, "resamples": float64(2000)},
		"comparison": {"active_version": "v2", "comparable": true, "paired_samples": float64(1200), "diff_bps": 1.5},
		"gate":       {"reason": "model_swap_rate_limited", "until": "2026-10-21T15:00:00Z"},
		"forward":    {"samples": float64(300), "takes": float64(100), "ev_bps": 4.0, "degraded": false},
	} {
		got, _ := m[key].(map[string]any)
		for k, v := range want {
			if got[k] != v {
				t.Errorf("meta_model.%s.%s = %v, want %v", key, k, got[k], v)
			}
		}
	}
	if ci, _ := m["comparison"].(map[string]any)["diff_ci"].(map[string]any); ci["low"] != 0.2 {
		t.Errorf("comparison.diff_ci = %v", ci)
	}

	// A model stored before the adoption record existed: the fields are omitted, not zero values.
	_, raw = getMetaCalibration(t, sourceWithMeta{metaSource: metaSource{found: true, report: metamodel.Report{Version: "v1", Status: "active"}}})
	var legacy struct {
		MetaModel map[string]any `json:"meta_model"`
	}
	if err := json.Unmarshal(raw, &legacy); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"criteria", "comparison", "gate", "forward", "oos_ev_ci"} {
		if v, present := legacy.MetaModel[key]; present {
			t.Errorf("legacy meta_model.%s = %v, want it omitted", key, v)
		}
	}
}

// Issue #780: an OOS EV below the detectable edge (about 11 bps) whose interval
// straddles 0 is 判定不能; no takes stays データ無し.
func TestCalibrationHandler_APICalibration_MetaModelEVVerdict(t *testing.T) {
	small, large := 4.0, 15.0
	tests := map[string]struct {
		report metamodel.Report
		want   string
	}{
		"no takes is no data": {metamodel.Report{Version: "v1", Status: "rejected"}, "no_data"},
		"4 bps with an interval above 0 is positive": {
			metamodel.Report{Version: "v2", Status: "active", OOSTakes: 600, OOSEVBps: &small, OOSEVCI: &metamodel.Interval{Low: 0.5, High: 7}}, "positive"},
		"4 bps with an interval straddling 0 is undetermined": {
			metamodel.Report{Version: "v4", Status: "rejected", OOSTakes: 600, OOSEVBps: &small, OOSEVCI: &metamodel.Interval{Low: -3, High: 11}}, "undetermined"},
		"15 bps with an interval straddling 0 is not significant": {
			metamodel.Report{Version: "v5", Status: "rejected", OOSTakes: 600, OOSEVBps: &large, OOSEVCI: &metamodel.Interval{Low: -2, High: 32}}, "not_significant"},
		"15 bps with an interval above 0 is positive": {
			metamodel.Report{Version: "v3", Status: "active", OOSTakes: 600, OOSEVBps: &large, OOSEVCI: &metamodel.Interval{Low: 3, High: 27}}, "positive"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			_, raw := getMetaCalibration(t, sourceWithMeta{metaSource: metaSource{found: true, report: tc.report}})
			var body struct {
				MetaModel map[string]any `json:"meta_model"`
			}
			if err := json.Unmarshal(raw, &body); err != nil {
				t.Fatalf("Unmarshal: %v (body=%s)", err, raw)
			}
			if body.MetaModel["oos_ev_verdict"] != tc.want || body.MetaModel["min_detectable_edge_bps"] != 11.0 {
				t.Errorf("verdict/min = %v/%v, want %s/11", body.MetaModel["oos_ev_verdict"], body.MetaModel["min_detectable_edge_bps"], tc.want)
			}
		})
	}
}
