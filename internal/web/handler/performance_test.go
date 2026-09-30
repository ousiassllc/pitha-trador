package handler_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/service/backtest"
	"github.com/ousiassllc/pitha-trador/internal/web/handler"
)

// recordingBacktestRunner records the WalkForwardConfig it was asked to
// run and returns result/err.
type recordingBacktestRunner struct {
	calls  []backtest.WalkForwardConfig
	result backtest.Result
	err    error
}

func (r *recordingBacktestRunner) RunWalkForward(_ context.Context, wf backtest.WalkForwardConfig) (backtest.Result, error) {
	r.calls = append(r.calls, wf)
	return r.result, r.err
}

func servePerformance(t *testing.T, runner handler.BacktestRunner, target string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.GET("/performance", handler.NewPerformanceHandler(runner).Page)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

func TestPerformanceHandler_Page_WithoutQueryRendersFormWithoutRunning(t *testing.T) {
	runner := &recordingBacktestRunner{}
	rec := servePerformance(t, runner, "/performance")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if len(runner.calls) != 0 {
		t.Errorf("RunWalkForward calls = %d, want 0 until the form is submitted", len(runner.calls))
	}
	if body := rec.Body.String(); !strings.Contains(body, `data-testid="backtest-form"`) || strings.Contains(body, `data-testid="performance-summary"`) {
		t.Errorf("body should show the form and no results; body=%s", body)
	}
}

func TestPerformanceHandler_Page_RunsWalkForwardOverJSTDayRangeAndRendersMetrics(t *testing.T) {
	runner := &recordingBacktestRunner{result: backtest.Result{
		Combined: backtest.Metrics{TradeCount: 12, WinRate: 0.5, Expectancy: 0.34, MaxDrawdownPct: 4.1},
	}}
	rec := servePerformance(t, runner, "/performance?from=2026-09-01&to=2026-09-10&training_days=3&validation_days=2&forward_days=1")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}
	if len(runner.calls) != 1 {
		t.Fatalf("RunWalkForward calls = %d, want 1", len(runner.calls))
	}
	wf := runner.calls[0]
	jst := time.FixedZone("JST", 9*60*60)
	if !wf.Start.Equal(time.Date(2026, 9, 1, 0, 0, 0, 0, jst)) || !wf.End.Equal(time.Date(2026, 9, 11, 0, 0, 0, 0, jst)) {
		t.Errorf("range = [%s, %s), want [2026-09-01 00:00 JST, 2026-09-11 00:00 JST) (to is inclusive)", wf.Start, wf.End)
	}
	if wf.TrainingPeriod != 72*time.Hour || wf.ValidationPeriod != 48*time.Hour || wf.ForwardPeriod != 24*time.Hour {
		t.Errorf("fold lengths = %s/%s/%s, want 72h/48h/24h", wf.TrainingPeriod, wf.ValidationPeriod, wf.ForwardPeriod)
	}
	body := rec.Body.String()
	for _, want := range []string{`data-metric="trade_count">12<`, `data-metric="expectancy">0.34%<`, `data-metric="max_drawdown_pct">4.10%<`} {
		if !strings.Contains(body, want) {
			t.Errorf("body does not contain %q", want)
		}
	}
}

func TestPerformanceHandler_Page_RejectsInvalidInputWithoutRunning(t *testing.T) {
	for name, query := range map[string]string{
		"bad date":         "from=2026-13-01&to=2026-09-10",
		"to before from":   "from=2026-09-10&to=2026-09-01",
		"zero fold length": "from=2026-09-01&to=2026-09-10&forward_days=0",
		"too short range":  "from=2026-09-01&to=2026-09-03",
	} {
		t.Run(name, func(t *testing.T) {
			runner := &recordingBacktestRunner{}
			rec := servePerformance(t, runner, "/performance?"+query)

			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400", rec.Code)
			}
			if len(runner.calls) != 0 {
				t.Errorf("RunWalkForward calls = %d, want 0 for invalid input", len(runner.calls))
			}
			if !strings.Contains(rec.Body.String(), `data-testid="backtest-error"`) {
				t.Errorf("body should show the validation error")
			}
		})
	}
}

func TestPerformanceHandler_Page_ShowsFixedMessageOnRunFailure(t *testing.T) {
	runner := &recordingBacktestRunner{err: errors.New("look-ahead check failed")}
	rec := servePerformance(t, runner, "/performance?from=2026-09-01&to=2026-09-10")

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `data-testid="backtest-error"`) {
		t.Errorf("body should show a run failure message")
	}
	if strings.Contains(body, "look-ahead check failed") {
		t.Errorf("body leaks the internal run error: %s", body)
	}
}

func dayQuery(days int, extra string) string {
	from := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 0, days-1)
	return "from=" + from.Format("2006-01-02") + "&to=" + to.Format("2006-01-02") + extra
}

func TestPerformanceHandler_Page_EnforcesInputBounds(t *testing.T) {
	for name, tc := range map[string]struct {
		query    string
		wantCode int
	}{
		"fold days at limit":     {"from=2026-01-01&to=2027-12-31&training_days=366&validation_days=1&forward_days=1", http.StatusOK},
		"fold days over limit":   {"from=2026-01-01&to=2026-12-31&training_days=367", http.StatusBadRequest},
		"duration overflow days": {"from=2026-01-01&to=2026-12-31&training_days=106752", http.StatusBadRequest},
		"int64 max days":         {"from=2026-01-01&to=2026-12-31&forward_days=9223372036854775807", http.StatusBadRequest},
		"range at limit":         {dayQuery(1830, "&forward_days=366"), http.StatusOK},
		"range over limit":       {dayQuery(1831, "&forward_days=366"), http.StatusBadRequest},
		"full calendar range":    {"from=0001-01-01&to=9999-12-31&training_days=1&validation_days=1&forward_days=1", http.StatusBadRequest},
		"splits at limit":        {dayQuery(1002, "&training_days=1&validation_days=1&forward_days=1"), http.StatusOK},
		"splits over limit":      {dayQuery(1003, "&training_days=1&validation_days=1&forward_days=1"), http.StatusBadRequest},
	} {
		t.Run(name, func(t *testing.T) {
			runner := &recordingBacktestRunner{}
			rec := servePerformance(t, runner, "/performance?"+tc.query)

			if rec.Code != tc.wantCode {
				t.Fatalf("status = %d, want %d (body=%s)", rec.Code, tc.wantCode, rec.Body.String())
			}
			if ran := len(runner.calls) == 1; ran != (tc.wantCode == http.StatusOK) {
				t.Errorf("RunWalkForward calls = %d for status %d", len(runner.calls), rec.Code)
			}
		})
	}
}

func TestPerformanceHandler_Page_TimeoutReturns503(t *testing.T) {
	runner := &recordingBacktestRunner{err: context.DeadlineExceeded}
	rec := servePerformance(t, runner, "/performance?from=2026-09-01&to=2026-09-10")

	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), `data-testid="backtest-error"`) {
		t.Errorf("status = %d, want 503 with the error shown", rec.Code)
	}
}
