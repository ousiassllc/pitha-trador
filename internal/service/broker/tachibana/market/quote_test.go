package market_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana"
	"github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana/market"
	tt "github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana/tachibanatest"
)

func priceRow(code string, extra map[string]string) map[string]string {
	r := map[string]string{"sIssueCode": code, "pDPP": "1000", "tDPP:T": "10:00"}
	for k, v := range extra {
		r[k] = v
	}
	return r
}

func servePrices(fb *tt.FakeBroker, rows func(codes []string) []map[string]string) {
	fb.Respond(func(req tt.Request, _ int) (int, map[string]any) {
		if req.Body["sCLMID"] != "CLMMfdsGetMarketPrice" {
			return http.StatusOK, nil
		}
		return http.StatusOK, ok("CLMMfdsGetMarketPrice", map[string]any{"aCLMMfdsMarketPrice": rows(strings.Split(req.Body["sTargetIssueCode"], ","))})
	})
}

func TestQuoteMapsBidAskVolumeDepthAndSpecialQuote(t *testing.T) {
	tests := []struct {
		name    string
		extra   map[string]string
		special bool
	}{
		{"general", map[string]string{"pQAS": "0101", "pQBS": "0101"}, false},
		{"special sell quote 0102", map[string]string{"pQAS": "0102", "pQBS": "0101"}, true},
		{"special buy quote 0102", map[string]string{"pQAS": "0101", "pQBS": "0102"}, true},
		{"pre-stop 0108", map[string]string{"pQAS": "0108", "pQBS": "0000"}, true},
		{"other kinds are not special (寄前気配 0107)", map[string]string{"pQAS": "0107", "pQBS": "0107"}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c, fb, _ := setup(t)
			extra := map[string]string{
				"pDPP": "6128", "pDOP": "6057", "pDHP": "6138", "pDLP": "6048", "pDV": "3864200", "pDJ": "23627822400", "pVWAP": "6100.5", "pPRP": "6042",
				"pQBP": "6128", "pBV": "1900", "pQAP": "6129", "pAV": "4800",
				"pGBV1": "100", "pGBV2": "200", "pGBV3": "50", "pGAV1": "10", "pGAV2": "5",
			}
			for k, v := range tc.extra {
				extra[k] = v
			}
			servePrices(fb, func(codes []string) []map[string]string { return []map[string]string{priceRow(codes[0], extra)} })

			q, err := market.FetchQuote(context.Background(), c, tachibana.PriorityWatchQuote, "6501")
			if err != nil {
				t.Fatal(err)
			}
			if q.Symbol != "6501" || q.Price != 6128 || q.VWAP != 6100.5 || q.Volume != 3864200 || q.Turnover != 23627822400 || *q.High != 6138 || *q.Low != 6048 {
				t.Errorf("quote = %+v", q)
			}
			// QBP is the best BUY (Bid), QAP the best SELL (Ask): no kabu-style swap.
			if *q.Bid != 6128 || *q.BidQty != 1900 || *q.Ask != 6129 || *q.AskQty != 4800 {
				t.Errorf("bid/ask = %v/%v %v/%v, want 6128/1900 6129/4800", *q.Bid, *q.BidQty, *q.Ask, *q.AskQty)
			}
			if *q.BidDepth != 350 || *q.AskDepth != 15 {
				t.Errorf("depth = %v/%v, want 350/15", *q.BidDepth, *q.AskDepth)
			}
			if q.SpecialQuote != tc.special {
				t.Errorf("SpecialQuote = %v, want %v", q.SpecialQuote, tc.special)
			}
			raw, err := json.Marshal(q.Raw)
			if err != nil || !strings.Contains(string(raw), `"sIssueCode":"6501"`) || !strings.Contains(string(raw), `"pQAP":"6129"`) {
				t.Errorf("raw = %s, %v; want the broker's row", raw, err)
			}
		})
	}
}

func TestQuoteMissingBoardAndNumbersAreMissingNotZero(t *testing.T) {
	c, fb, _ := setup(t)
	servePrices(fb, func(codes []string) []map[string]string {
		return []map[string]string{priceRow(codes[0], map[string]string{"pQBP": "", "pQAP": "oxox", "pVWAP": ""})}
	})
	q, err := market.FetchQuote(context.Background(), c, tachibana.PriorityWatchQuote, "6501")
	if err != nil {
		t.Fatal(err)
	}
	if q.Bid != nil || q.Ask != nil || q.BidDepth != nil || q.AskDepth != nil || q.VWAP != 0 || q.SpecialQuote {
		t.Errorf("quote = %+v, want missing best quotes/depth and no VWAP (FR-FE-2)", q)
	}
}

