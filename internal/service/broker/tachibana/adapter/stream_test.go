package adapter_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/tachibanasource"
	"github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana/adapter"
	tt "github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana/tachibanatest"
)

// Issue #737 end to end on the fake e支店: login → master → EVENT → a neutral
// Quote from Latest, with no REST 時価 request while EVENT is fresh, and a
// clean stop (EVENT closed, logout) when the process ends.
func TestLoginMasterEventAndLatestEndToEnd(t *testing.T) {
	clk := tt.NewManualClock(tt.AtJST(2026, 10, 8, 6, 0))
	fb := tt.New(t, clk)
	fb.Respond(func(req tt.Request, _ int) (int, map[string]any) {
		if req.Body["sCLMID"] == "CLMStkGetIssueSizyouMstKabu" {
			return http.StatusOK, map[string]any{"sCLMID": "CLMStkGetIssueSizyouMstKabu", "p_errno": "0", "sResultCode": "0",
				"aCLMStkIssueSizyouMstKabu": []map[string]string{{"sIssueCode": "7203", "sZyouzyouSizyou": "00", "sNehabaMin": "2500", "sNehabaMax": "3500", "sSinyouC": "1"}}}
		}
		return http.StatusOK, nil
	})
	a := adapter.New(adapter.Config{Settings: fb.Settings(), Credentials: tt.Credentials(), HTTPClient: fb.Server().Client(), Clock: clk})
	ctx, cancel := context.WithCancel(context.Background())
	if err := a.Start(ctx); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { defer close(done); a.Run(ctx) }()

	if err := a.SetWatch(ctx, []string{"7203"}); err != nil {
		t.Fatal(err)
	}
	conn := fb.WaitEvent(t, 1)
	tt.Eventually(t, func() bool { _, err := a.SymbolInfo(ctx, "7203"); return err == nil })

	_ = conn.Send(tt.FD(1, map[int]map[string]string{1: {"pDPP": "3000", "pQBP": "2999", "pQAP": "3001", "pQAS": "0108"}}))
	_ = conn.Send(tt.SS(2, 1, true))
	tt.Eventually(t, func() bool { return a.Feed().Statuses().SystemKnown })

	q, err := a.Latest(ctx, "7203")
	if err != nil || q.Price != 3000 || *q.Bid != 2999 || *q.Ask != 3001 || !q.SpecialQuote {
		t.Fatalf("Latest = %+v, %v", q, err)
	}
	if n := fb.Count("CLMMfdsGetMarketPrice"); n != 0 {
		t.Errorf("REST 時価 requests = %d, want none while EVENT is fresh", n)
	}
	if caps := a.Capabilities(); caps.MaxStreamSymbols != 120 || caps.Ranking {
		t.Errorf("capabilities = %+v", caps)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return")
	}
	if fb.Count("CLMAuthLogoutRequest") != 1 {
		t.Errorf("logouts = %d, want 1", fb.Count("CLMAuthLogoutRequest"))
	}
}

// The 立花 source settings (issue #728) reach the stream: a budget of one
// connection a day leaves the first connect alone and suspends the swap.
func TestSourceSettingsSetTheEventConnectionBudget(t *testing.T) {
	clk := tt.NewManualClock(tt.AtJST(2026, 10, 8, 6, 0))
	fb := tt.New(t, clk)
	a := adapter.New(adapter.Config{
		Settings: fb.Settings(), Credentials: tt.Credentials(), HTTPClient: fb.Server().Client(), Clock: clk,
		Source: tachibanasource.TachibanaSourceSettings{EventMaxConnectsPerDay: 1},
	})
	ctx, cancel := context.WithCancel(context.Background())
	if err := a.Start(ctx); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { defer close(done); a.Run(ctx) }()
	defer func() { cancel(); <-done }()

	_ = a.SetWatch(ctx, []string{"7203"})
	fb.WaitEvent(t, 1)
	_ = a.SetWatch(ctx, []string{"7203", "6758"})
	time.Sleep(100 * time.Millisecond)
	if n := len(fb.Events()); n != 1 || a.Feed().ConnectionsToday() != 1 {
		t.Errorf("connections = %d (tally %d), want the swap suspended by the budget of 1", n, a.Feed().ConnectionsToday())
	}
}
