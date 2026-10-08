package tachibana

import (
	"container/heap"
	"context"
	"sync"
	"time"
)

// Priority orders waiting requests: the lowest value is served first
// (integrations.md §5.3). Only the kinds below exist on purpose: there is no
// request type that polls every symbol's 時価 during the day (#720).
type Priority int

const (
	// PrioritySession is login/logout.
	PrioritySession Priority = iota
	// PriorityHeldQuote is 時価 of 保有銘柄 (and 注文中).
	PriorityHeldQuote
	// PriorityWatchQuote is 時価 of 監視銘柄.
	PriorityWatchQuote
	// PriorityMaster is the 朝1回 マスタ取得.
	PriorityMaster
	// PriorityHistory is the 夜間の日足取得 (issue #726). It never leaves the
	// queue between 8:00 and 15:30 JST.
	PriorityHistory
)

// daytimeRestricted reports whether requests of p wait out 8:00〜15:30.
func (p Priority) daytimeRestricted() bool { return p == PriorityHistory }

// gate admits one holder at a time, the highest priority waiter first (FIFO
// within a priority). Holding it from p_no numbering to the end of the
// response is what makes "one request in flight" and "send order = p_no
// order" true across the REQUEST, MASTER and PRICE virtual URLs.
type gate struct {
	mu      sync.Mutex
	busy    bool
	seq     uint64
	waiters waiterHeap
}

type waiter struct {
	prio  Priority
	seq   uint64
	ready chan struct{}
	index int // position in the heap, -1 once granted or withdrawn
}

// acquire blocks until the gate is free and p is the best waiting priority,
// or ctx is done. The returned function releases the gate once.
func (g *gate) acquire(ctx context.Context, p Priority) (func(), error) {
	g.mu.Lock()
	if !g.busy && len(g.waiters) == 0 {
		g.busy = true
		g.mu.Unlock()
		return g.releaser(), nil
	}
	g.seq++
	w := &waiter{prio: p, seq: g.seq, ready: make(chan struct{})}
	heap.Push(&g.waiters, w)
	g.mu.Unlock()

	select {
	case <-w.ready:
		return g.releaser(), nil
	case <-ctx.Done():
		g.mu.Lock()
		if w.index >= 0 { // still queued: withdraw
			heap.Remove(&g.waiters, w.index)
			g.mu.Unlock()
			return nil, ctx.Err()
		}
		g.mu.Unlock()
		// The gate was granted at the same moment: hand it on.
		g.release()
		return nil, ctx.Err()
	}
}

func (g *gate) releaser() func() {
	var once sync.Once
	return func() { once.Do(g.release) }
}

func (g *gate) release() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(g.waiters) == 0 {
		g.busy = false
		return
	}
	next := heap.Pop(&g.waiters).(*waiter)
	close(next.ready) // busy stays true: the gate passes straight to next
}

type waiterHeap []*waiter

func (h waiterHeap) Len() int { return len(h) }

func (h waiterHeap) Less(i, j int) bool {
	if h[i].prio != h[j].prio {
		return h[i].prio < h[j].prio
	}
	return h[i].seq < h[j].seq
}

func (h waiterHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].index, h[j].index = i, j
}

func (h *waiterHeap) Push(x any) {
	w := x.(*waiter)
	w.index = len(*h)
	*h = append(*h, w)
}

func (h *waiterHeap) Pop() any {
	old := *h
	n := len(old)
	w := old[n-1]
	old[n-1] = nil
	w.index = -1
	*h = old[:n-1]
	return w
}

// rateWindow keeps the number of sends in any one-second window at or under
// limit: the next send waits until the limit-th most recent one is a second
// old. Callers are serialized by the gate.
type rateWindow struct {
	mu    sync.Mutex
	limit int
	sent  []time.Time
}

func newRateWindow(limit int) *rateWindow { return &rateWindow{limit: limit} }

// wait blocks until a send is allowed and records it.
func (r *rateWindow) wait(ctx context.Context, clk Clock) error {
	r.mu.Lock()
	var at time.Time
	if len(r.sent) >= r.limit {
		at = r.sent[len(r.sent)-r.limit].Add(time.Second)
	}
	r.mu.Unlock()

	if !at.IsZero() {
		if err := Sleep(ctx, clk, at.Sub(clk.Now())); err != nil {
			return err
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sent = append(r.sent, clk.Now())
	if len(r.sent) > r.limit {
		r.sent = append(r.sent[:0], r.sent[len(r.sent)-r.limit:]...)
	}
	return nil
}

// waitOutDaytime blocks while p is daytime-restricted and the JST clock is in
// 8:00〜15:30, until the window ends.
func waitOutDaytime(ctx context.Context, clk Clock, p Priority) error {
	for p.daytimeRestricted() {
		now := clk.Now()
		if !InDaytime(now) {
			return nil
		}
		if err := Sleep(ctx, clk, AtClock(now, daytimeEnd).Sub(now)); err != nil {
			return err
		}
	}
	return nil
}
