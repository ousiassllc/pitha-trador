// Package researchhub owns the one 立花 e支店 session the research-only
// collections share (issues #1082, #1083): the nightly daily-bar collection
// and the intraday 時価 recording both record 立花 data next to the kabu
// broker, and a 認証ID admits one session, so they take the same adapter.
//
// The hub creates the adapter, and logs in, lazily on the first Acquire. The
// callers acquire only when they are about to need the broker (the daily-bar
// collection at night, the 時価 recording shortly before the session opens), so
// a switch that is off, or an unset credential, never reaches the broker: no
// login, no request, no connection. It is inert while 立花 is the selected
// broker (that adapter already owns the 認証ID's session). Nothing here is a
// trading input.
package researchhub

import (
	"context"
	"errors"
	"sync"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/config/tachibanasource"
	"github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana/adapter"
)

var (
	// ErrNotConfigured: the 認証ID or the 秘密鍵 path of the selected environment is unset.
	ErrNotConfigured = errors.New("researchhub: 立花の認証ID または 秘密鍵のパスが未設定です")
	// ErrBrokerIsTachibana: 立花 is the selected broker, which owns the session.
	ErrBrokerIsTachibana = errors.New("researchhub: ブローカーが立花のため、研究用の収集は動きません")
	// ErrNotStarted: Acquire before Start.
	ErrNotStarted = errors.New("researchhub: not started")
)

// Config configures a Hub.
type Config struct {
	// Broker is the broker selection read at start-up (its Tachibana part
	// carries the environment, base URL, 秘密鍵 path and request rate).
	Broker config.BrokerSettings
	// Secrets supply the selected environment's 認証ID.
	Secrets config.Secrets
	// Research reads the research settings (the EVENT budget and REST
	// interval of the feed are taken from them when the adapter is built).
	Research func(ctx context.Context) tachibanasource.TachibanaSourceSettings
	// Base carries test overrides (HTTPClient, Clock); other fields are ignored.
	Base adapter.Config
}

// Hub shares one 立花 adapter between the research collections.
type Hub struct {
	cfg Config

	mu        sync.Mutex
	ctx       context.Context
	wg        *sync.WaitGroup
	adapter   *adapter.Adapter
	feedStart bool
}

// New returns a Hub; nothing is created or contacted until Acquire.
func New(cfg Config) *Hub { return &Hub{cfg: cfg} }

// Start gives the hub the process context and the wait group the goroutines it
// launches join.
func (h *Hub) Start(ctx context.Context, wg *sync.WaitGroup) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.ctx, h.wg = ctx, wg
}

// Configured reports whether the credentials needed to log in are set and the
// hub may run at all; it returns ErrBrokerIsTachibana or ErrNotConfigured
// otherwise. It never touches the network.
func (h *Hub) Configured() error {
	if h.cfg.Broker.Provider == config.BrokerTachibana {
		return ErrBrokerIsTachibana
	}
	t := h.cfg.Broker.Tachibana
	if h.cfg.Secrets.TachibanaCredentials(t.Environment).AuthID == "" || t.PrivateKeyPath() == "" {
		return ErrNotConfigured
	}
	return nil
}

// Acquire returns the shared adapter, creating it and logging in on the first
// call. A failed first login is not an error here: the session keeps retrying
// and the adapter's requests fail with broker.ErrNoSession meanwhile.
func (h *Hub) Acquire(ctx context.Context) (*adapter.Adapter, error) {
	if err := h.Configured(); err != nil {
		return nil, err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.ctx == nil {
		return nil, ErrNotStarted
	}
	if h.adapter != nil {
		return h.adapter, nil
	}
	research := tachibanasource.BuildTachibanaSource(nil).Research
	if h.cfg.Research != nil {
		research = h.cfg.Research(ctx).Research
	}
	cfg := h.cfg.Base
	cfg.Settings = h.cfg.Broker.Tachibana
	cfg.Credentials = h.cfg.Secrets.TachibanaCredentials(cfg.Settings.Environment)
	cfg.Source = tachibanasource.TachibanaSourceSettings{
		EventMaxConnectsPerDay: research.QuotesMaxConnects,
		RestQuote:              tachibanasource.TachibanaRestQuoteSettings{MinIntervalSeconds: research.QuotesRestMinInterval, RequestsPerRound: tachibanasource.DefaultTachibanaRestQuoteRequests},
	}
	cfg.Notifier = nil // research failures raise no operator notice
	a := adapter.New(cfg)
	_ = a.Start(h.ctx) // a failed first login keeps retrying in the background
	h.adapter = a
	h.wg.Add(1)
	go func() { // the session logs out after the process context ends
		defer h.wg.Done()
		<-h.ctx.Done()
		a.Session().Wait()
	}()
	return a, nil
}

// StartFeed launches the adapter's EVENT feed once (it connects only when it
// has a watch list and a session). The caller sets the watch list on the
// returned adapter.
func (h *Hub) StartFeed(ctx context.Context) (*adapter.Adapter, error) {
	a, err := h.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.feedStart {
		h.feedStart = true
		h.wg.Add(1)
		go func() { defer h.wg.Done(); a.Feed().Run(h.ctx) }()
	}
	return a, nil
}
