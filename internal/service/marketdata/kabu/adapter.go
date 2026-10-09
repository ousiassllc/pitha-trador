package kabu

import (
	"context"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/broker"
	"github.com/ousiassllc/pitha-trador/internal/service/marketdata"
	"github.com/ousiassllc/pitha-trador/internal/service/marketdata/infolimit"
	"github.com/ousiassllc/pitha-trador/internal/service/marketdata/kabu/pushfeed"
	"github.com/ousiassllc/pitha-trador/internal/service/marketdata/kabu/quote"
)

const (
	// Name is the adapter name reported in broker.Capabilities.
	Name = "kabu"
	// MaxStreamSymbols is the most symbols the ranking watch list registers
	// for PUSH. kabuステーション caps the API登録銘柄リスト at 50, shared with
	// REST /board and /symbol registrations; 5 stay free for those.
	MaxStreamSymbols = 45
	// Exchange is the kabuステーションAPI market code every instrument is
	// queried under: the target universe is TSE-listed equities only, so
	// instruments.Market (free text such as "TSE Prime") is not translated
	// per row.
	Exchange = marketdata.ExchangeTSE
	// TokenRefreshInterval is how often the token is reissued. The API does
	// not publish an exact token TTL (docs/architecture/overview.md §5), so
	// 20 minutes is a conservative guess: well before any plausible expiry,
	// well above the full-scan interval (60s).
	TokenRefreshInterval = 20 * time.Minute
)

// Adapter is the kabuステーションAPI implementation of broker.Broker.
type Adapter struct {
	client *marketdata.Client
	feed   *pushfeed.Feed
}

var _ broker.Broker = (*Adapter)(nil)

// New returns an Adapter over client whose PUSH feed registers universe's
// stocks (or the ranking watch list, UseWatchlist).
func New(client *marketdata.Client, universe pushfeed.Universe) *Adapter {
	return &Adapter{
		client: client,
		feed:   pushfeed.New(universe, client, marketdata.DefaultPushURL, Exchange),
	}
}

// Client is the underlying kabuステーションAPI client, for kabu-only tools
// (the GET /ranking measurement, FR-SCHED-8) that are not neutralised.
func (a *Adapter) Client() *marketdata.Client { return a.client }

// Capabilities implements broker.Broker.
func (a *Adapter) Capabilities() broker.Capabilities {
	return broker.Capabilities{
		Name:              Name,
		MaxStreamSymbols:  MaxStreamSymbols,
		Ranking:           true,
		RequestsPerSecond: infolimit.DefaultMaxPerSecond,
	}
}

// Start implements broker.Session: issue the token, then reissue it every
// TokenRefreshInterval (marketdata.Client.Start).
func (a *Adapter) Start(ctx context.Context) error {
	return a.client.Start(ctx, TokenRefreshInterval)
}

// Status implements broker.Session.
func (a *Adapter) Status() broker.SessionStatus {
	return SessionStatusOf(a.client.TokenStatus())
}

// Quote implements broker.QuoteSource: a REST board snapshot.
func (a *Adapter) Quote(ctx context.Context, symbol string) (broker.Quote, error) {
	board, err := a.client.GetBoard(ctx, symbol, Exchange)
	if err != nil {
		return broker.Quote{}, err
	}
	return quote.FromBoard(board), nil
}

// SymbolInfo implements broker.SymbolInfoSource: MarginSell (制度信用売建) is
// true exactly for 貸借銘柄.
func (a *Adapter) SymbolInfo(ctx context.Context, symbol string) (broker.SymbolInfo, error) {
	info, err := a.client.GetSymbol(ctx, symbol, Exchange)
	if err != nil {
		return broker.SymbolInfo{}, err
	}
	return broker.SymbolInfo{Lendable: info.MarginSell, UpperLimit: info.UpperLimit, LowerLimit: info.LowerLimit}, nil
}

// SetWatch implements broker.StreamFeed.
func (a *Adapter) SetWatch(ctx context.Context, symbols []string) error {
	return a.feed.SetWatch(ctx, symbols)
}

// UseWatchlist implements broker.StreamFeed.
func (a *Adapter) UseWatchlist() { a.feed.UseWatchlist() }

// Latest implements broker.StreamFeed: the fresh PUSH board, else a REST poll.
func (a *Adapter) Latest(ctx context.Context, symbol string) (broker.Quote, error) {
	return a.feed.Latest(ctx, symbol)
}

// Run implements broker.StreamFeed.
func (a *Adapter) Run(ctx context.Context) { a.feed.Run(ctx) }

// BoardFailures implements broker.Health.
func (a *Adapter) BoardFailures() *domain.FailureStreak { return a.client.BoardFailures() }

// BrokerFailures implements broker.Health.
func (a *Adapter) BrokerFailures() *domain.FailureStreak { return a.client.BrokerFailures() }
