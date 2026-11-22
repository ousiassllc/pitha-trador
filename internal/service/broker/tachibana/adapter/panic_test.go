package adapter_test

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/broker"
	"github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana"
	"github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana/adapter"
	"github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana/guard"
	"github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana/session"
	tt "github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana/tachibanatest"
)

// wsPanics panics in the goroutine that dials the EVENT WebSocket (the feed's)
// and passes every other request (the REST client's) through.
type wsPanics struct {
	next   http.RoundTripper
	panics atomic.Int32
}

func (w *wsPanics) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Header.Get("Upgrade") == "websocket" {
		w.panics.Add(1)
		panic("injected EVENT dial panic")
	}
	return w.next.RoundTrip(r)
}

// When the EVENT feed panics past its restart limit the adapter stops the
// whole 立花 connection and nothing else: Run returns, the session logs out and
// reports "stopped" (status and notice), requests get ErrNoSession, and the
// caller's own context is still alive (the app keeps running).
func TestFeedGivingUpStopsTheTachibanaConnectionOnly(t *testing.T) {
	clk := tt.NewManualClock(tt.AtJST(2026, 10, 8, 10, 0))
	fb := tt.New(t, clk)
	hc := *fb.Server().Client()
	rt := &wsPanics{next: hc.Transport}
	hc.Transport = rt

	var stopped atomic.Int32
	a := adapter.New(adapter.Config{
		Settings: fb.Settings(), Credentials: tt.Credentials(), HTTPClient: &hc, Clock: clk,
		Notifier: session.NotifierFunc(func(_ context.Context, n session.Notice) {
			if n.Kind == session.NoticeStopped {
				stopped.Add(1)
			}
		}),
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := a.Start(ctx); err != nil {
		t.Fatal(err)
	}
	_ = a.SetWatch(ctx, []string{"7203"})
	done := make(chan struct{})
	go func() { defer close(done); a.Run(ctx) }()

	for i := 0; i < 3000; i++ {
		select {
		case <-done:
		default:
			clk.Advance(time.Second)
			time.Sleep(time.Millisecond)
			continue
		}
		break
	}
	select {
	case <-done:
	default:
		t.Fatal("Run did not return after the feed gave up")
	}

	if got, want := int(rt.panics.Load()), 1+guard.MaxRestarts; got != want {
		t.Errorf("EVENT dial panics = %d, want %d", got, want)
	}
	if st := a.Status(); st.Issue != broker.SessionIssueStopped || st.Guidance == "" {
		t.Errorf("status = %+v, want stopped with guidance", st)
	}
	if stopped.Load() != 1 {
		t.Errorf("stopped notices = %d, want 1", stopped.Load())
	}
	err := a.Client().Call(ctx, tachibana.TargetPrice, tachibana.PriorityWatchQuote, "CLMTest", nil, nil)
	if !errors.Is(err, broker.ErrNoSession) {
		t.Errorf("request after the stop = %v, want ErrNoSession", err)
	}
	if ctx.Err() != nil {
		t.Error("the caller's context must stay alive: only the connection stops")
	}
}
