package marketdata

import (
	"sync"
	"time"
)

// BoardCache holds the latest PUSH-delivered Board per symbol so a
// consumer can use a recent PUSH snapshot instead of waiting on a REST
// poll (overview.md §5 "銘柄登録・PUSH購読"). It is safe for concurrent
// use.
type BoardCache struct {
	mu     sync.RWMutex
	boards map[string]cachedBoard
}

type cachedBoard struct {
	board Board
	at    time.Time
}

// NewBoardCache returns an empty BoardCache.
func NewBoardCache() *BoardCache {
	return &BoardCache{boards: make(map[string]cachedBoard)}
}

// Put stores board (received at at) as its symbol's latest. A board
// without a usable current price (Board.HasPrice) is dropped so it can
// neither replace a good cached board nor be served later (issue #173).
func (c *BoardCache) Put(board Board, at time.Time) {
	if board.Symbol == "" || !board.HasPrice() {
		return
	}
	c.mu.Lock()
	c.boards[board.Symbol] = cachedBoard{board: board, at: at}
	c.mu.Unlock()
}

// Fresh returns symbol's latest cached board if it was received no more
// than maxAge before now.
func (c *BoardCache) Fresh(symbol string, now time.Time, maxAge time.Duration) (Board, bool) {
	c.mu.RLock()
	cb, ok := c.boards[symbol]
	c.mu.RUnlock()
	if !ok || now.Sub(cb.at) > maxAge {
		return Board{}, false
	}
	return cb.board, true
}
