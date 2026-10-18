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

	"github.com/ousiassllc/pitha-trador/internal/service/exitalign"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/calibration"
)

type alignmentSource struct {
	calibration.StaticCalibrationSource
	alignment exitalign.LabelExitAlignment
	err       error
	window    time.Duration
}

func (s *alignmentSource) Alignment(_ context.Context, window time.Duration) (exitalign.LabelExitAlignment, error) {
	s.window = window
	return s.alignment, s.err
}

func getAlignment(t *testing.T, source calibration.CalibrationSource, query string) (int, []byte) {
	t.Helper()
	_, api := humatest.New(t)
	huma.Register(api, huma.Operation{Method: http.MethodGet, Path: "/api/v1/calibration/exit-alignment"}, calibration.NewCalibrationHandler(source).APIAlignment)
	resp := api.Get("/api/v1/calibration/exit-alignment" + query)
	return resp.Code, resp.Body.Bytes()
}

// Issue #774: the endpoint returns the table, the warnings, the comparison
// distributions and the OOS EV beside the paper return ("データ無し" = null).
func TestCalibrationHandler_APIAlignment(t *testing.T) {
	ev := 12.0
	src := &alignmentSource{alignment: exitalign.LabelExitAlignment{
		Rows:         []exitalign.AlignmentRow{{Key: "take_profit", Item: "利確", LabelText: "40", ExitText: "120", Comparable: true}},
		Warnings:     []exitalign.AlignmentWarning{{Code: "take_profit_mismatch", Message: "利確が一致しません"}},
		ToleranceBps: 5, ToleranceMinutes: 1,
		Comparison: &exitalign.LabelExitComparison{
			HorizonMinutes: 10, ReplayedCount: 4, SkippedCount: 1,
			DiffBps:           exitalign.DistributionBps{Count: 4, Mean: -21, P95: 3},
			ReplayExits:       []exitalign.ReasonCount{{Reason: "trailing_stop", Count: 3}},
			NotEvaluatedExits: []string{"jev_direction_reversed"},
		},
		OOSPaper: &exitalign.OOSPaperComparison{ModelVersion: "v1", OOSEVBps: &ev},
	}}
	code, raw := getAlignment(t, src, "?days=7")
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", code, raw)
	}
	if src.window != 7*24*time.Hour {
		t.Errorf("window = %v, want 7 days", src.window)
	}
	var body struct {
		Rows       []map[string]any `json:"rows"`
		Warnings   []map[string]any `json:"warnings"`
		Comparison map[string]any   `json:"comparison"`
		OOSPaper   map[string]any   `json:"oos_paper"`
		TolBps     float64          `json:"tolerance_bps"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("Unmarshal: %v (body=%s)", err, raw)
	}
	if len(body.Rows) != 1 || body.Rows[0]["key"] != "take_profit" || body.Rows[0]["aligned"] != false {
		t.Errorf("rows = %v", body.Rows)
	}
	if len(body.Warnings) != 1 || body.Warnings[0]["code"] != "take_profit_mismatch" || body.TolBps != 5 {
		t.Errorf("warnings = %v, tolerance_bps = %v", body.Warnings, body.TolBps)
	}
	if body.Comparison["replayed_count"] != 4.0 || body.Comparison["skipped_count"] != 1.0 {
		t.Errorf("comparison = %v", body.Comparison)
	}
	if diff, _ := body.Comparison["diff_bps"].(map[string]any); diff["mean"] != -21.0 || diff["p95"] != 3.0 {
		t.Errorf("comparison.diff_bps = %v", body.Comparison["diff_bps"])
	}
	if exits, _ := body.Comparison["replay_exits"].([]any); len(exits) != 1 {
		t.Errorf("comparison.replay_exits = %v", body.Comparison["replay_exits"])
	}
	if body.OOSPaper["oos_ev_bps"] != 12.0 || body.OOSPaper["paper_mean_bps"] != nil || body.OOSPaper["paper_trades"] != 0.0 {
		t.Errorf("oos_paper = %v, want the EV and no paper mean", body.OOSPaper)
	}
}

func TestCalibrationHandler_APIAlignment_DefaultsAndErrors(t *testing.T) {
	src := &alignmentSource{}
	if code, raw := getAlignment(t, src, ""); code != http.StatusOK || src.window != 30*24*time.Hour {
		t.Fatalf("status = %d window = %v, want 200 and 30 days (body=%s)", code, src.window, raw)
	}
	// Without a source the table is empty, not an error.
	code, raw := getAlignment(t, calibration.StaticCalibrationSource{}, "")
	if code != http.StatusOK {
		t.Fatalf("status without a source = %d, want 200 (body=%s)", code, raw)
	}
	var empty struct {
		Rows     []any          `json:"rows"`
		Warnings []any          `json:"warnings"`
		Cmp      map[string]any `json:"comparison"`
	}
	if err := json.Unmarshal(raw, &empty); err != nil || empty.Rows == nil || empty.Warnings == nil || empty.Cmp != nil {
		t.Fatalf("empty body = %+v (err %v); want empty arrays and no comparison", empty, err)
	}
	if code, _ := getAlignment(t, src, "?days=0"); code != http.StatusUnprocessableEntity {
		t.Errorf("days=0 status = %d, want 422", code)
	}
	if code, _ := getAlignment(t, &alignmentSource{err: errors.New("boom")}, ""); code != http.StatusInternalServerError {
		t.Errorf("source error status = %d, want 500", code)
	}
}
