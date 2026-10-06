// Package pushfeed wires kabuステーションAPI's PUSH subscription
// (flows.md §10.1 起動時フロー, integrations.md §5): it registers the
// scan universe, keeps the latest PUSH board per symbol, and serves
// market-data jobs a usable board - the fresh PUSH one when present,
// otherwise a REST poll.
package pushfeed

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/safego"
	"github.com/ousiassllc/pitha-trador/internal/service/marketdata"
)

const (
	// MaxRegisterSymbols is how many symbols are registered for PUSH. kabu
	// station caps the API登録銘柄リスト at 50 across PUSH and REST, and every
	// REST /board or /symbol request registers its symbol too, so
	// marketdata.RestRotationSlots stay free for the REST poll to rotate
	// through (marketdata.Client.withRestSlot).
	MaxRegisterSymbols = marketdata.MaxRegisteredSymbols - marketdata.RestRotationSlots
	// BoardMaxAge is how old a PUSH board may be to be preferred over a
	// REST poll.
	BoardMaxAge = 30 * time.Second

	retryMin = 2 * time.Second
	retryMax = time.Minute
)

// Universe lists the instruments to subscribe.
type Universe interface {
	ListActiveByKind(ctx context.Context, kind string) ([]domain.Instrument, error)
}

// Broker is the kabuステーションAPI REST surface Feed uses
// (*marketdata.Client).
type Broker interface {
	RegisterSymbols(ctx context.Context, symbols []marketdata.RegisterSymbol) (marketdata.RegisterSuccess, error)
	UnregisterAll(ctx context.Context) error
	GetBoard(ctx context.Context, symbol string, exchange int) (marketdata.Board, error)
	Status() *marketdata.StatusTracker
}

// Feed owns the PUSH subscription and the latest-board cache.
type Feed struct {
	Universe Universe
	Broker   Broker
	// URL is the PUSH WebSocket endpoint (marketdata.DefaultPushURL).
	URL string
	// Exchange is the kabu exchange code registered and polled.
	Exchange int

	boards *marketdata.BoardCache
}

// New returns a Feed subscribing to url.
func New(universe Universe, broker Broker, url string, exchange int) *Feed {
	return &Feed{Universe: universe, Broker: broker, URL: url, Exchange: exchange, boards: marketdata.NewBoardCache()}
}

// RegisterUniverse registers the active stocks (capped at
// MaxRegisterSymbols) so their updates arrive over PUSH. Symbols beyond
// the cap are served by the REST poll only, which rotates through the
// remaining registration slots. The registration list is emptied first:
// kabu station keeps registrations across app restarts, and stale ones
// would leave no slot for the REST poll (4002006).
func (f *Feed) RegisterUniverse(ctx context.Context) error {
	stocks, err := f.Universe.ListActiveByKind(ctx, domain.InstrumentKindStock)
	if err != nil {
		return fmt.Errorf("pushfeed: list universe: %w", err)
	}
	if len(stocks) == 0 {
		return nil
	}
	if len(stocks) > MaxRegisterSymbols {
		slog.Warn("pushfeed: universe exceeds kabu PUSH registration cap, extra symbols use REST polling only",
			"universe", len(stocks), "cap", MaxRegisterSymbols)
		stocks = stocks[:MaxRegisterSymbols]
	}
	symbols := make([]marketdata.RegisterSymbol, len(stocks))
	for i, inst := range stocks {
		symbols[i] = marketdata.RegisterSymbol{Symbol: inst.Symbol, Exchange: f.Exchange}
	}
	if err := f.Broker.UnregisterAll(ctx); err != nil {
		return fmt.Errorf("pushfeed: unregister all symbols: %w", err)
	}
	if _, err := f.Broker.RegisterSymbols(ctx, symbols); err != nil {
		return fmt.Errorf("pushfeed: register symbols: %w", err)
	}
	return nil
}

// Run registers the universe and consumes the PUSH WebSocket into the
// board cache until ctx is done, re-registering and reconnecting with
// exponential backoff after any failure (no token yet, kabuステーション
// down, dropped connection). Failures never affect the REST path: Latest
// falls back to GetBoard whenever no fresh PUSH board exists.
func (f *Feed) Run(ctx context.Context) {
	push := marketdata.NewPushClient(f.URL, f.Broker.Status())
	retry := retryMin
	for ctx.Err() == nil {
		// A panic (register, PUSH read, board callback) is logged and
		// handled like any failed attempt: back off, then re-subscribe.
		err := safego.Try("pushfeed subscription", func() error {
			if err := f.RegisterUniverse(ctx); err != nil {
				return err
			}
			connectedAt := time.Now()
			err := push.Run(ctx, func(b marketdata.Board) { f.boards.Put(b, time.Now()) })
			if time.Since(connectedAt) >= retryMax { // was stable: restart the backoff
				retry = retryMin
			}
			return err
		})
		if ctx.Err() != nil {
			return
		}
		if !errors.Is(err, marketdata.ErrNoToken) {
			slog.Warn("pushfeed: PUSH subscription ended, retrying", "error", err, "retry_in", retry)
		}
		timer := time.NewTimer(retry)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		retry = min(retry*2, retryMax)
	}
}

// Latest returns symbol's board: a PUSH board received within
// BoardMaxAge, otherwise a REST poll. A board without a usable current
// price (0 before 寄り付き/未約定; Board.HasPrice) is never returned:
// callers get marketdata.ErrPriceUnavailable instead, so a price-0 bar is
// never persisted (issue #173).
func (f *Feed) Latest(ctx context.Context, symbol string) (marketdata.Board, error) {
	board, ok := f.boards.Fresh(symbol, time.Now(), BoardMaxAge)
	if !ok {
		var err error
		if board, err = f.Broker.GetBoard(ctx, symbol, f.Exchange); err != nil {
			return marketdata.Board{}, err
		}
	}
	if !board.HasPrice() {
		return marketdata.Board{}, fmt.Errorf("%w: %s", marketdata.ErrPriceUnavailable, symbol)
	}
	return board, nil
}
