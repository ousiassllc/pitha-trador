package orderflow_test

import (
	"math"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/broker"
	"github.com/ousiassllc/pitha-trador/internal/service/orderflow"
)

func quote(bid, bidQty, ask, askQty float64) broker.Quote {
	return broker.Quote{Bid: new(bid), BidQty: new(bidQty), Ask: new(ask), AskQty: new(askQty)}
}

var t0 = time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)

func wantSum(t *testing.T, tr *orderflow.Tracker, at time.Time, window time.Duration, want float64) {
	t.Helper()
	got := tr.Sum("7203", at, window)
	if got == nil {
		t.Fatalf("Sum(%v) = nil, want %v", window, want)
	}
	if math.IsNaN(*got) || math.Abs(*got-want) > 1e-9 {
		t.Fatalf("Sum(%v) = %v, want %v", window, *got, want)
	}
}

func wantNil(t *testing.T, tr *orderflow.Tracker, at time.Time, window time.Duration) {
	t.Helper()
	if got := tr.Sum("7203", at, window); got != nil {
		t.Fatalf("Sum(%v) = %v, want nil", window, *got)
	}
}

// Cont-Kukanov-Stoikov's event rule for each of the bid-side and ask-side
// price moves (up / unchanged / down).
func TestObserve_EventRule(t *testing.T) {
	prev := quote(100, 10, 101, 20)
	cases := []struct {
		name string
		cur  broker.Quote
		want float64
	}{
		{"unchanged prices, quantities grow", quote(100, 15, 101, 25), (15 - 10) - (25 - 20)},
		{"bid up: new bid quantity counts in full", quote(100.5, 7, 101, 20), 7 - 0},
		{"bid down: previous bid quantity is removed", quote(99.5, 7, 101, 20), -10},
		{"ask down: new ask quantity is selling pressure", quote(100, 10, 100.5, 4), -4},
		{"ask up: previous ask quantity is removed", quote(100, 10, 101.5, 4), 20},
		{"identical book", quote(100, 10, 101, 20), 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tr := orderflow.NewTracker()
			tr.Observe("7203", prev, t0)
			tr.Observe("7203", c.cur, t0.Add(time.Second))
			wantSum(t, tr, t0.Add(time.Second), 10*time.Second, c.want)
		})
	}
}

func TestSum_RollingWindows(t *testing.T) {
	tr := orderflow.NewTracker()
	tr.Observe("7203", quote(100, 10, 101, 10), t0)
	tr.Observe("7203", quote(100, 20, 101, 10), t0.Add(5*time.Second))  // +10
	tr.Observe("7203", quote(100, 20, 101, 40), t0.Add(25*time.Second)) // -30
	tr.Observe("7203", quote(100, 50, 101, 40), t0.Add(55*time.Second)) // +30
	now := t0.Add(60 * time.Second)

	wantSum(t, tr, now, 10*time.Second, 30) // only the +30 at 55s
	wantSum(t, tr, now, 30*time.Second, 30) // the -30 at 25s is before the window
	wantSum(t, tr, now, 40*time.Second, 0)  // -30 (25s) and +30 (55s)
	wantSum(t, tr, now, 60*time.Second, 10) // +10 -30 +30
}

func TestSum_MissingData(t *testing.T) {
	tr := orderflow.NewTracker()
	wantNil(t, tr, t0, 10*time.Second) // never observed
	tr.Observe("7203", quote(100, 10, 101, 10), t0)
	wantNil(t, tr, t0, 10*time.Second) // first quote has no previous book: no event yet
	tr.Observe("7203", quote(100, 12, 101, 10), t0.Add(time.Second))
	wantNil(t, tr, t0.Add(time.Minute), 10*time.Second) // the only event left the window
	wantNil(t, tr, t0.Add(time.Second), 0)              // zero window
	wantNil(t, tr, t0.Add(time.Second), -time.Second)   // negative window
	if tr.Sum("9984", t0, 10*time.Second) != nil {
		t.Fatal("unknown symbol must have no OFI")
	}
}

