package rankingwatch

import "sync"

// Watchlist is the current watch list, shared between Watcher (writer) and
// the candidate refresh (reader, candidates.Refresher.Watch). It is empty
// until the first cycle and whenever the ranking is empty or failed and
// nothing is held, so the Scanner Dashboard never shows candidates from a
// ranking that is no longer there.
type Watchlist struct {
	mu      sync.RWMutex
	symbols map[string]struct{}
}

// Set replaces the watch list.
func (w *Watchlist) Set(symbols []string) {
	set := make(map[string]struct{}, len(symbols))
	for _, s := range symbols {
		set[s] = struct{}{}
	}
	w.mu.Lock()
	w.symbols = set
	w.mu.Unlock()
}

// Contains reports whether symbol is on the watch list.
func (w *Watchlist) Contains(symbol string) bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	_, ok := w.symbols[symbol]
	return ok
}

// Len is the number of watched symbols.
func (w *Watchlist) Len() int {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return len(w.symbols)
}
