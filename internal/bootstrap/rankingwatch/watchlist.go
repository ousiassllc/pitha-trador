package rankingwatch

import "sync"

// Watchlist is the symbols the candidate refresh screens, shared between
// Watcher (writer) and candidates.Refresher.Watch (reader). In the session it
// is the watch list; outside it, the watch list of the last in-session cycle
// (plus the held symbols), so the Scanner Dashboard goes on showing the
// candidates from the stored data (FR-SCAN-7). It is empty until the first
// cycle and whenever an in-session ranking is empty or failed and nothing is
// held, so the dashboard never shows candidates from a ranking that is no
// longer there.
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
