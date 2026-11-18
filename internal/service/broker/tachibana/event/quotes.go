package event

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/broker"
	"github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana"
	"github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana/market"
)

// MaxEventAge is how old an EVENT-derived value may be and still be served by
// Latest without a REST refill (the kabu feed's 30 seconds, FR-SCHED-10).
const MaxEventAge = 30 * time.Second

// ErrStale is returned by Latest when the value is older than MaxEventAge and
// the REST refill is not allowed yet (at most one per refill interval): the
// value is treated as stale. It also matches broker.ErrPriceUnavailable.
var ErrStale = errors.New("tachibana: value is stale and the REST refill interval has not passed")

type symbolState struct {
	fields  map[string]string
	updated time.Time
	// rest: the last merge was a REST 時価 refill (false: an EVENT difference).
	rest bool
}

// merge folds items (EVENT differences or, with rest, a REST row) into
// symbol's latest values. Empty values carry no information and never overwrite.
func (f *Feed) merge(symbol string, items map[string]string, now time.Time, rest bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	st := f.states[symbol]
	if st == nil {
		st = &symbolState{fields: map[string]string{}}
		f.states[symbol] = st
	}
	for k, v := range items {
		if v != "" {
			st.fields[k] = v
		}
	}
	st.updated, st.rest = now, rest
}

// Peek returns symbol's latest merged value whatever its age, and when it was
// last updated; ok is false without a value (or without a positive 現在値). It
// never requests anything: the research 時価 recording (issue #1083) reads it
// each minute and asks Latest only for what is missing or stale.
func (f *Feed) Peek(symbol string) (q broker.Quote, updatedAt time.Time, ok bool) {
	f.mu.Lock()
	st := f.states[symbol]
	if st == nil {
		f.mu.Unlock()
		return broker.Quote{}, time.Time{}, false
	}
	fields := make(map[string]string, len(st.fields))
	for k, v := range st.fields {
		fields[k] = v
	}
	updatedAt = st.updated
	f.mu.Unlock()
	q, ok = market.QuoteFromFields(symbol, fields)
	return q, updatedAt, ok
}

// ViaREST reports whether symbol's latest value was last updated by a REST
// 時価 refill rather than an EVENT notification.
func (f *Feed) ViaREST(symbol string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	st := f.states[symbol]
	return st != nil && st.rest
}

// fresh returns symbol's quote when its latest values are at most MaxEventAge old.
func (f *Feed) fresh(symbol string) (broker.Quote, bool) {
	f.mu.Lock()
	st := f.states[symbol]
	if st == nil || f.clock.Now().Sub(st.updated) > MaxEventAge {
		f.mu.Unlock()
		return broker.Quote{}, false
	}
	fields := make(map[string]string, len(st.fields))
	for k, v := range st.fields {
		fields[k] = v
	}
	f.mu.Unlock()
	return market.QuoteFromFields(symbol, fields)
}

// Latest implements broker.StreamFeed: the EVENT value when it is at most 30
// seconds old, otherwise a REST 時価 refill of every stale watched symbol in
// the configured number of requests (one by default, at most 120 symbols
// each) - but never more often than the refill interval; in between the value is stale (ErrStale). A symbol the broker has
// no price for returns tachibana.ErrNoData.
func (f *Feed) Latest(ctx context.Context, symbol string) (broker.Quote, error) {
	if q, ok := f.fresh(symbol); ok {
		return q, nil
	}
	f.refillMu.Lock()
	defer f.refillMu.Unlock()
	if q, ok := f.fresh(symbol); ok { // refilled while this call waited
		return q, nil
	}
	now := f.clock.Now()
	if !f.lastRefill.IsZero() && now.Sub(f.lastRefill) < f.refillInterval {
		return broker.Quote{}, fmt.Errorf("%w: %w", broker.ErrPriceUnavailable, ErrStale)
	}
	f.lastRefill = now

	batch := f.refillBatch(symbol)
	rows, err := market.FetchFields(ctx, f.client, tachibana.PriorityWatchQuote, batch)
	for code, fields := range rows {
		if _, ok := market.QuoteFromFields(code, fields); ok {
			f.merge(code, fields, f.clock.Now(), true)
		}
	}
	slog.Debug("tachibana: REST quote refill", "symbols", len(batch), "answered", len(rows))
	if err != nil {
		return broker.Quote{}, err
	}
	if q, ok := f.fresh(symbol); ok {
		return q, nil
	}
	return broker.Quote{}, fmt.Errorf("%w: no price for %s", tachibana.ErrNoData, symbol)
}

// refillBatch is symbol first, then every other watched symbol whose value is
// stale, up to the refill's request budget (120 symbols per request).
func (f *Feed) refillBatch(symbol string) []string {
	batch := []string{symbol}
	f.mu.Lock()
	defer f.mu.Unlock()
	now := f.clock.Now()
	for _, s := range f.watchSetLocked() {
		if len(batch) >= f.refillSymbols {
			break
		}
		if st := f.states[s]; s != symbol && (st == nil || now.Sub(st.updated) > MaxEventAge) {
			batch = append(batch, s)
		}
	}
	return batch
}
