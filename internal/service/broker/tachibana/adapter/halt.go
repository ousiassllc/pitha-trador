package adapter

import (
	"context"
	"sync"
)

// halter stops the whole 立花 connection (session loop and EVENT feed) when
// one of them has given up after repeated panics (issue #1211): the contexts
// handed to Start and Run are bound to it, and stop cancels both. The rest of
// the process is untouched.
type halter struct {
	mu      sync.Mutex
	stopped bool
	cancels []context.CancelFunc
}

// bind returns ctx made cancelable by stop (already canceled once stopped).
func (h *halter) bind(ctx context.Context) context.Context {
	ctx, cancel := context.WithCancel(ctx)
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.stopped {
		cancel()
	}
	h.cancels = append(h.cancels, cancel)
	return ctx
}

func (h *halter) stop() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.stopped = true
	for _, cancel := range h.cancels {
		cancel()
	}
}
