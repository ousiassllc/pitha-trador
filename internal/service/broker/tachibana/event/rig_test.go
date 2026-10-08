package event_test

import (
	"bytes"
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana"
	"github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana/event"
	tt "github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana/tachibanatest"
)

// rig is a logged-in client and an EVENT feed on a manual clock against the
// fake broker.
type rig struct {
	t    *testing.T
	clk  *tt.ManualClock
	fb   *tt.FakeBroker
	c    *tachibana.Client
	feed *event.Feed

	ctx     context.Context
	cancel  context.CancelFunc
	done    chan struct{}
	started bool
}

func newRig(t *testing.T, budget, refillSeconds int) *rig {
	t.Helper()
	r := &rig{t: t, clk: tt.NewManualClock(tt.AtJST(2026, 10, 8, 10, 0)), done: make(chan struct{})}
	r.fb = tt.New(t, r.clk)
	r.c = tachibana.NewClient(tachibana.Config{BaseURL: r.fb.BaseURL(), RequestsPerSecond: 10, HTTPClient: r.fb.Server().Client(), Clock: r.clk})
	if _, err := r.c.Login(context.Background(), tt.AuthID, r.fb.Key()); err != nil {
		t.Fatal(err)
	}
	r.feed = event.New(event.Config{
		Client: r.c, ConnectionBudget: budget, RefillInterval: time.Duration(refillSeconds) * time.Second,
		HTTPClient: r.fb.Server().Client(), Clock: r.clk,
	})
	r.ctx, r.cancel = context.WithCancel(context.Background())
	t.Cleanup(func() {
		r.cancel()
		if !r.started {
			return
		}
		select {
		case <-r.done:
		case <-time.After(10 * time.Second):
			t.Error("the feed did not stop")
		}
	})
	return r
}

// run starts the feed; call it once.
func (r *rig) run() {
	r.started = true
	go func() { defer close(r.done); r.feed.Run(r.ctx) }()
}

// advanceUntil moves the clock in one-second steps until cond holds.
func (r *rig) advanceUntil(cond func() bool) {
	r.t.Helper()
	for i := 0; i < 3000; i++ {
		if cond() {
			return
		}
		r.clk.Advance(time.Second)
		time.Sleep(time.Millisecond)
	}
	r.t.Fatal("condition not met while advancing the clock")
}

func (r *rig) restRefills() int { return r.fb.Count("CLMMfdsGetMarketPrice") }

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// captureLogs routes slog to a buffer for the test.
func captureLogs(t *testing.T) *syncBuffer {
	t.Helper()
	buf := &syncBuffer{}
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(old) })
	return buf
}
