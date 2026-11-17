package trialresults_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/domain/jevtrial"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query/trialquery"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/trialresults"
)

type fakeSource struct {
	compare    trialquery.Comparison
	cost       trialquery.CostReport
	err        error
	ranges     []query.Range
	costCalled bool
}

func (s *fakeSource) Compare(_ context.Context, r query.Range) (trialquery.Comparison, error) {
	s.ranges = append(s.ranges, r)
	return s.compare, s.err
}

func (s *fakeSource) Cost(_ context.Context, r query.Range, _ time.Time) (trialquery.CostReport, error) {
	s.costCalled = true
	s.ranges = append(s.ranges, r)
	return s.cost, s.err
}

func serve(t *testing.T, src trialresults.Source, method, target string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	trialresults.New(src).Register(engine)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(method, target, nil))
	return rec
}

func rate(v float64) *float64 { return &v }

// seeded is a comparison and a cost report as trialquery returns them.
func seeded() *fakeSource {
	reasons := func(m map[string]int) map[string]int {
		out := map[string]int{}
		for _, r := range jevtrial.SkipReasons {
			out[r] = m[r]
		}
		return out
	}
	rows := trialquery.RowCounts{Total: 8, OK: 7, Failed: 1, MissingRate: rate(0.125)}
	return &fakeSource{
		compare: trialquery.Comparison{
			Arms:   []string{"arm-a"},
			Cohort: trialquery.Cohort{Size: 5},
			PerArm: []trialquery.ArmReport{{ArmID: "arm-a", PeriodRows: rows}},
			Fairness: trialquery.Fairness{Skipped: trialquery.Skipped{
				PerArm:          []trialquery.ArmSkips{{ArmID: "arm-a", Asked: 8, Skipped: 2, ByReason: reasons(map[string]int{jevtrial.SkipThrottled: 2})}},
				ArmsWithoutRows: []trialquery.ArmSkips{{ArmID: "arm-x", Skipped: 1, ByReason: reasons(map[string]int{jevtrial.SkipKeyNotSet: 1})}},
			}},
			CounterfactualProxy: trialquery.Proxy{Proxy: true, Arms: []trialquery.ProxyArm{{
				ArmID: "arm-a", NoneAnswers: 2, NoAnswer: 1,
				Horizons: []trialquery.ProxyHorizon{{HorizonMinutes: 5, Labelled: 2, Coverage: rate(2.0 / 3), BarrierHit: map[string]int{"tp": 1, "sl": 1}, AvgNetReturnBps: rate(-3.5)}},
			}}},
		},
		cost: trialquery.CostReport{
			Total: trialquery.CostBlock{Calls: 8, Reported: trialquery.SourceCost{Calls: 5, USD: 0.0123}, Unknown: trialquery.UnknownCost{Calls: 3, UnknownCountedUSD: 0.5}},
			PerArm: []trialquery.ArmCost{{ArmID: "arm-a", CostBlock: trialquery.CostBlock{
				Calls: 8, Reported: trialquery.SourceCost{Calls: 5, USD: 0.0123}, Unknown: trialquery.UnknownCost{Calls: 3, UnknownCountedUSD: 0.5},
			}}},
		},
	}
}

func TestPage_ShowsTheAggregationsFigures(t *testing.T) {
	src := seeded()
	rec := serve(t, src, http.MethodGet, "/performance/trial")
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, body)
	}
	for _, want := range []string{
		"12.5%（1 / 8）",          // missing_rate: (failed+refused+partial)/total, as the MCP defines it
		"全アームに聞けた判断: 5 件",       // cohort size
		`data-arm="arm-x"`,      // an arm that only has skips is not hidden
		"混雑で待機中だった",             // skip reason, named in Japanese
		"API キーが未設定だった",         // reason of the skip-only arm
		"$0.0123（5 件）",          // reported
		"不明（3 件）",               // unknown is a count; its counted_usd (0.5) is not an amount
		"NONE と反実仮想（参考値・proxy）", // proxy
		"-3.5 bps",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	if strings.Contains(body, "0.5000") || strings.Contains(body, "$0.50") {
		t.Errorf("the unknown cost's counted amount leaked as a cost")
	}
	if !src.costCalled {
		t.Errorf("cost was not read")
	}
}

// The period is the MCP tools' default: the last 30 JST days including today.
func TestPage_PeriodIsTheLastJSTDaysAndFollowsDays(t *testing.T) {
	for target, days := range map[string]int{
		"/performance/trial":            30,
		"/performance/trial?days=7":     7,
		"/performance/trial?days=90":    90,
		"/performance/trial?days=9999":  30,
		"/performance/trial?days=-1":    30,
		"/performance/trial?days=oops":  30,
		"/performance/trial?days=7&x=1": 7,
	} {
		src := &fakeSource{}
		rec := serve(t, src, http.MethodGet, target)
		if rec.Code != http.StatusOK || len(src.ranges) == 0 {
			t.Fatalf("%s: status %d, ranges %v", target, rec.Code, src.ranges)
		}
		r := src.ranges[0]
		if r.From.Location() != query.JST || r.To.Location() != query.JST || r.To.Hour() != 0 || r.From.Hour() != 0 {
			t.Errorf("%s: range %v..%v must be JST midnights", target, r.From, r.To)
		}
		if got := int(r.To.Sub(r.From).Hours() / 24); got != days {
			t.Errorf("%s: %d days, want %d", target, got, days)
		}
		if !strings.Contains(rec.Body.String(), `href="/performance/trial?days=`+strconv.Itoa(days)+`" aria-current="true"`) {
			t.Errorf("%s: the %d-day link is not marked current: %s", target, days, rec.Body.String())
		}
	}
}

func TestPage_NoRecordIsTheEmptyState(t *testing.T) {
	rec := serve(t, &fakeSource{}, http.MethodGet, "/performance/trial")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `data-testid="trial-empty"`) || strings.Contains(rec.Body.String(), "<table") {
		t.Errorf("status %d, body: %s", rec.Code, rec.Body.String())
	}
}

func TestPage_ReadFailureIs500WithAFixedMessage(t *testing.T) {
	rec := serve(t, &fakeSource{err: errors.New("query: secret SQL detail")}, http.MethodGet, "/performance/trial")
	body := rec.Body.String()
	if rec.Code != http.StatusInternalServerError || !strings.Contains(body, "お試しの成績を読み込めませんでした。") || strings.Contains(body, "secret SQL detail") {
		t.Errorf("status %d, body: %s", rec.Code, body)
	}
	rec = serve(t, &fakeSource{err: query.ErrLimitExceeded}, http.MethodGet, "/performance/trial")
	if rec.Code != http.StatusInternalServerError || !strings.Contains(rec.Body.String(), "期間を短くしてください") {
		t.Errorf("limit: status %d, body: %s", rec.Code, rec.Body.String())
	}
}

// The tab only reads: it registers GET and nothing else.
func TestRegister_IsGetOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	trialresults.New(&fakeSource{}).Register(engine)
	routes := engine.Routes()
	if len(routes) != 1 || routes[0].Method != http.MethodGet || routes[0].Path != "/performance/trial" {
		t.Errorf("routes = %v, want only GET /performance/trial", routes)
	}
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		if rec := serve(t, &fakeSource{}, method, "/performance/trial"); rec.Code != http.StatusNotFound {
			t.Errorf("%s status = %d, want 404", method, rec.Code)
		}
	}
}
