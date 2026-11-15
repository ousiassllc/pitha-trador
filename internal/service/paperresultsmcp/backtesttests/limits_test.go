package backtesttests

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/backtest"
	"github.com/ousiassllc/pitha-trador/internal/service/backtestlab"
	"github.com/ousiassllc/pitha-trador/internal/service/backtestlab/labtest"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp"
)

func rows(t *testing.T, srv *testServer) int {
	t.Helper()
	return count(t, srv.w, `SELECT COUNT(*) FROM backtest_runs`)
}

// One backtest at a time: a second start while one is running is
// concurrency_limited and writes nothing; once the first ends the slot is free.
func TestASecondStartWhileOneRunsIsConcurrencyLimited(t *testing.T) {
	block := labtest.NewBlocking()
	srv := newServer(t, options{source: func(context.Context, string) (backtestlab.ConfigSource, error) { return block, nil }})
	first := srv.start(t, nil)
	<-block.Entered // the run is in flight

	detail := srv.call(t, "backtest_start", withPeriod(nil)).wantError(t, http.StatusTooManyRequests, paperresultsmcp.CodeConcurrencyLimited)
	if !strings.Contains(detail, "同時に 1 件") {
		t.Errorf("detail = %q", detail)
	}
	if n := rows(t, srv); n != 1 {
		t.Fatalf("%d rows after the refused start, want 1 (nothing written)", n)
	}
	if got := srv.get(t, first.RunID); got.Status != "running" {
		t.Errorf("the first run is %q while blocked, want running", got.Status)
	}

	close(block.Release)
	srv.waitFinished(t, first.RunID)
	if got := srv.get(t, first.RunID); got.Status != "succeeded" {
		t.Fatalf("the first run ended %q", got.Status)
	}
	third := srv.start(t, nil) // the source no longer blocks (Release is closed)
	srv.waitFinished(t, third.RunID)
	if third.RunID != 2 {
		t.Errorf("run id %d: the refused start must not have used an id", third.RunID)
	}
}

// A run over the wall-clock cap is stopped and recorded as failed / timed_out;
// the next one is not affected.
func TestARunOverTheTimeCapFailsWithTimedOut(t *testing.T) {
	block := labtest.NewBlocking() // never released: only the cap ends the run
	srv := newServer(t, options{timeout: 20 * time.Millisecond, source: func(context.Context, string) (backtestlab.ConfigSource, error) { return block, nil }})
	r := srv.start(t, nil)
	srv.waitFinished(t, r.RunID)
	got := srv.get(t, r.RunID)
	if got.Status != "failed" || got.ErrorCode == nil || *got.ErrorCode != "timed_out" || got.Variants != nil {
		t.Fatalf("get = %+v", got.runView)
	}
	if got.FinishedAt == nil || got.StartedAt == nil {
		t.Errorf("a timed-out run keeps its start and finish times: %+v", got.runView)
	}
	// Not stuck: the slot is free again.
	srv2 := srv.start(t, nil)
	srv.waitFinished(t, srv2.RunID)
}

