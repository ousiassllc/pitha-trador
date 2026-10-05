package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"
)

// handlerTracker counts the requests whose handler is still running.
// http.Server.Shutdown does not wait for (or close) hijacked connections,
// i.e. the WebSocket streams, whose handlers keep running after it
// returns; the tracker is how httpServer.shutdown waits for them.
type handlerTracker struct {
	next http.Handler

	mu     sync.Mutex
	active int
	idle   chan struct{} // closed when active is 0 and a waiter is registered
}

func (t *handlerTracker) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	t.mu.Lock()
	t.active++
	t.mu.Unlock()
	defer t.done()
	t.next.ServeHTTP(w, r)
}

func (t *handlerTracker) done() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.active--
	if t.active == 0 && t.idle != nil {
		close(t.idle)
		t.idle = nil
	}
}

// wait blocks until no handler is running or ctx is done.
func (t *handlerTracker) wait(ctx context.Context) error {
	t.mu.Lock()
	if t.active == 0 {
		t.mu.Unlock()
		return nil
	}
	if t.idle == nil {
		t.idle = make(chan struct{})
	}
	idle := t.idle
	t.mu.Unlock()

	select {
	case <-idle:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("%w: handlers still running", ctx.Err())
	}
}

// httpServer is an http.Server whose request contexts all derive from one
// server-owned context that shutdown cancels.
type httpServer struct {
	*http.Server
	tracker    *handlerTracker
	cancelBase context.CancelFunc
}

func newHTTPServer(addr string, handler http.Handler) *httpServer {
	baseCtx, cancelBase := context.WithCancel(context.Background())
	tracker := &handlerTracker{next: handler}
	return &httpServer{
		Server: &http.Server{
			Addr:              addr,
			Handler:           tracker,
			ReadHeaderTimeout: 10 * time.Second,
			BaseContext:       func(net.Listener) context.Context { return baseCtx },
		},
		tracker:    tracker,
		cancelBase: cancelBase,
	}
}

// shutdown stops accepting connections and waits up to timeout for
// in-flight (non-hijacked) requests, then cancels the server context so
// the handlers of hijacked WebSocket connections (which run on their
// request's ctx) close their sockets and return, and waits up to timeout
// for them to do so. Only after it returns is it safe to stop the services
// and close the DB those handlers read from.
func (s *httpServer) shutdown(timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	shutdownErr := s.Shutdown(ctx)

	s.cancelBase()
	drainCtx, drainCancel := context.WithTimeout(context.Background(), timeout)
	defer drainCancel()
	drainErr := s.tracker.wait(drainCtx)
	return errors.Join(shutdownErr, drainErr)
}
