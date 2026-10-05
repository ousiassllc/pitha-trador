// Package symbolcache memoises kabuステーションAPI 銘柄情報 lookups
// (marketdata.Client.GetSymbol) for the market-data job (issue #511).
package symbolcache

import (
	"context"
	"sync"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/marketdata"
)

// Getter fetches one symbol's marketdata.SymbolInfo (*Client).
type Getter interface {
	GetSymbol(ctx context.Context, symbol string, exchange int) (marketdata.SymbolInfo, error)
}

// jst is the Tokyo trading day's time zone: the price limits are fixed
// per trading day, so a cached entry is valid until JST midnight.
var jst = time.FixedZone("JST", 9*60*60)

// Cache serves SymbolInfo from memory, fetching each symbol at most once
// per JST day: 値幅制限 is fixed for the trading day and 貸借 changes
// rarely, so the per-minute market-data job must not hit /symbol for
// every symbol every cycle. A failed fetch is not cached. Safe for
// concurrent use.
type Cache struct {
	getter   Getter
	exchange int
	now      func() time.Time

	mu      sync.Mutex
	entries map[string]symbolEntry
}

type symbolEntry struct {
	info marketdata.SymbolInfo
	day  string // JST date the entry was fetched on
}

// New returns a Cache that fetches via getter for exchange.
func New(getter Getter, exchange int) *Cache {
	return &Cache{getter: getter, exchange: exchange, now: time.Now, entries: make(map[string]symbolEntry)}
}

// Get returns symbol's SymbolInfo as of today (JST), fetching it when it has
// not been fetched yet today.
func (c *Cache) Get(ctx context.Context, symbol string) (marketdata.SymbolInfo, error) {
	day := c.now().In(jst).Format(time.DateOnly)
	c.mu.Lock()
	e, ok := c.entries[symbol]
	c.mu.Unlock()
	if ok && e.day == day {
		return e.info, nil
	}

	info, err := c.getter.GetSymbol(ctx, symbol, c.exchange)
	if err != nil {
		return marketdata.SymbolInfo{}, err
	}
	c.mu.Lock()
	c.entries[symbol] = symbolEntry{info: info, day: day}
	c.mu.Unlock()
	return info, nil
}