func TestQuoteRequestShape(t *testing.T) {
	c, fb, _ := setup(t)
	servePrices(fb, func(codes []string) []map[string]string { return []map[string]string{priceRow(codes[0], nil)} })
	if _, err := market.FetchQuote(context.Background(), c, tachibana.PriorityHeldQuote, "6501"); err != nil {
		t.Fatal(err)
	}
	var req tt.Request
	for _, r := range fb.Requests() {
		if r.Body["sCLMID"] == "CLMMfdsGetMarketPrice" {
			req = r
		}
	}
	if !strings.Contains(req.Path, "/price/") {
		t.Errorf("path = %q, want the PRICE virtual URL", req.Path)
	}
	cols := strings.Split(req.Body["sTargetColumn"], ",")
	for _, want := range []string{"pDPP", "tDPP:T", "pDOP", "pDHP", "pDLP", "pDV", "pDJ", "pVWAP", "pPRP", "pQAP", "pQAS", "pQBP", "pQBS", "pAV", "pBV", "pGAP1", "pGAP10", "pGAV1", "pGAV10", "pGBP1", "pGBP10", "pGBV1", "pGBV10"} {
		found := false
		for _, c := range cols {
			found = found || c == want
		}
		if !found {
			t.Errorf("sTargetColumn lacks %s: %v", want, cols)
		}
	}
	if req.Body["sTargetIssueCode"] != "6501" {
		t.Errorf("sTargetIssueCode = %q", req.Body["sTargetIssueCode"])
	}
}

func TestQuotesSplitAt120SymbolsAndDropDuplicates(t *testing.T) {
	c, fb, _ := setup(t)
	var mu sync.Mutex
	var sizes []int
	servePrices(fb, func(codes []string) []map[string]string {
		mu.Lock()
		sizes = append(sizes, len(codes))
		mu.Unlock()
		var rows []map[string]string
		for _, code := range codes {
			if code != "0100" { // the broker has nothing for this one
				rows = append(rows, priceRow(code, nil))
			}
		}
		return rows
	})
	var symbols []string
	for i := 0; i < 250; i++ {
		symbols = append(symbols, fmt.Sprintf("%04d", i))
	}
	symbols = append(symbols, "0001", " ", "")
	quotes, err := market.FetchQuotes(context.Background(), c, tachibana.PriorityWatchQuote, symbols)
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(sizes) != "[120 120 10]" {
		t.Errorf("request sizes = %v, want [120 120 10]", sizes)
	}
	if len(quotes) != 249 {
		t.Errorf("quotes = %d, want 249 (250 distinct minus the one without data)", len(quotes))
	}
	if _, has := quotes["0100"]; has {
		t.Error("a symbol the broker has no data for must be absent")
	}
}

// A symbol without data is a per-symbol outcome: ErrNoData, and the feed-wide
// market_data_down streak is reset, not extended.
func TestSymbolWithoutDataIsNotAFeedFailure(t *testing.T) {
	c, fb, _ := setup(t)
	fb.Respond(func(req tt.Request, _ int) (int, map[string]any) {
		if req.Body["sCLMID"] == "CLMMfdsGetMarketPrice" && req.Body["sTargetIssueCode"] == "BAD" {
			return http.StatusOK, ok("CLMMfdsGetMarketPrice", map[string]any{"aCLMMfdsMarketPrice": []map[string]string{{"sIssueCode": "BAD"}}})
		}
		if req.Body["sCLMID"] == "CLMMfdsGetMarketPrice" {
			return http.StatusOK, tt.ControlError("9", "stopped")
		}
		return http.StatusOK, nil
	})
	for i := 0; i < 2; i++ {
		if _, err := market.FetchQuote(context.Background(), c, tachibana.PriorityWatchQuote, "7203"); err == nil {
			t.Fatal("want the feed failure")
		}
	}
	if c.BoardFailures().ConsecutiveFailures() != 2 {
		t.Fatalf("streak = %d, want 2", c.BoardFailures().ConsecutiveFailures())
	}
	_, err := market.FetchQuote(context.Background(), c, tachibana.PriorityWatchQuote, "BAD")
	if !errors.Is(err, tachibana.ErrNoData) {
		t.Fatalf("err = %v, want ErrNoData", err)
	}
	if n := c.BoardFailures().ConsecutiveFailures(); n != 0 {
		t.Errorf("streak after a symbol without data = %d, want 0 (the request itself succeeded)", n)
	}
}

// Master, 全銘柄マスタ and 時価 requests share the one serial queue: never two
// in flight, whichever virtual URL they use.
func TestMasterAndPriceRequestsNeverOverlap(t *testing.T) {
	c, fb, clk := setup(t)
	fb.Respond(func(req tt.Request, _ int) (int, map[string]any) {
		switch req.Body["sCLMID"] {
		case "CLMStkGetIssueSizyouMstKabu":
			return http.StatusOK, ok("CLMStkGetIssueSizyouMstKabu", map[string]any{"aCLMStkIssueSizyouMstKabu": masterRows()})
		case "CLMMfdsGetMarketPrice":
			return http.StatusOK, ok("CLMMfdsGetMarketPrice", map[string]any{"aCLMMfdsMarketPrice": []map[string]string{priceRow("6501", nil)}})
		}
		return http.StatusOK, nil
	})
	m := market.NewMaster(c, clk)
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); _ = m.Load(context.Background()) }()
		go func() {
			defer wg.Done()
			_, _ = market.FetchQuotes(context.Background(), c, tachibana.PriorityWatchQuote, []string{"6501"})
		}()
	}
	wg.Wait()
	if n := fb.MaxInFlight(); n != 1 {
		t.Errorf("max requests in flight = %d, want 1", n)
	}
}
