// Package orderflow accumulates each symbol's Order Flow Imbalance (OFI) from
// the stream of best-quote updates (Cont, Kukanov & Stoikov, "The Price Impact
// of Order Book Events", arXiv:1011.6402) so the Feature Engine can read it
// over rolling windows (functional.md §4.1 ofi_short/ofi_mid/ofi_long).
//
// OFI is a feature / filter input only (the meta-labelling input of
// issue #750): nothing here, nor any caller, may place an order from it.
package orderflow

import (
	"math"
	"sync"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/orderflowsettings"
	"github.com/ousiassllc/pitha-trador/internal/service/broker"
)

// Retention is how long events are kept: the widest window the Settings
// screen allows.
const Retention = orderflowsettings.MaxWindow

// bucket is the OFI contributed by every best-quote event whose timestamp
// falls in the second sec (Unix seconds). Summing per second keeps memory
// bounded by Retention however busy the PUSH feed is; window edges therefore
// have one-second resolution.
type bucket struct {
	sec int64
	ofi float64
}

// book is one valid best-quote state.
type book struct {
	bid, bidQty, ask, askQty float64
	at                       time.Time
}

type symbolState struct {
	prev    *book // nil until a valid book was seen, and again after an invalid one
	buckets []bucket
}

// Tracker holds the OFI history of every observed symbol. It is safe for
// concurrent use.
type Tracker struct {
	mu      sync.Mutex
	symbols map[string]*symbolState
}

// NewTracker returns an empty Tracker.
func NewTracker() *Tracker {
	return &Tracker{symbols: make(map[string]*symbolState)}
}

// validBook converts q into a book, or false when q cannot be a two-sided,
// tradable best quote: a missing side or quantity, a non-positive or
// non-finite price, a zero/negative/non-finite quantity (an empty side), a
// crossed book (Bid > Ask), or a special quote (特別気配 / 板寄せ, whose
// quantities do not describe resting liquidity at the displayed price).
func validBook(q broker.Quote, at time.Time) (book, bool) {
	if q.SpecialQuote || q.Bid == nil || q.Ask == nil || q.BidQty == nil || q.AskQty == nil {
		return book{}, false
	}
	b := book{bid: *q.Bid, bidQty: *q.BidQty, ask: *q.Ask, askQty: *q.AskQty, at: at}
	for _, price := range [...]float64{b.bid, b.ask} {
		if !(price > 0) || math.IsInf(price, 0) {
			return book{}, false
		}
	}
	for _, qty := range [...]float64{b.bidQty, b.askQty} {
		if !(qty > 0) || math.IsInf(qty, 0) {
			return book{}, false
		}
	}
	if b.bid > b.ask {
		return book{}, false
	}
	return b, true
}

// event is the OFI contribution of the best-quote move prev -> cur:
//
//	e = 1{Pb>=Pb'}·qb - 1{Pb<=Pb'}·qb' - 1{Pa<=Pa'}·qa + 1{Pa>=Pa'}·qa'
//
// (primes are the previous quote). Buy-side pressure is positive.
func event(prev, cur book) float64 {
	var e float64
	if cur.bid >= prev.bid {
		e += cur.bidQty
	}
	if cur.bid <= prev.bid {
		e -= prev.bidQty
	}
	if cur.ask <= prev.ask {
		e -= cur.askQty
	}
	if cur.ask >= prev.ask {
		e += prev.askQty
	}
	return e
}

// Observe records q, received at at, as symbol's latest best quote. An
// invalid quote (see validBook) contributes nothing and forgets the previous
// book, so the next valid quote starts a fresh chain instead of producing an
// event across the gap. A quote older than the previous one is ignored.
func (t *Tracker) Observe(symbol string, q broker.Quote, at time.Time) {
	if symbol == "" {
		return
	}
	cur, ok := validBook(q, at)

	t.mu.Lock()
	defer t.mu.Unlock()
	st := t.symbols[symbol]
	if st == nil {
		if !ok {
			return
		}
		st = &symbolState{}
		t.symbols[symbol] = st
	}
	if !ok {
		st.prev = nil
		return
	}
	if st.prev != nil && at.Before(st.prev.at) {
		return
	}
	if st.prev != nil {
		st.add(at, event(*st.prev, cur))
	}
	st.prev = &cur
	st.prune(at)
}

func (s *symbolState) add(at time.Time, ofi float64) {
	sec := at.Unix()
	if n := len(s.buckets); n > 0 && s.buckets[n-1].sec == sec {
		s.buckets[n-1].ofi += ofi
		return
	}
	s.buckets = append(s.buckets, bucket{sec: sec, ofi: ofi})
}

// prune drops buckets older than Retention before at.
func (s *symbolState) prune(at time.Time) {
	cutoff := at.Add(-Retention).Unix()
	drop := 0
	for drop < len(s.buckets) && s.buckets[drop].sec <= cutoff {
		drop++
	}
	if drop > 0 {
		s.buckets = append(s.buckets[:0], s.buckets[drop:]...)
	}
}

// Sum returns symbol's OFI over the trailing window ending at at: the sum of
// every event in the seconds (at-window, at]. It is nil when window is not
// positive, symbol was never observed, or no event fell inside the window -
// an unobserved book is "unknown", never an OFI of 0.
func (t *Tracker) Sum(symbol string, at time.Time, window time.Duration) *float64 {
	if window <= 0 {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	st := t.symbols[symbol]
	if st == nil {
		return nil
	}
	atSec := at.Unix()
	fromSec := atSec - int64(math.Ceil(window.Seconds())) // exclusive
	var sum float64
	found := false
	for _, b := range st.buckets {
		if b.sec > fromSec && b.sec <= atSec {
			sum += b.ofi
			found = true
		}
	}
	if !found {
		return nil
	}
	return &sum
}
