package rankingwatch_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/marketdata"
)

func TestWatcher_FailureOrPanicMeansZeroCandidatesAndNextCycleRecovers(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		panics bool
	}{
		{"http error", &marketdata.APIError{StatusCode: 400, Code: 100001, Message: "bad"}, false},
		{"no token", marketdata.ErrNoToken, false},
		{"rate limited", marketdata.ErrRateLimited, false},
		{"decode error", errors.New("decode ranking: invalid character"), false},
		{"panic", nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t, "7203", "6758")
			r.held.symbols = []string{"6758"}
			r.src.set([]string{"7203"}, nil, false)
			r.cycle()
			if !r.list.Contains("7203") {
				t.Fatal("setup: 7203 should be watched")
			}

			r.src.set(nil, tc.err, tc.panics)
			r.cycle() // must not panic or stop

			if r.list.Contains("7203") {
				t.Error("candidate of the previous ranking is still published after a failed fetch")
			}
			if !r.list.Contains("6758") {
				t.Error("held symbol lost after a failed fetch")
			}
			if !strings.Contains(r.logs.String(), "ranking failed") {
				t.Errorf("failure not logged: %s", r.logs.String())
			}

			r.src.set([]string{"7203"}, nil, false) // next tick: kabu answers again
			r.cycle()
			if !r.list.Contains("7203") || !r.list.Contains("6758") {
				t.Error("watch list did not recover on the next cycle")
			}
			if !strings.Contains(r.logs.String(), "ranking recovered") {
				t.Errorf("recovery not logged: %s", r.logs.String())
			}
		})
	}
}

func TestWatcher_FailedTypeDiscardsTheWholeRanking(t *testing.T) {
	r := newRig(t, "7203")
	calls := 0
	r.w.Source = sourceFunc(func(rankType int) ([]string, error) {
		calls++
		if rankType == 2 {
			return nil, errors.New("boom")
		}
		return []string{"7203"}, nil
	})
	r.cycle()
	if r.list.Len() != 0 {
		t.Errorf("watch list = %d symbols, want 0: a half-fetched ranking is never used", r.list.Len())
	}
	if calls != 2 {
		t.Errorf("calls = %d, want 2", calls)
	}
}

type sourceFunc func(rankType int) ([]string, error)

func (f sourceFunc) RankingSymbols(_ context.Context, rankType int, _ string) ([]string, error) {
	return f(rankType)
}

func TestWatcher_RepeatedFailuresAreLoggedOncePerTenMinutes(t *testing.T) {
	r := newRig(t, "7203")
	r.src.set(nil, marketdata.ErrNoToken, false)
	for range 10 { // minutes 0..9
		r.cycle()
	}
	if n := strings.Count(r.logs.String(), "ranking failed"); n != 1 {
		t.Errorf("failure lines in 10 cycles = %d, want 1", n)
	}
	r.cycle() // minute 10
	if n := strings.Count(r.logs.String(), "ranking failed"); n != 2 {
		t.Errorf("failure lines after 10 minutes = %d, want 2", n)
	}
}

func TestWatcher_LogsCountsOnlyNeverSymbolsOrPrices(t *testing.T) {
	r := newRig(t, "7203")
	r.src.set([]string{"7203"}, nil, false)
	r.cycle()
	r.src.set(nil, &marketdata.APIError{StatusCode: 400, Code: 100001, Message: "bad"}, false)
	r.cycle()
	if strings.Contains(r.logs.String(), "7203") {
		t.Errorf("a ranked symbol code reached the log: %s", r.logs.String())
	}
	for _, want := range []string{"ranked_rows=", "watch=", "duration_ms=", "error_code=100001"} {
		if !strings.Contains(r.logs.String(), want) {
			t.Errorf("log lacks %q: %s", want, r.logs.String())
		}
	}
}
