// Package symbolcache memoises broker 銘柄情報 lookups
// (broker.SymbolInfoSource) for the market-data job (issue #511).
package symbolcache

import (
	"context"
	"sync"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/broker"
)

// jst is the Tokyo trading day's time zone: the price limits are fixed
// per trading day, so a cached entry is valid until JST midnight.
var jst = time.FixedZone("JST", 9*60*60)

// Cache serves SymbolInfo from memory, fetching each symbol at most once
// per JST day: 値幅制限 is fixed for the trading day and 貸借 changes
// rarely, so the per-minute market-data job must not hit /symbol for
// every symbol every cycle. A failed fetch is not cached. Safe for
// concurrent use.
type Cache struct {
	source broker.SymbolInfoSource
	now    func() time.Time

	mu      sync.Mutex
	entries map[string]symbolEntry
}

type symbolEntry struct {
	info broker.SymbolInfo
	day  string // JST date the entry was fetched on
}

// New returns a Cache that fetches via source.
func New(source broker.SymbolInfoSource) *Cache {
	return &Cache{source: source, now: time.Now, entries: make(map[string]symbolEntry)}
}

// Get returns symbol's SymbolInfo as of today (JST), fetching it when it has
// not been fetched yet today.
func (c *Cache) Get(ctx context.Context, symbol string) (broker.SymbolInfo, error) {
	day := c.now().In(jst).Format(time.DateOnly)
	c.mu.Lock()
	e, ok := c.entries[symbol]
	c.mu.Unlock()
	if ok && e.day == day {
		return e.info, nil
	}

	info, err := c.source.SymbolInfo(ctx, symbol)
	if err != nil {
		return broker.SymbolInfo{}, err
	}
	c.mu.Lock()
	c.entries[symbol] = symbolEntry{info: info, day: day}
	c.mu.Unlock()
	return info, nil
}
