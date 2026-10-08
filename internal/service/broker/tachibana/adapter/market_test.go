package adapter_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana"
	"github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana/adapter"
	tt "github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana/tachibanatest"
)

// Issue #735: the morning login loads the master once; SymbolInfo and Quote
// answer from it and from 時価, and a second login the same day does not
// fetch the master again.
func TestLoginLoadsMasterOnceAndAdapterServesSymbolInfoAndQuote(t *testing.T) {
	clk := tt.NewManualClock(tt.AtJST(2026, 10, 8, 6, 0))
	fb := tt.New(t, clk)
	fb.Respond(func(req tt.Request, _ int) (int, map[string]any) {
		switch req.Body["sCLMID"] {
		case "CLMStkGetIssueSizyouMstKabu":
			return http.StatusOK, map[string]any{"sCLMID": "CLMStkGetIssueSizyouMstKabu", "p_errno": "0", "sResultCode": "0",
				"aCLMStkIssueSizyouMstKabu": []map[string]string{
					{"sIssueCode": "7203", "sZyouzyouSizyou": "00", "sNehabaMin": "2500.000000", "sNehabaMax": "3500.000000", "sSinyouC": "1"}}}
		case "CLMMfdsGetMarketPrice":
			return http.StatusOK, map[string]any{"sCLMID": "CLMMfdsGetMarketPrice", "p_errno": "0", "sResultCode": "0",
				"aCLMMfdsMarketPrice": []map[string]string{{"sIssueCode": "7203", "pDPP": "3000", "pQBP": "2999", "pQAP": "3001", "pQAS": "0102"}}}
		}
		return http.StatusOK, nil
	})
	a := adapter.New(adapter.Config{Settings: fb.Settings(), Credentials: tt.Credentials(), HTTPClient: fb.Server().Client(), Clock: clk})
	ctx, cancel := context.WithCancel(context.Background())
	if err := a.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { cancel(); a.Run(ctx) }()

	tt.Eventually(t, func() bool { return fb.Count("CLMStkGetIssueSizyouMstKabu") == 1 })
	var infoErr error
	tt.Eventually(t, func() bool { _, infoErr = a.SymbolInfo(ctx, "7203"); return infoErr == nil })
	info, _ := a.SymbolInfo(ctx, "7203")
	if !*info.Lendable || *info.UpperLimit != 3500 || *info.LowerLimit != 2500 {
		t.Errorf("SymbolInfo = %+v", info)
	}
	if _, err := a.SymbolInfo(ctx, "0000"); !errors.Is(err, tachibana.ErrNoData) {
		t.Errorf("unlisted symbol: %v, want ErrNoData", err)
	}

	q, err := a.Quote(ctx, "7203")
	if err != nil || *q.Bid != 2999 || *q.Ask != 3001 || !q.SpecialQuote {
		t.Errorf("Quote = %+v, %v", q, err)
	}
	if _, err := a.Quote(ctx, "1111"); !errors.Is(err, tachibana.ErrNoData) {
		t.Errorf("Quote of a symbol without data: %v, want ErrNoData", err)
	}

	// Another login the same day (a lost session) must not refetch the master.
	if err := a.Session().LoginNow(ctx); err != nil {
		t.Fatal(err)
	}
	if fb.Count("CLMAuthLoginRequest") != 2 {
		t.Fatal("expected a second login")
	}
	if _, err := a.Quote(ctx, "7203"); err != nil { // a request after the OnLogin goroutine had its chance
		t.Fatal(err)
	}
	if n := fb.Count("CLMStkGetIssueSizyouMstKabu"); n != 1 {
		t.Errorf("master requests = %d, want 1 for the day", n)
	}
}
