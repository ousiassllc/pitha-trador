package event_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	tt "github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana/tachibanatest"
)

// Peek serves the merged value however old it is, without any request, and
// says whether the last update was an EVENT notification or a REST refill.
func TestPeekServesTheLatestMergedValueRegardlessOfAgeAndSaysHowItArrived(t *testing.T) {
	r := newRig(t, 10, 60)
	_ = r.feed.SetWatch(r.ctx, []string{"7203", "6758"})
	r.run()
	conn := r.fb.WaitEvent(t, 1)

	if _, _, ok := r.feed.Peek("7203"); ok {
		t.Fatal("Peek has a value before any notification")
	}
	_ = conn.Send(tt.FD(1, rows(map[int]map[string]string{1: {"pDPP": "3000", "pQBP": "2999", "pQAP": "3001"}})))
	_ = conn.Send(tt.SS(2, 1, true))
	tt.Eventually(t, func() bool { return r.feed.Statuses().SystemKnown })
	arrived := r.clk.Now()

	r.clk.Advance(2 * time.Hour) // far past the 30 seconds Latest tolerates
	q, at, ok := r.feed.Peek("7203")
	if !ok || q.Price != 3000 || *q.Bid != 2999 || !at.Equal(arrived) {
		t.Fatalf("Peek = %+v at %v (ok=%v), want the EVENT value stamped when it arrived (%v)", q, at, ok, arrived)
	}
	if r.feed.ViaREST("7203") {
		t.Error("ViaREST = true for an EVENT value")
	}
	if n := r.restRefills(); n != 0 {
		t.Errorf("REST requests = %d, Peek must never request", n)
	}

	// A REST refill (Latest of a stale value) replaces the value and marks it.
	r.fb.Respond(func(req tt.Request, _ int) (int, map[string]any) {
		if req.Body["sCLMID"] != "CLMMfdsGetMarketPrice" {
			return http.StatusOK, nil
		}
		return http.StatusOK, map[string]any{"sCLMID": "CLMMfdsGetMarketPrice", "p_errno": "0", "sResultCode": "0",
			"aCLMMfdsMarketPrice": []map[string]string{{"sIssueCode": "7203", "pDPP": "3100"}}}
	})
	if _, err := r.feed.Latest(context.Background(), "7203"); err != nil {
		t.Fatal(err)
	}
	q, _, ok = r.feed.Peek("7203")
	if !ok || q.Price != 3100 || !r.feed.ViaREST("7203") {
		t.Errorf("after the refill: Peek = %+v (ok=%v), ViaREST = %v, want 3100 via REST", q, ok, r.feed.ViaREST("7203"))
	}
	if r.feed.ViaREST("0000") {
		t.Error("ViaREST of an unknown symbol is true")
	}
}
