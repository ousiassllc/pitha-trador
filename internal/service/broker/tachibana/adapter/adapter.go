// Package adapter is the 立花証券 e支店・API (v4r10) behind broker.Broker
// (docs/architecture/overview/integrations.md §5.3, issue #724). It composes
// the REQUEST I/F client (tachibana) and the login manager (tachibana/session).
package adapter

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"path"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/config/tachibanasource"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/broker"
	"github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana"
	"github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana/event"
	"github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana/market"
	"github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana/session"
)

// MaxStreamSymbols is the EVENT I/F's subscription cap (and the 時価 request
// cap): 120 symbols.
const MaxStreamSymbols = 120

// errNoRanking is returned by Candidates: the broker has no ranking API.
var errNoRanking = errors.New("tachibana: the broker has no ranking API")

// Config configures an Adapter.
type Config struct {
	// Settings are the effective 立花 settings (Settings screen).
	Settings config.TachibanaSettings
	// Credentials are the selected environment's secrets. Only AuthID is used:
	// the adapter reads market data and never sends a 第二暗証番号.
	Credentials config.TachibanaCredentials
	// Source carries the 立花 監視銘柄ソース settings the stream uses: the daily
	// EVENT connection budget and the REST 時価 assist limits (issue #728).
	Source tachibanasource.TachibanaSourceSettings
	// Notifier receives operator notices; may be nil.
	Notifier session.Notifier
	// HTTPClient and Clock override the defaults (tests).
	HTTPClient *http.Client
	Clock      tachibana.Clock
}

// Adapter is the 立花証券 e支店・API behind broker.Broker.
type Adapter struct {
	client  *tachibana.Client
	session *session.Session
	master  *market.Master
	feed    *event.Feed
}

var _ broker.Broker = (*Adapter)(nil)

// New builds the adapter; nothing is started (Start does).
func New(cfg Config) *Adapter {
	client := tachibana.NewClient(tachibana.Config{
		BaseURL:           cfg.Settings.BaseURL(),
		RequestsPerSecond: cfg.Settings.RequestMaxPerSecond,
		HTTPClient:        cfg.HTTPClient,
		Clock:             cfg.Clock,
	})
	master := market.NewMaster(client, cfg.Clock)
	sess := session.New(session.Config{
		Client:     client,
		AuthID:     cfg.Credentials.AuthID,
		KeyPath:    cfg.Settings.PrivateKeyPath(),
		ReauthTime: cfg.Settings.ReauthTime,
		APIVersion: path.Base(urlPath(cfg.Settings.BaseURL())),
		Clock:      cfg.Clock,
		Notifier:   cfg.Notifier,
		OnLogin:    master.LoadWithRetry,
	})
	feed := event.New(event.Config{
		Client:           client,
		ConnectionBudget: cfg.Source.EventMaxConnectsPerDay,
		RefillInterval:   time.Duration(cfg.Source.RestQuote.MinIntervalSeconds) * time.Second,
		RefillRequests:   cfg.Source.RestQuote.RequestsPerRound,
		HTTPClient:       cfg.HTTPClient,
		Clock:            cfg.Clock,
	})
	return &Adapter{client: client, session: sess, master: master, feed: feed}
}

// Client is the REQUEST I/F client the adapter's data paths use.
func (a *Adapter) Client() *tachibana.Client { return a.client }

// Session is the login manager.
func (a *Adapter) Session() *session.Session { return a.session }

// Capabilities implements broker.Broker.
func (a *Adapter) Capabilities() broker.Capabilities {
	return broker.Capabilities{
		Name:              config.BrokerTachibana,
		MaxStreamSymbols:  MaxStreamSymbols,
		Ranking:           false,
		RequestsPerSecond: a.client.RequestsPerSecond(),
	}
}

// Start implements broker.Session: log in (a failed first attempt returns the
// sanitized error) and keep the session alive until ctx is done.
func (a *Adapter) Start(ctx context.Context) error { return a.session.Start(ctx) }

// Status implements broker.Session.
func (a *Adapter) Status() broker.SessionStatus { return a.session.Status() }

// Run implements broker.StreamFeed: it keeps the EVENT connection until ctx
// is done and returns once that is closed and the session has logged out, so
// the composition root's wait group covers both.
func (a *Adapter) Run(ctx context.Context) {
	a.feed.Run(ctx)
	a.session.Wait()
}

// SetWatch implements broker.StreamFeed: the symbols to receive over EVENT
// (at most 120; changes cost a reconnect, see event.Feed.SetWatch).
func (a *Adapter) SetWatch(ctx context.Context, symbols []string) error {
	return a.feed.SetWatch(ctx, symbols)
}

// UseWatchlist implements broker.StreamFeed (a no-op, see event.Feed).
func (a *Adapter) UseWatchlist() { a.feed.UseWatchlist() }

// Latest implements broker.StreamFeed: the EVENT value of the last 30
// seconds, else a rate-limited REST refill (see event.Feed.Latest).
func (a *Adapter) Latest(ctx context.Context, symbol string) (broker.Quote, error) {
	return a.feed.Latest(ctx, symbol)
}

// Feed is the EVENT receiver (recorded EC notifications, SS/US statuses,
// connection tally).
func (a *Adapter) Feed() *event.Feed { return a.feed }

// Candidates implements broker.CandidateSource: 立花 has no ranking
// (Capabilities.Ranking is false).
func (a *Adapter) Candidates(context.Context) ([]string, error) { return nil, errNoRanking }

// BoardFailures implements broker.Health (market_data_down).
func (a *Adapter) BoardFailures() *domain.FailureStreak { return a.client.BoardFailures() }

// BrokerFailures implements broker.Health (broker_api_error).
func (a *Adapter) BrokerFailures() *domain.FailureStreak { return a.client.BrokerFailures() }

// urlPath is the path of raw ("" when malformed).
func urlPath(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Path
}
