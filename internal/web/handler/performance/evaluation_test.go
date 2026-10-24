package performance_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/service/actuals"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/performance"
)

// fixedEvaluation is a PerformanceSource that also evaluates (as the real
// provider does): it returns ev.
type fixedEvaluation struct {
	stubPerformanceSource
	ev actuals.Evaluation
}

func (f fixedEvaluation) Evaluate(context.Context, actuals.Range) (actuals.Evaluation, error) {
	return f.ev, nil
}

func reliable(n int, v, lo, hi float64) actuals.Metric {
	return actuals.Metric{N: n, Value: v, Lo: lo, Hi: hi, HasCI: true, Defined: true, Reliable: true}
}

// winning is an evaluation of 120 trades over 24 days that passes every check.
func winning() actuals.Evaluation {
	stats := actuals.Stats{
		N: 120, Days: 24, Wins: 90, NetPnLJPY: 24000, FeesJPY: 1200, SlippageJPY: 900, SlippageRecordedFill: 240,
		GrossBeforeFeesJPY: 25200, GrossBeforeCostsJPY: 26100,
		WinRate: reliable(120, 0.75, 0.6, 0.9), EVJPY: reliable(120, 200, 80, 320), EVBps: reliable(120, 20, 8, 32),
		ProfitFactor: reliable(120, 4, 2, 9), MaxDrawdownJPY: reliable(120, 600, 300, 900), MaxDrawdownPct: reliable(120, 0.2, 0.1, 0.3),
	}
	return actuals.Evaluation{
		InitialCapital: 30_000_000, Criteria: actuals.DefaultCriteria(), Overall: stats,
		ByDay:       []actuals.Group{{Key: "2026-09-01", Stats: stats}},
		ByDirection: []actuals.Group{{Key: "LONG", Stats: stats}},
		Verdict: actuals.Verdict{Status: actuals.StatusPass, Checks: []actuals.Check{
			{Key: actuals.CheckPeriodDays, Required: 20, Actual: 24, HasActual: true, Status: actuals.StatusPass},
			{Key: actuals.CheckTrades, Required: 100, Actual: 120, HasActual: true, Status: actuals.StatusPass},
			{Key: actuals.CheckEVLowerBound, Required: 0, Actual: 8, HasActual: true, Status: actuals.StatusPass},
			{Key: actuals.CheckMaxDrawdown, Required: 5, Actual: 0.2, HasActual: true, Status: actuals.StatusPass},
		}},
		Jev: []actuals.JevGroup{{LabelVersion: 2, HitRate: reliable(60, 0.55, 0.45, 0.65), Brier: reliable(60, 0.24, 0.2, 0.28)}},
		Meta: []actuals.MetaComparison{{
			Version: "v-a", Status: "active", LabelVersion: 2, OOSStart: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
			OOSEnd: time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC), OOSSamples: 120, OOSReliable: true,
			OOSAUC: new(0.62), OOSExpectedBps: new(7.5), Linked: stats, PredictedEVBps: reliable(40, 25, 20, 30),
			EVGapBps: reliable(40, -5, -9, -1), InOOSWindow: stats,
		}},
	}
}

type failingEvaluation struct{ stubPerformanceSource }

func (failingEvaluation) Evaluate(context.Context, actuals.Range) (actuals.Evaluation, error) {
	return actuals.Evaluation{}, errors.New("db is locked")
}

func serveWithEvaluation(t *testing.T, src performance.PerformanceSource) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	h := performance.NewPerformanceHandler(&recordingBacktestRunner{}, src)
	engine.GET("/performance", h.Page)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/performance", nil))
	return rec
}

// The page shows 合格 with each check, the capital the drawdown is relative to
// (as a placeholder), the cost breakdown and the meta model beside its OOS.
func TestPage_EvaluationShowsPassWithChecksAndCosts(t *testing.T) {
	rec := serveWithEvaluation(t, fixedEvaluation{ev: winning()})
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	for _, want := range []string{
		`data-testid="evaluation-verdict" data-status="pass"`,
		"合格ライン: 合格",
		`data-testid="evaluation-check-ev_lower_bound" data-status="pass"`,
		"初期資金 ¥30,000,000",
		"仮置き",
		"最大DD(資金比)",
		"0.20% [0.10%–0.30%]",
		"75.0% [60.0%–90.0%]",
		`data-key="2026-09-01"`,
		`data-testid="evaluation-table-direction"`,
		"スリッページ",
		"約定価格に含まれたコスト",
		"¥900（記録のある約定 240）",
		"グロス（コスト前）",
		`data-key="v-a"`,
		"2026-09-01〜2026-09-08",
		"ラベル版 2",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page should contain %q", want)
		}
	}
}

// Metrics that are not reliable read 判定不能 (with the reference value in a
// help mark), and so does the verdict.
func TestPage_EvaluationShowsUndeterminedMetrics(t *testing.T) {
	ev := winning()
	ev.Overall.WinRate = actuals.Metric{N: 6, Value: 0.5, Lo: 0.1, Hi: 0.9, HasCI: true, Defined: true}
	ev.Overall.ProfitFactor = actuals.Metric{N: 6}
	ev.Verdict.Status = actuals.StatusUndetermined
	body := serveWithEvaluation(t, fixedEvaluation{ev: ev}).Body.String()
	if !strings.Contains(body, `data-testid="evaluation-verdict" data-status="undetermined"`) || !strings.Contains(body, "合格ライン: 判定不能") {
		t.Error("verdict should read 判定不能")
	}
	if !strings.Contains(body, "参考値 50.0%（n=6、区間 [10.0%–90.0%]）") {
		t.Error("an unreliable but defined metric should carry its reference value in its help text")
	}
	if n := strings.Count(body, "data-undetermined"); n != 2 {
		t.Errorf("判定不能 cells = %d, want 2 (win rate and profit factor of the overall row)", n)
	}
}

// A source that cannot evaluate (a database without trades) is a 判定不能 verdict with
// every overall metric undetermined.
func TestPage_EvaluationOfEmptyDatabase(t *testing.T) {
	body := serveWithEvaluation(t, stubPerformanceSource{}).Body.String()
	if !strings.Contains(body, `data-status="undetermined"`) || !strings.Contains(body, "初期資金 未設定") {
		t.Error("empty evaluation should be 判定不能 with an unset capital")
	}
	if n := strings.Count(body, "data-undetermined"); n < 6 {
		t.Errorf("判定不能 cells = %d, want the overall metrics undetermined", n)
	}
}

func TestPage_EvaluationErrorIsFixedMessage(t *testing.T) {
	rec := serveWithEvaluation(t, failingEvaluation{})
	body := rec.Body.String()
	if rec.Code != http.StatusInternalServerError || !strings.Contains(body, "実績評価の取得に失敗しました。") {
		t.Errorf("status %d, body lacks the fixed message", rec.Code)
	}
	if strings.Contains(body, "db is locked") {
		t.Error("the cause must not be shown")
	}
}
