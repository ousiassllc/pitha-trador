package session_test

import (
	"context"
	"net/http"
	"net/url"
	"path"
	"sync"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/broker"
	"github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana"
	"github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana/session"
	tt "github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana/tachibanatest"
)

// harness is the client and session of the adapter without its data paths:
// the session tests look at the exact request sequence, which the master's
// fetch after a login (adapter, issue #735) would add to.
type harness struct {
	client *tachibana.Client
	sess   *session.Session
}

func newHarness(settings config.TachibanaSettings, n session.Notifier, hc *http.Client, clk tachibana.Clock) *harness {
	client := tachibana.NewClient(tachibana.Config{BaseURL: settings.BaseURL(), RequestsPerSecond: settings.RequestMaxPerSecond, HTTPClient: hc, Clock: clk})
	u, _ := url.Parse(settings.BaseURL())
	sess := session.New(session.Config{
		Client: client, AuthID: tt.AuthID, KeyPath: settings.PrivateKeyPath(), ReauthTime: settings.ReauthTime,
		APIVersion: path.Base(u.Path), Clock: clk, Notifier: n,
	})
	return &harness{client: client, sess: sess}
}

func (h *harness) Client() *tachibana.Client            { return h.client }
func (h *harness) Session() *session.Session            { return h.sess }
func (h *harness) Status() broker.SessionStatus         { return h.sess.Status() }
func (h *harness) BoardFailures() *domain.FailureStreak { return h.client.BoardFailures() }
func (h *harness) Start(ctx context.Context) error      { return h.sess.Start(ctx) }
func (h *harness) Run(ctx context.Context)              { <-ctx.Done(); h.sess.Wait() }

// rig is a started session on a manual clock against the fake broker.
type rig struct {
	t      *testing.T
	clk    *tt.ManualClock
	fb     *tt.FakeBroker
	a      *harness
	ctx    context.Context
	cancel context.CancelFunc

	mu      sync.Mutex
	notices []session.Notice
}

func newRig(t *testing.T, start time.Time) *rig {
	t.Helper()
	r := &rig{t: t, clk: tt.NewManualClock(start)}
	r.fb = tt.New(t, r.clk)
	r.a = newHarness(r.fb.Settings(), session.NotifierFunc(func(_ context.Context, n session.Notice) {
		r.mu.Lock()
		r.notices = append(r.notices, n)
		r.mu.Unlock()
	}), r.fb.Server().Client(), r.clk)
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

// newAdapterWith builds the rig's session again with other settings.
func newAdapterWith(r *rig, settings config.TachibanaSettings) *harness {
	return newHarness(settings, nil, r.fb.Server().Client(), r.clk)
}
