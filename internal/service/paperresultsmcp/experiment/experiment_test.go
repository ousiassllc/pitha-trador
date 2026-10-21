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

func comparison(status string, confounded bool, before, after query.Performance) Comparison {
	c := Comparison{
		Experiment: Experiment{Status: status}, MinTrades: 10,
		Before: Period{Performance: before}, After: Period{Performance: after}, Confounded: confounded,
	}
	c.Diff = Diff{TotalPnlJPY: after.TotalPnlJPY - before.TotalPnlJPY, MaxDrawdownJPY: after.MaxDrawdownJPY - before.MaxDrawdownJPY}
	return c
}

// The verdict looks at net pnl and maximum drawdown only, and gives none
// (insufficient_trades / confounded / aborted) unless both periods reached the
// minimum number of trades and nothing else changed.
func TestVerdict(t *testing.T) {
	tests := []struct {
		name string
		c    Comparison
		want string
	}{
		// 最低件数前は判定なし: either side below the minimum, however good the pnl.
		{"after below the minimum", comparison(StatusClosed, false, perf(10, 0, 100), perf(9, 9999, 0)), VerdictInsufficientTrades},
		{"before below the minimum", comparison(StatusClosed, false, perf(9, 0, 100), perf(10, 9999, 0)), VerdictInsufficientTrades},
		{"no trades at all", comparison(StatusRunning, false, perf(0, 0, 0), perf(0, 0, 0)), VerdictInsufficientTrades},
		{"confounded beats everything", comparison(StatusClosed, true, perf(10, 0, 100), perf(10, 9999, 0)), VerdictConfounded},
		{"aborted is never judged", comparison(StatusAborted, false, perf(10, 0, 100), perf(10, 9999, 0)), VerdictAborted},
		{"higher pnl, no more drawdown", comparison(StatusClosed, false, perf(10, 100, 50), perf(10, 200, 50)), VerdictImproved},
		{"higher pnl, less drawdown", comparison(StatusClosed, false, perf(10, 100, 50), perf(10, 200, 20)), VerdictImproved},
		{"lower pnl, no less drawdown", comparison(StatusClosed, false, perf(10, 100, 50), perf(10, 50, 50)), VerdictWorsened},
		{"lower pnl, more drawdown", comparison(StatusClosed, false, perf(10, 100, 50), perf(10, -50, 80)), VerdictWorsened},
		{"higher pnl but more drawdown", comparison(StatusClosed, false, perf(10, 100, 50), perf(10, 200, 80)), VerdictMixed},
		{"lower pnl but less drawdown", comparison(StatusClosed, false, perf(10, 100, 50), perf(10, 50, 20)), VerdictMixed},
		{"nothing moved", comparison(StatusClosed, false, perf(10, 100, 50), perf(10, 100, 50)), VerdictMixed},
	}
	for _, tt := range tests {
		if got := verdict(tt.c); got != tt.want {
			t.Errorf("%s: verdict = %s, want %s", tt.name, got, tt.want)
		}
	}
}

// The win rate and the other reference figures never decide.
func TestVerdictIgnoresTheWinRate(t *testing.T) {
	worse := perf(10, 100, 50)
	better := perf(10, 200, 50)
	rate := 0.1
	better.WinRate = &rate
	if got := verdict(comparison(StatusClosed, false, worse, better)); got != VerdictImproved {
		t.Errorf("verdict = %s, want improved despite a lower win rate", got)
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
