package calibration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/calibration"
)

type metricsSource domain.CalibrationMetrics

func (s metricsSource) Metrics(context.Context, int) (domain.CalibrationMetrics, error) {
	return domain.CalibrationMetrics(s), nil
}

// Issue #767: the response carries the triple-barrier EV (net) and the
// tp/sl/timeout reach rates, overall and per confidence band.
func TestCalibrationHandler_APICalibration_TripleBarrier(t *testing.T) {
	_, api := humatest.New(t)
	huma.Get(api, "/api/v1/calibration", calibration.NewCalibrationHandler(metricsSource{
		Buckets: []domain.ConfidenceBucket{
			{Range: "0.50-0.60", SampleCount: 4, Barrier: domain.BarrierStats{
				SampleCount: 4, TPCount: 1, SLCount: 2, TimeoutCount: 1,
				TPRate: 0.25, SLRate: 0.5, TimeoutRate: 0.25, EVNetBps: -7.5,
			}},
			{Range: "0.60-0.70"},
		},
		Barrier: domain.BarrierStats{
			SampleCount: 10, TPCount: 4, SLCount: 3, TimeoutCount: 3,
			TPRate: 0.4, SLRate: 0.3, TimeoutRate: 0.3, EVNetBps: 12.5,
		},
	}).APICalibration)

	resp := api.Get("/api/v1/calibration")
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", resp.Code, resp.Body.String())
	}
	var body struct {
		Buckets []struct {
			TripleBarrier map[string]float64 `json:"triple_barrier"`
		} `json:"buckets"`
		TripleBarrier map[string]float64 `json:"triple_barrier"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("Unmarshal: %v (body=%s)", err, resp.Body.String())
	}

	want := map[string]float64{
		"sample_count": 10, "tp_count": 4, "sl_count": 3, "timeout_count": 3,
		"tp_rate": 0.4, "sl_rate": 0.3, "timeout_rate": 0.3, "ev_net_bps": 12.5,
	}
	for key, v := range want {
		if got, ok := body.TripleBarrier[key]; !ok || got != v {
			t.Errorf("triple_barrier.%s = %v (present %v), want %v", key, got, ok, v)
		}
	}
	if len(body.Buckets) != 2 {
		t.Fatalf("buckets = %d, want 2", len(body.Buckets))
	}
	if got := body.Buckets[0].TripleBarrier; got["ev_net_bps"] != -7.5 || got["sl_rate"] != 0.5 || got["sample_count"] != 4 {
		t.Errorf("bucket[0].triple_barrier = %v, want ev -7.5, sl_rate 0.5, sample_count 4", got)
	}
	// A band without labels still has the object, all zero.
	if got, ok := body.Buckets[1].TripleBarrier["sample_count"]; !ok || got != 0 {
		t.Errorf("bucket[1].triple_barrier.sample_count = %v (present %v), want 0", got, ok)
	}
	if got := body.Buckets[1].TripleBarrier["ev_net_bps"]; got != 0 {
		t.Errorf("bucket[1].triple_barrier.ev_net_bps = %v, want 0", got)
	}
}
