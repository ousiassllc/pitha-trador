package experiment

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

var now = time.Date(2026, 10, 22, 3, 0, 0, 0, time.UTC)

func oneSetting() Args {
	return Args{
		Hypothesis: "1 回の最大損失を下げるとドローダウンが減る",
		Changes:    []Change{{Kind: KindSetting, Name: "risk.paper.max_trade_loss_pct", Before: "1", After: "0.5"}},
	}
}

// 変数 1 つの検査: an experiment changes exactly one variable. Two or more are
// refused with ErrMultipleVariables (even two changes of the same key), none is
// an invalid argument.
func TestValidateAcceptsExactlyOneVariable(t *testing.T) {
	req, err := oneSetting().Validate(now)
	if err != nil {
		t.Fatalf("one variable: %v", err)
	}
	if req.StartedAt != now || req.Change.Name != "risk.paper.max_trade_loss_pct" {
		t.Errorf("request = %+v, want started now with the key kept", req)
	}

	two := oneSetting()
	two.Changes = append(two.Changes, Change{Kind: KindSetting, Name: "risk.max_daily_trades", Before: "5", After: "3"})
	if _, err := two.Validate(now); !errors.Is(err, ErrMultipleVariables) {
		t.Errorf("two variables: err = %v, want ErrMultipleVariables", err)
	}
	same := oneSetting()
	same.Changes = append(same.Changes, same.Changes[0])
	if _, err := same.Validate(now); !errors.Is(err, ErrMultipleVariables) {
		t.Errorf("the same variable twice: err = %v, want ErrMultipleVariables", err)
	}
	mixed := oneSetting()
	mixed.Changes = append(mixed.Changes, Change{Kind: KindModel, Name: "jev-model", Before: "a", After: "b"})
	if _, err := mixed.Validate(now); !errors.Is(err, ErrMultipleVariables) {
		t.Errorf("a setting and a model: err = %v, want ErrMultipleVariables", err)
	}
	none := oneSetting()
	none.Changes = nil
	if _, err := none.Validate(now); err == nil || errors.Is(err, ErrMultipleVariables) {
		t.Errorf("no variable: err = %v, want an invalid argument", err)
	}
}

func TestValidateRules(t *testing.T) {
	future := now.Add(time.Hour).Format(time.RFC3339)
	one := func(mutate func(*Args)) Args {
		a := oneSetting()
		mutate(&a)
		return a
	}
	tests := []struct {
		name string
		args Args
		want error // nil: any *InvalidError
	}{
		{"key off the whitelist", one(func(a *Args) { a.Changes[0].Name = "min_entry_quality" }), ErrKeyNotAllowed},
		{"a switch key is not whitelisted either", one(func(a *Args) { a.Changes[0].Name = "mcp.settings_write_enabled" }), ErrKeyNotAllowed},
		{"unknown kind", one(func(a *Args) { a.Changes[0].Kind = "weather" }), nil},
		{"empty hypothesis", one(func(a *Args) { a.Hypothesis = "  " }), nil},
		{"hypothesis too long", one(func(a *Args) { a.Hypothesis = strings.Repeat("あ", MaxHypothesisRunes+1) }), nil},
		{"control character", one(func(a *Args) { a.Hypothesis = "a\x00b" }), nil},
		{"unchanged value", one(func(a *Args) { a.Changes[0].After = a.Changes[0].Before }), nil},
		{"empty after", one(func(a *Args) { a.Changes[0].After = "" }), nil},
		{"audit id on a model", one(func(a *Args) { a.Changes[0].Kind = KindModel; id := int64(3); a.AuditID = &id }), nil},
		{"zero audit id", one(func(a *Args) { id := int64(0); a.AuditID = &id }), nil},
		{"bad started_at", one(func(a *Args) { a.StartedAt = "yesterday" }), nil},
		{"started_at in the future", one(func(a *Args) { a.StartedAt = future }), nil},
	}
	for _, tt := range tests {
		_, err := tt.args.Validate(now)
		if tt.want != nil {
			if !errors.Is(err, tt.want) {
				t.Errorf("%s: err = %v, want %v", tt.name, err, tt.want)
			}
			continue
		}
		if _, ok := IsInvalid(err); !ok {
			t.Errorf("%s: err = %v, want an *InvalidError", tt.name, err)
		}
	}

	// Model and prompt experiments take free names; a newline is allowed in the
	// hypothesis only; started_at may be in the past.
	a := oneSetting()
	a.Hypothesis = "1 行目\n2 行目"
	a.Changes = []Change{{Kind: KindPrompt, Name: "trader", Before: "trader-v3", After: "trader-v4"}}
	a.StartedAt = "2026-10-01T09:00:00+09:00"
	req, err := a.Validate(now)
	if err != nil || req.StartedAt != time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC) {
		t.Errorf("prompt experiment: %+v, %v", req, err)
	}
}

func perf(trades int, pnl, drawdown float64) query.Performance {
	return query.Performance{ClosedTradeCount: trades, TotalPnlJPY: pnl, MaxDrawdownJPY: drawdown}
}

// series is n trades alternating mean+spread / mean-spread (an even n has the
// exact mean and a sample standard deviation of about spread).
func series(n int, mean, spread float64) []float64 {
	xs := make([]float64, n)
	for i := range xs {
		xs[i] = mean + spread
		if i%2 == 1 {
			xs[i] = mean - spread
		}
	}
	return xs
}

