package pushfeed

import (
	"context"
	"fmt"
	"slices"
	"sync"

	"github.com/ousiassllc/pitha-trador/internal/service/marketdata"
)

// watchState is Feed's ranking-driven registration: instead of the first
// MaxRegisterSymbols of the universe, the PUSH subscription follows the
// watch list rankingwatch.Watcher decides every minute (functional.md
// FR-SCHED-9, at most 45 symbols).
type watchState struct {
	// mu serializes registration calls: SetWatch (watcher goroutine) and
	// Run's re-registration after a reconnect.
	mu      sync.Mutex
	enabled bool
	// symbols is the latest watch list, remembered even when registering it
	// failed so the next reconnect registers it.
	symbols []string
	// registered is what kabu is known to hold for us.
	registered []string
}

// UseWatchlist switches the Feed from registering the universe to registering
// the watch list set by SetWatch. Call it before Run.
func (f *Feed) UseWatchlist() {
	f.watch.mu.Lock()
	defer f.watch.mu.Unlock()
	f.watch.enabled = true
}

// register is what Run does before each (re)connect: the universe, or the
// current watch list when UseWatchlist was called. The registration list is
// emptied first because kabu station keeps registrations across app restarts
// and stale ones would leave no slot for the REST poll (4002006).
func (f *Feed) register(ctx context.Context) error {
	f.watch.mu.Lock()
	defer f.watch.mu.Unlock()
	if !f.watch.enabled {
		return f.RegisterUniverse(ctx)
	}
	f.watch.registered = nil
	if err := f.Broker.UnregisterAll(ctx); err != nil {
		return fmt.Errorf("pushfeed: unregister all symbols: %w", err)
	}
	return f.apply(ctx)
}

// SetWatch makes symbols the registered PUSH set: the symbols dropped since
// the last call are unregistered (freeing their slots whether or not kabu's
// PUT /register replaces the list), then the new list is registered. An empty
// list only unregisters. The latest set is remembered even when the call
// fails (no token yet, kabu down), so Run registers it after the next
// reconnect; the caller retries on its next cycle.
func (f *Feed) SetWatch(ctx context.Context, symbols []string) error {
	f.watch.mu.Lock()
	defer f.watch.mu.Unlock()
	f.watch.symbols = slices.Clone(symbols)
	return f.apply(ctx)
}

// apply registers watch.symbols; the caller holds watch.mu.
func (f *Feed) apply(ctx context.Context) error {
	w := &f.watch
	var dropped []marketdata.RegisterSymbol
	for _, sym := range w.registered {
		if !slices.Contains(w.symbols, sym) {
			dropped = append(dropped, marketdata.RegisterSymbol{Symbol: sym, Exchange: f.Exchange})
		}
	}
	if err := f.Broker.UnregisterSymbols(ctx, dropped); err != nil {
		return fmt.Errorf("pushfeed: unregister dropped watch symbols: %w", err)
	}
	w.registered = slices.DeleteFunc(w.registered, func(sym string) bool { return !slices.Contains(w.symbols, sym) })
	if len(w.symbols) == 0 {
		return nil
	}
	reg := make([]marketdata.RegisterSymbol, len(w.symbols))
	for i, sym := range w.symbols {
		reg[i] = marketdata.RegisterSymbol{Symbol: sym, Exchange: f.Exchange}
	}
	if _, err := f.Broker.RegisterSymbols(ctx, reg); err != nil {
		return fmt.Errorf("pushfeed: register watch symbols: %w", err)
	}
	w.registered = slices.Clone(w.symbols)
	return nil
}
