package performance_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/service/actuals"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/performance"
)

// rangeRecorder is an evaluating PerformanceSource that records the range it
// is asked to evaluate.
type rangeRecorder struct {
	stubPerformanceSource
	ev     actuals.Evaluation
	ranges []actuals.Range
}

func (r *rangeRecorder) Evaluate(_ context.Context, rng actuals.Range) (actuals.Evaluation, error) {
	r.ranges = append(r.ranges, rng)
	return r.ev, nil
}

func serveAt(t *testing.T, src performance.PerformanceSource, target string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.GET("/performance", performance.NewPerformanceHandler(&recordingBacktestRunner{}, src).Page)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

var jstZone = time.FixedZone("JST", 9*60*60)

func TestPage_EvaluationPeriodDefaultsToAllHistory(t *testing.T) {
	src := &rangeRecorder{ev: winning()}
	rec := serveAt(t, src, "/performance")
	if rec.Code != http.StatusOK || len(src.ranges) != 1 || !src.ranges[0].From.IsZero() || !src.ranges[0].To.IsZero() {
		t.Fatalf("status %d ranges %+v, want one unbounded range", rec.Code, src.ranges)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`data-testid="evaluation-period"`, `name="eval_from"`, `name="eval_to"`,
		`<span class="text-ink" aria-current="true">全期間</span>`,
		"直近7日</a>", "直近30日</a>", "直近90日</a>",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page should contain %q", want)
		}
	}
	if strings.Contains(body, "evaluation-period-error") {
		t.Error("no error expected")
	}
}

// The chosen JST days bound the evaluated closed-at range, the end inclusive,
// and the form echoes them.
func TestPage_EvaluationPeriodBoundsTheEvaluation(t *testing.T) {
	src := &rangeRecorder{ev: winning()}
	rec := serveAt(t, src, "/performance?eval_from=2026-10-01&eval_to=2026-10-14")
	if rec.Code != http.StatusOK || len(src.ranges) != 1 {
		t.Fatalf("status %d ranges %+v", rec.Code, src.ranges)
	}
	wantFrom := time.Date(2026, 10, 1, 0, 0, 0, 0, jstZone)
	wantTo := time.Date(2026, 10, 15, 0, 0, 0, 0, jstZone)
	if got := src.ranges[0]; !got.From.Equal(wantFrom) || !got.To.Equal(wantTo) {
		t.Errorf("range = [%v, %v), want [%v, %v)", got.From, got.To, wantFrom, wantTo)
	}
	body := rec.Body.String()
	for _, want := range []string{`value="2026-10-01"`, `value="2026-10-14"`} {
		if !strings.Contains(body, want) {
			t.Errorf("form should echo %q", want)
		}
	}
	if strings.Contains(body, `aria-current="true">全期間`) {
		t.Error("全期間 must not be current for a bounded range")
	}
	if strings.Contains(body, `data-testid="backtest-form"`) {
		t.Error("eval_from must not open the バックテスト tab")
	}
}

// Either bound may be omitted.
func TestPage_EvaluationPeriodHalfOpen(t *testing.T) {
	src := &rangeRecorder{ev: winning()}
	serveAt(t, src, "/performance?eval_from=2026-10-01")
	serveAt(t, src, "/performance?eval_to=2026-10-14")
	if len(src.ranges) != 2 || src.ranges[0].From.IsZero() || !src.ranges[0].To.IsZero() || !src.ranges[1].From.IsZero() || src.ranges[1].To.IsZero() {
		t.Errorf("ranges = %+v, want [from, ∞) then (-∞, to)", src.ranges)
	}
}

