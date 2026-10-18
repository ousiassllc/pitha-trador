package performance_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/service/backtest"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/performance"
)

func serveSensitivity(t *testing.T, runner performance.BacktestRunner, target string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	performance.NewPerformanceHandler(runner, stubPerformanceSource{}).Register(engine)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

const sensitivityQuery = "/performance/cost-sensitivity?from=2026-09-01&to=2026-09-20"

func syntheticResult() backtest.Result {
	ev := func(v float64) *float64 { return &v }
	// Predicted EVs (bps) of 8, 3 and 1 on three trades; the 4th has none.
	return backtest.Result{ForwardTrades: []backtest.Trade{
		{NetReturnPct: 0.30, PredictedEVBps: ev(8)}, {NetReturnPct: 0.04, PredictedEVBps: ev(3)},
		{NetReturnPct: -0.20, PredictedEVBps: ev(1)}, {NetReturnPct: 0.10},
	}}
}

// Issue #779: the cost sensitivity table (extra cost 0/2/5/10 bps -> Expectancy
// and the share of still-profitable trades) is exportable as CSV and JSON.
func TestCostSensitivity_ExportsCSV(t *testing.T) {
	rec := serveSensitivity(t, &recordingBacktestRunner{result: syntheticResult()}, sensitivityQuery)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/csv") {
		t.Errorf("Content-Type = %q, want text/csv", ct)
	}
	if !strings.Contains(rec.Header().Get("Content-Disposition"), "cost-sensitivity.csv") {
		t.Errorf("Content-Disposition = %q, want a cost-sensitivity.csv attachment", rec.Header().Get("Content-Disposition"))
	}
	want := "extra_cost_bps,trade_count,expectancy_pct,expectancy_bps,positive_share,predicted_ev_trade_count,predicted_ev_positive_share\n" +
		"0,4,0.0600,6.00,0.7500,3,1.0000\n" +
		"2,4,0.0400,4.00,0.7500,3,0.6667\n" +
		"5,4,0.0100,1.00,0.5000,3,0.3333\n" +
		"10,4,-0.0400,-4.00,0.2500,3,0.0000\n"
	if rec.Body.String() != want {
		t.Fatalf("CSV =\n%s\nwant\n%s", rec.Body, want)
	}
}

func TestCostSensitivity_JSON(t *testing.T) {
	rec := serveSensitivity(t, &recordingBacktestRunner{result: syntheticResult()}, sensitivityQuery+"&format=json")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", rec.Code, rec.Body)
	}
	var body struct {
		Rows []struct {
			ExtraCostBps             float64 `json:"extra_cost_bps"`
			ExpectancyBps            float64 `json:"expectancy_bps"`
			PositiveShare            float64 `json:"positive_share"`
			PredictedEVTradeCount    int     `json:"predicted_ev_trade_count"`
			PredictedEVPositiveShare float64 `json:"predicted_ev_positive_share"`
		} `json:"rows"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || len(body.Rows) != 4 {
		t.Fatalf("body = %s (err %v), want 4 rows", rec.Body, err)
	}
	if last := body.Rows[3]; last.ExtraCostBps != 10 || last.PositiveShare != 0.25 || last.PredictedEVTradeCount != 3 || last.PredictedEVPositiveShare != 0 {
		t.Errorf("last row = %+v, want extra 10 bps, share 0.25, 3 predicted-EV trades, predicted share 0", last)
	}
	if mid := body.Rows[1]; mid.ExtraCostBps != 2 || mid.PredictedEVPositiveShare < 0.666 || mid.PredictedEVPositiveShare > 0.667 {
		t.Errorf("2 bps row = %+v, want predicted share 2/3", mid)
	}
}

func TestCostSensitivity_Errors(t *testing.T) {
	if rec := serveSensitivity(t, &recordingBacktestRunner{}, "/performance/cost-sensitivity?from=bad&to=2026-09-20"); rec.Code != http.StatusBadRequest {
		t.Errorf("bad date status = %d, want 400", rec.Code)
	}
	if rec := serveSensitivity(t, &recordingBacktestRunner{}, sensitivityQuery+"&format=xml"); rec.Code != http.StatusBadRequest {
		t.Errorf("bad format status = %d, want 400", rec.Code)
	}
	if rec := serveSensitivity(t, &recordingBacktestRunner{err: errors.New("boom")}, sensitivityQuery); rec.Code != http.StatusInternalServerError {
		t.Errorf("runner error status = %d, want 500", rec.Code)
	}
	if rec := serveSensitivity(t, &recordingBacktestRunner{err: context.DeadlineExceeded}, sensitivityQuery); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("timeout status = %d, want 503", rec.Code)
	}
}
