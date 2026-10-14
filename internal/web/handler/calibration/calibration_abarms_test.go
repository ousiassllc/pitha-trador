package calibration_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"

	"github.com/ousiassllc/pitha-trador/internal/domain/marketregime"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/calibration"
)

// Issue #755: the response carries each A/B arm's win rate and EV after costs,
// overall and per regime.
func TestCalibrationHandler_APICalibration_ABArms(t *testing.T) {
	_, api := humatest.New(t)
	huma.Get(api, "/api/v1/calibration", calibration.NewCalibrationHandler(metricsSource{
		ABArms: []marketregime.ArmStats{
			{
				Arm:         marketregime.ArmMomentum,
				RegimeStats: marketregime.RegimeStats{TradeCount: 4, WinRate: 0.25, EVNetBps: -3.5, TotalPnL: -1200},
				ByRegime: []marketregime.RegimeStats{
					{Regime: marketregime.MeanRevert, TradeCount: 4, WinRate: 0.25, EVNetBps: -3.5, TotalPnL: -1200},
				},
			},
			{Arm: marketregime.ArmRegime},
		},
	}).APICalibration)

	resp := api.Get("/api/v1/calibration")
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", resp.Code, resp.Body.String())
	}
	var body struct {
		ABArms []struct {
			Arm        string  `json:"arm"`
			TradeCount int     `json:"trade_count"`
			WinRate    float64 `json:"win_rate"`
			EVNetBps   float64 `json:"ev_net_bps"`
			TotalPnL   float64 `json:"total_pnl"`
			ByRegime   []struct {
				Regime     string  `json:"regime"`
				TradeCount int     `json:"trade_count"`
				WinRate    float64 `json:"win_rate"`
				EVNetBps   float64 `json:"ev_net_bps"`
			} `json:"by_regime"`
		} `json:"ab_arms"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("Unmarshal: %v (body=%s)", err, resp.Body.String())
	}
	if len(body.ABArms) != 2 || body.ABArms[0].Arm != "momentum" || body.ABArms[1].Arm != "regime" {
		t.Fatalf("ab_arms = %+v, want momentum then regime", body.ABArms)
	}
	m := body.ABArms[0]
	if m.TradeCount != 4 || m.WinRate != 0.25 || m.EVNetBps != -3.5 || m.TotalPnL != -1200 {
		t.Errorf("momentum arm = %+v, want 4 trades, win 0.25, EV -3.5 bps, PnL -1200", m)
	}
	if len(m.ByRegime) != 1 || m.ByRegime[0].Regime != "mean_revert" || m.ByRegime[0].TradeCount != 4 {
		t.Errorf("momentum by_regime = %+v, want one mean_revert row", m.ByRegime)
	}
	// An arm without positions has an empty (not missing) by_regime list.
	if r := body.ABArms[1]; r.TradeCount != 0 || r.ByRegime == nil || len(r.ByRegime) != 0 {
		t.Errorf("regime arm = %+v, want zero trades and an empty by_regime", r)
	}
}

// Without an A/B source `ab_arms` is omitted.
func TestCalibrationHandler_APICalibration_OmitsABArmsWithoutSource(t *testing.T) {
	_, api := humatest.New(t)
	huma.Get(api, "/api/v1/calibration", calibration.NewCalibrationHandler(metricsSource{}).APICalibration)

	resp := api.Get("/api/v1/calibration")
	var body map[string]json.RawMessage
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if _, ok := body["ab_arms"]; ok {
		t.Errorf("ab_arms present without a source: %s", resp.Body.String())
	}
}