// An invalid period is a 400 with a message beside the form; the section
// falls back to all history rather than showing nothing.
func TestPage_EvaluationPeriodInvalid(t *testing.T) {
	for name, tc := range map[string]struct {
		query, message string
	}{
		"bad from":       {"eval_from=2026-13-01", "開始日は YYYY-MM-DD"},
		"bad to":         {"eval_to=yesterday", "終了日は YYYY-MM-DD"},
		"to before from": {"eval_from=2026-10-14&eval_to=2026-10-01", "終了日は開始日以降"},
	} {
		t.Run(name, func(t *testing.T) {
			src := &rangeRecorder{ev: winning()}
			rec := serveAt(t, src, "/performance?"+tc.query)
			body := rec.Body.String()
			if rec.Code != http.StatusBadRequest || !strings.Contains(body, `data-testid="evaluation-period-error"`) || !strings.Contains(body, tc.message) {
				t.Errorf("status %d, want 400 with %q", rec.Code, tc.message)
			}
			if len(src.ranges) != 1 || !src.ranges[0].From.IsZero() || !src.ranges[0].To.IsZero() {
				t.Errorf("ranges = %+v, want one unbounded fallback evaluation", src.ranges)
			}
		})
	}
}

// The same day is a one-day range.
func TestPage_EvaluationPeriodSameDay(t *testing.T) {
	src := &rangeRecorder{ev: winning()}
	rec := serveAt(t, src, "/performance?eval_from=2026-10-14&eval_to=2026-10-14")
	if rec.Code != http.StatusOK || src.ranges[0].To.Sub(src.ranges[0].From) != 24*time.Hour {
		t.Errorf("status %d range %+v, want a 24h range", rec.Code, src.ranges)
	}
}

// Jev's slices render as one table per dimension with a row per label
// version and slice, 判定不能 for slices below the sample floor.
func TestPage_EvaluationShowsJevSlices(t *testing.T) {
	ev := winning()
	small := actuals.Metric{N: 3, Value: 0.67, Lo: 0.2, Hi: 1, HasCI: true, Defined: true}
	slice := func(key string, m actuals.Metric) actuals.JevSlice {
		return actuals.JevSlice{Key: key, HitRate: m, Brier: reliable(60, 0.2, 0.15, 0.25)}
	}
	ev.Jev = []actuals.JevGroup{{
		LabelVersion: 2, HitRate: reliable(60, 0.55, 0.45, 0.65), Brier: reliable(60, 0.24, 0.2, 0.28),
		ByDay:       []actuals.JevSlice{slice("2026-10-12", small)},
		ByWeek:      []actuals.JevSlice{slice("2026-W42", reliable(60, 0.55, 0.45, 0.65))},
		ByDirection: []actuals.JevSlice{slice("LONG", reliable(40, 0.6, 0.5, 0.7)), slice("SHORT", small)},
		ByHorizon:   []actuals.JevSlice{slice("10", reliable(60, 0.55, 0.45, 0.65))},
	}}
	body := serveWithEvaluation(t, fixedEvaluation{ev: ev}).Body.String()
	for _, want := range []string{
		`data-testid="evaluation-jev-day"`, `data-testid="evaluation-jev-week"`,
		`data-testid="evaluation-jev-direction"`, `data-testid="evaluation-jev-horizon"`,
		`data-key="ラベル版 2（トリプルバリア・コスト控除後） 2026-10-12"`,
		`data-key="ラベル版 2（トリプルバリア・コスト控除後） LONG"`,
		`>10分</td>`, "60.0% [50.0%–70.0%]", "参考値 67.0%（n=3、区間 [20.0%–100.0%]）",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page should contain %q", want)
		}
	}
}

func TestPage_EvaluationOmitsEmptyJevSliceTables(t *testing.T) {
	ev := winning() // Jev without slices
	body := serveWithEvaluation(t, fixedEvaluation{ev: ev}).Body.String()
	if strings.Contains(body, `data-testid="evaluation-jev-`) {
		t.Error("no slice table expected without slices")
	}
}

// The period selector is not shown on the バックテスト tab.
func TestPage_BacktestTabHasNoEvaluationPeriod(t *testing.T) {
	rec := serveAt(t, &rangeRecorder{ev: winning()}, "/performance?tab=backtest")
	if strings.Contains(rec.Body.String(), `data-testid="evaluation-period"`) {
		t.Error("the evaluation period selector belongs to the 実績 tab")
	}
}