// comparison builds what Compare would hold for the two per-trade series with
// the given maximum drawdowns, at the floor minimum.
func comparison(status string, confounded bool, before, after []float64, ddBefore, ddAfter float64) Comparison {
	c := Comparison{
		Experiment: Experiment{Status: status}, MinTrades: MinTradesFloor, Confounded: confounded,
		Before:      Period{Performance: perf(len(before), 0, ddBefore)},
		After:       Period{Performance: perf(len(after), 0, ddAfter)},
		PerTradePnl: Welch(before, after),
	}
	c.Diff = Diff{MaxDrawdownJPY: ddAfter - ddBefore}
	return c
}

// The verdict is the 95% interval of the per-trade mean pnl difference plus
// the drawdown, and gives none (aborted / confounded / insufficient_trades /
// not_computable) unless the order of the rules lets it get that far. Only an
// interval wholly above 0 with the drawdown not worse is a win.
func TestVerdict(t *testing.T) {
	const n = MinTradesFloor
	base := series(n, 100, 500)
	tests := []struct {
		name string
		c    Comparison
		want string
	}{
		// 判定の順: 中止 → 交絡 → 件数不足 → 勝ち負け.
		{"aborted beats everything", comparison(StatusAborted, true, series(10, 0, 1), series(10, 9999, 1), 100, 0), VerdictAborted},
		{"confounded beats thin periods", comparison(StatusClosed, true, series(10, 0, 1), series(10, 9999, 1), 100, 0), VerdictConfounded},
		{"confounded beats a clear win", comparison(StatusClosed, true, base, series(n, 700, 500), 100, 0), VerdictConfounded},
		{"after below the minimum (63)", comparison(StatusClosed, false, base, series(n-1, 9999, 1), 100, 0), VerdictInsufficientTrades},
		{"before below the minimum (63)", comparison(StatusClosed, false, series(n-1, 0, 1), series(n, 9999, 1), 100, 0), VerdictInsufficientTrades},
		{"no trades at all", comparison(StatusRunning, false, nil, nil, 0, 0), VerdictInsufficientTrades},
		{"enough trades but every pnl identical", comparison(StatusClosed, false, series(n, 100, 0), series(n, 200, 0), 50, 50), VerdictNotComputable},
		// 勝ち負け.
		{"interval above 0, drawdown equal: win", comparison(StatusClosed, false, base, series(n, 700, 500), 50, 50), VerdictImproved},
		{"interval above 0, drawdown smaller: win", comparison(StatusClosed, false, base, series(n, 700, 500), 50, 20), VerdictImproved},
		{"interval above 0 but drawdown worse: not a win", comparison(StatusClosed, false, base, series(n, 700, 500), 50, 80), VerdictMixed},
		{"interval crosses 0 (mean up a little): not a win", comparison(StatusClosed, false, base, series(n, 160, 500), 50, 20), VerdictInconclusive},
		{"interval crosses 0 (mean down a little)", comparison(StatusClosed, false, base, series(n, 40, 500), 50, 50), VerdictInconclusive},
		{"no change at all", comparison(StatusClosed, false, base, base, 50, 50), VerdictInconclusive},
		{"interval below 0, drawdown smaller", comparison(StatusClosed, false, series(n, 700, 500), base, 50, 20), VerdictWorsened},
		{"interval below 0, drawdown worse", comparison(StatusClosed, false, series(n, 700, 500), base, 50, 80), VerdictWorsened},
	}
	for _, tt := range tests {
		if got := verdict(tt.c); got != tt.want {
			t.Errorf("%s: verdict = %s, want %s (interval %v..%v)", tt.name, got, tt.want, ptr(tt.c.PerTradePnl.CILow), ptr(tt.c.PerTradePnl.CIHigh))
		}
	}
}

func ptr(p *float64) any {
	if p == nil {
		return nil
	}
	return *p
}

// The win rate and the other reference figures never decide.
func TestVerdictIgnoresTheWinRate(t *testing.T) {
	n := MinTradesFloor
	c := comparison(StatusClosed, false, series(n, 100, 500), series(n, 700, 500), 50, 50)
	rate := 0.1
	c.After.Performance.WinRate = &rate
	if got := verdict(c); got != VerdictImproved {
		t.Errorf("verdict = %s, want improved despite a lower win rate", got)
	}
}

// The total pnl and the drawdown are no longer the rule: a higher total with
// the same per-trade mean (more trades) is not a win by itself.
func TestVerdictDoesNotFollowTheTotal(t *testing.T) {
	n := MinTradesFloor
	c := comparison(StatusClosed, false, series(n, 100, 500), series(n, 100, 500), 50, 50)
	c.After.Performance.TotalPnlJPY, c.Diff.TotalPnlJPY = 1e6, 1e6
	if got := verdict(c); got != VerdictInconclusive {
		t.Errorf("verdict = %s, want inconclusive: the mean per trade did not move", got)
	}
}

func TestWindowFollowsTheExperiment(t *testing.T) {
	started := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	ended := started.Add(48 * time.Hour)
	running := Experiment{startedAt: started}
	if s, e := running.window(now); s != started || e != now {
		t.Errorf("running window = [%v, %v), want [start, now)", s, e)
	}
	closed := Experiment{startedAt: started, endedAt: &ended}
	if s, e := closed.window(now); s != started || e != ended {
		t.Errorf("closed window = [%v, %v), want [start, end)", s, e)
	}
}
