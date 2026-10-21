package proposal_test

import (
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/proposal"
)

func validArgs() proposal.Args {
	qty, stop, loss := int64(100), 950.0, 5000.0
	plan := "  明日の寄りで手じまい  "
	return proposal.Args{
		IdempotencyKey: "proposal-0001", Symbol: "7203", Bucket: proposal.BucketStock, Action: proposal.ActionOpenLong,
		Quantity: &qty, OrderType: proposal.OrderMarket, StopLossPrice: &stop, ExitPlan: &plan, MaxLossJPY: &loss,
		Rationale: " 決算が良い ",
		Sources:   []proposal.Source{{Title: "決算短信", URL: "https://example.com/a", RetrievedAt: "2026-10-18T09:00:00+09:00"}},
		ExpiresAt: "2026-10-18T21:00:00+09:00",
	}
}

// The request hash is the identity of a proposal's content: spelling that does
// not change the content (surrounding spaces, the zone an instant is written
// in) hashes the same, the idempotency_key is not part of it, and any change of
// content changes it.
func TestRequestHashCoversContentNotSpelling(t *testing.T) {
	base, err := validArgs().Validate()
	if err != nil {
		t.Fatal(err)
	}

	same := validArgs()
	same.IdempotencyKey = "another-key-0002"
	same.Rationale = "決算が良い"
	same.ExpiresAt = "2026-10-18T12:00:00Z"
	same.Sources[0].RetrievedAt = "2026-10-18T00:00:00Z"
	got, err := same.Validate()
	if err != nil {
		t.Fatal(err)
	}
	if got.Hash() != base.Hash() || len(base.Hash()) != 64 {
		t.Fatalf("equivalent spellings hash differently: %s vs %s", got.Hash(), base.Hash())
	}

	for name, mutate := range map[string]func(*proposal.Args){
		"symbol":     func(a *proposal.Args) { a.Symbol = "6758" },
		"quantity":   func(a *proposal.Args) { q := int64(200); a.Quantity = &q },
		"rationale":  func(a *proposal.Args) { a.Rationale = "別の理由" },
		"expires_at": func(a *proposal.Args) { a.ExpiresAt = "2026-10-18T22:00:00+09:00" },
		"stop":       func(a *proposal.Args) { s := 940.0; a.StopLossPrice = &s },
		"source":     func(a *proposal.Args) { a.Sources[0].Title = "別の資料" },
	} {
		a := validArgs()
		mutate(&a)
		r, err := a.Validate()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if r.Hash() == base.Hash() {
			t.Errorf("changing %s did not change the hash", name)
		}
	}
}

// expires_at must be after the receipt time and at most 24 hours later.
func TestCheckExpiryWindow(t *testing.T) {
	now := time.Date(2026, 10, 18, 3, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name string
		at   time.Time
		ok   bool
	}{
		{"one second after", now.Add(time.Second), true},
		{"exactly 24 hours", now.Add(24 * time.Hour), true},
		{"24 hours and a second", now.Add(24*time.Hour + time.Second), false},
		{"the receipt time itself", now, false},
		{"the past", now.Add(-time.Hour), false},
	} {
		err := proposal.Request{ExpiresAt: tc.at}.CheckExpiry(now)
		if (err == nil) != tc.ok {
			t.Errorf("%s: CheckExpiry = %v, want ok=%v", tc.name, err, tc.ok)
		}
		if _, isInvalid := proposal.IsInvalid(err); err != nil && !isInvalid {
			t.Errorf("%s: %v is not an InvalidError", tc.name, err)
		}
	}
}
