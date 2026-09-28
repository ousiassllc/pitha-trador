package marketdata

import (
	"sync"
	"time"
)

// SymbolStatus is the last known freshness state for one symbol
// (docs/architecture/overview.md §5 "異常時": kabuステーションAPI無応答・
// エラー時は該当銘柄をstale data判定し新規取引を禁止する).
type SymbolStatus struct {
	// LastUpdated is when this symbol last received a successful REST
	// response or PUSH message. Zero if never updated.
	LastUpdated time.Time
	// Stale is true when the most recent attempt to refresh this symbol
	// failed (REST error/timeout) or no PUSH/REST update has ever been
	// recorded.
	Stale bool
	// LastError is the error from the most recent failed refresh, or nil
	// if the symbol is currently fresh.
	LastError error
}

// StatusTracker records, per symbol, whether the most recently observed
// kabuステーションAPI data (REST poll or PUSH message) is fresh or stale.
// Feature Engine and other downstream services query it before consuming a
// symbol's market data so unresponsive/erroring symbols can be excluded
// (overview.md §5, functional.md 障害対応方針). It is safe for concurrent
// use.
type StatusTracker struct {
	mu    sync.RWMutex
	state map[string]SymbolStatus
}

// NewStatusTracker returns an empty StatusTracker. Symbols with no
// recorded status are considered stale by IsStale until their first
// MarkFresh/MarkStale call.
func NewStatusTracker() *StatusTracker {
	return &StatusTracker{state: make(map[string]SymbolStatus)}
}

// MarkFresh records a successful update for symbol at "at", clearing any
// prior stale/error state.
func (t *StatusTracker) MarkFresh(symbol string, at time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.state[symbol] = SymbolStatus{LastUpdated: at}
}

// MarkStale records that symbol's data could not be refreshed, keeping
// whatever LastUpdated timestamp was previously recorded so callers can
// see how long ago fresh data existed.
func (t *StatusTracker) MarkStale(symbol string, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	prev := t.state[symbol]
	t.state[symbol] = SymbolStatus{LastUpdated: prev.LastUpdated, Stale: true, LastError: err}
}

// IsStale reports whether symbol is currently marked stale. Symbols that
// have never been recorded are treated as stale (no fresh data has ever
// been observed for them).
func (t *StatusTracker) IsStale(symbol string) bool {
	status, ok := t.Status(symbol)
	if !ok {
		return true
	}
	return status.Stale
}

// Status returns symbol's current tracked status and whether any status
// has been recorded for it yet.
func (t *StatusTracker) Status(symbol string) (SymbolStatus, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	status, ok := t.state[symbol]
	return status, ok
}