// A missing side, an empty side, a crossed book or a special quote (板寄せ)
// must never produce NaN / Inf, and must not create an event across the gap.
func TestObserve_InvalidBooks(t *testing.T) {
	special := quote(100, 10, 101, 10)
	special.SpecialQuote = true
	nan := quote(math.NaN(), 10, 101, 10)
	invalid := map[string]broker.Quote{
		"no quote":         {},
		"missing ask":      {Bid: new(100.0), BidQty: new(10.0), AskQty: new(10.0)},
		"missing ask qty":  {Bid: new(100.0), BidQty: new(10.0), Ask: new(101.0)},
		"zero bid qty":     quote(100, 0, 101, 10),
		"zero ask qty":     quote(100, 10, 101, 0),
		"negative qty":     quote(100, -5, 101, 10),
		"zero price":       quote(0, 10, 101, 10),
		"crossed":          quote(102, 10, 101, 10),
		"special (板寄せ)":    special,
		"NaN price":        nan,
		"infinite qty":     quote(100, math.Inf(1), 101, 10),
		"infinite price":   quote(100, 10, math.Inf(1), 10),
		"negative price":   quote(-1, 10, 101, 10),
		"NaN quantity":     quote(100, math.NaN(), 101, 10),
		"inf price (both)": quote(math.Inf(1), 10, math.Inf(1), 10),
	}
	for name, bad := range invalid {
		t.Run(name, func(t *testing.T) {
			tr := orderflow.NewTracker()
			tr.Observe("7203", bad, t0)
			tr.Observe("7203", quote(100, 10, 101, 10), t0.Add(time.Second))
			wantNil(t, tr, t0.Add(time.Second), 10*time.Second) // only one valid book so far
			tr.Observe("7203", quote(100, 11, 101, 10), t0.Add(2*time.Second))
			wantSum(t, tr, t0.Add(2*time.Second), 10*time.Second, 1)
		})
	}
}

func TestObserve_InvalidBookBreaksTheChain(t *testing.T) {
	tr := orderflow.NewTracker()
	tr.Observe("7203", quote(100, 10, 101, 10), t0)
	halt := quote(100, 10, 101, 10)
	halt.SpecialQuote = true
	tr.Observe("7203", halt, t0.Add(time.Second))
	// After the auction the book reopens far away: no event is formed
	// between the pre-auction book and this one.
	tr.Observe("7203", quote(120, 500, 121, 400), t0.Add(2*time.Second))
	wantNil(t, tr, t0.Add(2*time.Second), 10*time.Second)
}

func TestObserve_OutOfOrderQuoteIgnored(t *testing.T) {
	tr := orderflow.NewTracker()
	tr.Observe("7203", quote(100, 10, 101, 10), t0.Add(2*time.Second))
	tr.Observe("7203", quote(100, 99, 101, 10), t0) // older than the previous one
	wantNil(t, tr, t0.Add(2*time.Second), 10*time.Second)
}

func TestObserve_SameSecondEventsAccumulate(t *testing.T) {
	tr := orderflow.NewTracker()
	tr.Observe("7203", quote(100, 10, 101, 10), t0)
	tr.Observe("7203", quote(100, 12, 101, 10), t0.Add(100*time.Millisecond)) // +2
	tr.Observe("7203", quote(100, 15, 101, 10), t0.Add(200*time.Millisecond)) // +3
	wantSum(t, tr, t0.Add(time.Second), 10*time.Second, 5)
}

func TestObserve_RetentionBoundsHistory(t *testing.T) {
	tr := orderflow.NewTracker()
	q := quote(100, 10, 101, 10)
	tr.Observe("7203", q, t0)
	for i := 1; i <= 1200; i++ {
		q = quote(100, 10+float64(i%2), 101, 10)
		tr.Observe("7203", q, t0.Add(time.Duration(i)*time.Second))
	}
	end := t0.Add(1200 * time.Second)
	// the longest window Retention allows still answers; events older than
	// that were dropped without affecting newer sums
	if tr.Sum("7203", end, orderflow.Retention) == nil {
		t.Fatal("Sum over Retention = nil, want a value")
	}
	wantNil(t, tr, end.Add(2*orderflow.Retention), orderflow.Retention)
}

func TestObserve_EmptySymbolIgnored(t *testing.T) {
	tr := orderflow.NewTracker()
	tr.Observe("", quote(100, 10, 101, 10), t0)
	tr.Observe("", quote(100, 11, 101, 10), t0.Add(time.Second))
	if tr.Sum("", t0.Add(time.Second), 10*time.Second) != nil {
		t.Fatal("empty symbol must be ignored")
	}
}
