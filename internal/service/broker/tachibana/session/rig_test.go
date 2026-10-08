package session_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana"
	"github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana/adapter"
	"github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana/session"
	tt "github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana/tachibanatest"
)

// rig is a started adapter on a manual clock against the fake broker.
type rig struct {
	t      *testing.T
	clk    *tt.ManualClock
	fb     *tt.FakeBroker
	a      *adapter.Adapter
	ctx    context.Context
	cancel context.CancelFunc

	mu      sync.Mutex
	notices []session.Notice
}

func newRig(t *testing.T, start time.Time) *rig {
	t.Helper()
	r := &rig{t: t, clk: tt.NewManualClock(start)}
	r.fb = tt.New(t, r.clk)
	r.a = adapter.New(adapter.Config{
		Settings:    r.fb.Settings(),
		Credentials: tt.Credentials(),
		Notifier: session.NotifierFunc(func(_ context.Context, n session.Notice) {
			r.mu.Lock()
			r.notices = append(r.notices, n)
			r.mu.Unlock()
		}),
		HTTPClient: r.fb.Server().Client(),
		Clock:      r.clk,
	})
	r.ctx, r.cancel = context.WithCancel(context.Background())
	t.Cleanup(func() { r.cancel(); r.a.Run(r.ctx) })
	return r
}

// start runs Start and waits until the loop armed its first timer, so a later
// clock advance cannot overtake the loop's first wait computation.
func (r *rig) start() error {
	r.t.Helper()
	n := r.clk.Created()
	err := r.a.Start(r.ctx)
	r.clk.WaitCreated(r.t, n)
	return err
}

func (r *rig) countNotices(k session.NoticeKind) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, got := range r.notices {
		if got.Kind == k {
			n++
		}
	}
	return n
}

func (r *rig) totalNotices() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.notices)
}

func (r *rig) logins() int { return r.fb.Count("CLMAuthLoginRequest") }

// advance moves the clock and waits until the session loop re-armed.
func (r *rig) advance(d time.Duration) {
	r.t.Helper()
	n := r.clk.Created()
	r.clk.Advance(d)
	r.clk.WaitCreated(r.t, n)
}

func (r *rig) call() error {
	return r.a.Client().Call(r.ctx, tachibana.TargetPrice, tachibana.PriorityWatchQuote, "CLMTest", nil, nil)
}

// newAdapterWith builds the rig's adapter again with other settings.
func newAdapterWith(r *rig, settings config.TachibanaSettings) *adapter.Adapter {
	return adapter.New(adapter.Config{
		Settings:    settings,
		Credentials: tt.Credentials(),
		HTTPClient:  r.fb.Server().Client(),
		Clock:       r.clk,
	})
}