func TestLimitsOfAStartAreRefusedBeforeAnythingIsWritten(t *testing.T) {
	srv := newServer(t, options{})
	many := func(n int) []map[string]any {
		out := make([]map[string]any, n)
		for i := range out {
			out[i] = map[string]any{"params": map[string]any{}}
		}
		return out
	}
	tooLong := map[string]any{"from": "2026-09-01", "to": "2026-10-03"} // 32 days
	cases := []struct {
		name   string
		args   map[string]any
		status int
		code   paperresultsmcp.Code
		detail string
	}{
		{"13 variants", withPeriod(map[string]any{"variants": many(13)}), 422, paperresultsmcp.CodeLimitExceeded, "12 組"},
		{"7 extra costs", withPeriod(map[string]any{"extra_cost_bps": []float64{0, 1, 2, 3, 4, 5, 6}}), 422, paperresultsmcp.CodeLimitExceeded, "6 個"},
		{"a period of 32 days", tooLong, 422, paperresultsmcp.CodeLimitExceeded, "31 日"},
		{"a bad parameter", withPeriod(map[string]any{"variants": []map[string]any{{}, {"params": map[string]any{"stop_loss_pct": -3.0}}}}), 422, paperresultsmcp.CodeInvalidValue, "variants[1].params.stop_loss_pct"},
		{"a long label", withPeriod(map[string]any{"variants": []map[string]any{{"label": strings.Repeat("a", 41)}}}), 422, paperresultsmcp.CodeInvalidValue, "variants[0].label"},
		{"a bad extra cost", withPeriod(map[string]any{"extra_cost_bps": []float64{-1}}), 422, paperresultsmcp.CodeInvalidValue, "extra_cost_bps[0]"},
		{"an unknown member", withPeriod(map[string]any{"variants": []map[string]any{{"params": map[string]any{"max_leverage": 3}}}}), 422, paperresultsmcp.CodeInvalidArgument, ""},
		{"an unknown argument", withPeriod(map[string]any{"settings": map[string]any{}}), 422, paperresultsmcp.CodeInvalidArgument, ""},
		{"a wrong type", withPeriod(map[string]any{"variants": "all"}), 422, paperresultsmcp.CodeInvalidArgument, ""},
		{"only from", map[string]any{"from": "2026-10-05"}, 422, paperresultsmcp.CodeInvalidPeriod, ""},
	}
	for _, c := range cases {
		detail := srv.call(t, "backtest_start", c.args).wantError(t, c.status, c.code)
		if !strings.Contains(detail, c.detail) {
			t.Errorf("%s: detail %q, want it to mention %q", c.name, detail, c.detail)
		}
		if strings.Contains(detail, "-3") {
			t.Errorf("%s: the detail quotes the caller's value: %q", c.name, detail)
		}
	}
	if n := rows(t, srv); n != 0 {
		t.Fatalf("%d rows written by refused calls", n)
	}
	// The boundaries are accepted: 12 variants, 6 extra costs, 31 days.
	ok := srv.start(t, map[string]any{"variants": many(12), "extra_cost_bps": []float64{0, 1, 2, 3, 4, 5}, "from": "2026-09-04", "to": "2026-10-05"})
	srv.waitFinished(t, ok.RunID)
	if got := srv.get(t, ok.RunID); got.Status != "succeeded" || len(got.Variants) != 12 {
		t.Fatalf("the boundary run = %+v (%d variants)", got.runView, len(got.Variants))
	}
}

func TestTheDefaultPeriodIsTheLast30DaysAndReachesTheSourceAsInstants(t *testing.T) {
	var asked backtest.Period
	srv := newServer(t, options{source: func(context.Context, string) (backtestlab.ConfigSource, error) {
		return periodSpy{&asked}, nil
	}})
	r := srv.start(t, map[string]any{"from": nil, "to": nil})
	srv.waitFinished(t, r.RunID)
	// FixedNow is 2026-10-18 12:00 JST: [2026-09-19, 2026-10-19) JST.
	if want := time.Date(2026, 9, 18, 15, 0, 0, 0, time.UTC); !asked.Start.Equal(want) {
		t.Errorf("period start %v, want %v", asked.Start, want)
	}
	if want := time.Date(2026, 10, 18, 15, 0, 0, 0, time.UTC); !asked.End.Equal(want) {
		t.Errorf("period end %v, want %v", asked.End, want)
	}
	got := srv.get(t, r.RunID)
	if got.Status != "succeeded" || got.Period.From != "2026-09-19" || got.Period.To != "2026-10-19" || got.Period.FirstSnapshotAt != nil {
		t.Errorf("a period without data: %+v %+v", got.runView, got.Period)
	}
	if len(got.Variants) != 1 || got.Variants[0].TradeCount != 0 || got.Variants[0].ExpectancyPct != nil || got.Variants[0].IsReferenceBest {
		t.Errorf("no data should be an empty result, got %+v", got.Variants)
	}
}

type periodSpy struct{ into *backtest.Period }

func (p periodSpy) RunConfigs(_ context.Context, period backtest.Period) ([]backtest.RunConfig, error) {
	*p.into = period
	return nil, nil
}
