// Package newsfeed implements News Ingest (docs/architecture/overview.md
// §13, functional.md §4.16 FR-LUNA-1〜5): it periodically fetches news
// for the symbols that can actually use it (the current Fast Screener
// candidates and held positions, see SymbolSource; only during the TSE
// session) from the external news feed, classifies each new item with Luna, and keeps the results in an in-memory, TTL-bounded
// per-symbol cache. Nothing is persisted: the cache is read by Jev
// Scout/Trader to inject `news_context` into jev_decisions.state_json
// (FR-LUNA-3) and by the event-driven re-evaluation trigger as FR-SCAN-1's
// "ニュースフラグ" (TakeNewsFlag).
//
// Every failure - feed unreachable, Luna down, malformed response - only
// skips the affected symbol/item and is logged. It never raises a news
// flag and never blocks the Fast Screener/Jev flow (FR-LUNA-4).
package newsfeed

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/assist"
)

const (
	// DefaultConcurrency bounds the simultaneous feed/Luna requests of one
	// Poll cycle, so a cycle's duration grows with symbols/DefaultConcurrency
	// rather than linearly with the symbol count.
	DefaultConcurrency = 4

	// DefaultMaxItems is FR-LUNA-3's "直近N件": how many classified items
	// the cache keeps per symbol.
	DefaultMaxItems = 5
	// DefaultTTL is how long a classified item stays in the cache
	// (measured from its publication time).
	DefaultTTL = 6 * time.Hour
)

// Feed fetches the current news articles about one symbol.
type Feed interface {
	Fetch(ctx context.Context, symbol string) ([]assist.NewsItem, error)
}

// Classifier classifies one news item (assist.Luna).
type Classifier interface {
	Classify(ctx context.Context, item assist.NewsItem) (assist.Classification, error)
}

// SymbolSource lists the symbols News Ingest polls this cycle: the symbols
// whose news is read at all (Jev Scout/Trader news_context and the
// event-driven re-evaluation news flag only ever look at Fast Screener
// candidates and held positions), not the whole ~4,000-symbol universe.
type SymbolSource interface {
	NewsSymbols(ctx context.Context) ([]string, error)
}

// Option configures a Service.
type Option func(*Service)

// WithMaxItems overrides DefaultMaxItems.
func WithMaxItems(n int) Option {
	return func(s *Service) {
		if n > 0 {
			s.maxItems = n
		}
	}
}

// WithTTL overrides DefaultTTL.
func WithTTL(ttl time.Duration) Option {
	return func(s *Service) {
		if ttl > 0 {
			s.ttl = ttl
		}
	}
}

// WithConcurrency overrides DefaultConcurrency: how many symbols' feed
// fetches (and Luna classifications) run at once.
func WithConcurrency(n int) Option {
	return func(s *Service) {
		if n > 0 {
			s.concurrency = n
		}
	}
}

// WithSessionGate makes Poll a no-op whenever open(now) is false (outside
// the 東証立会時間), so nights and weekends send no feed requests. The
// default polls at any time.
func WithSessionGate(open func(time.Time) bool) Option {
	return func(s *Service) { s.inSession = open }
}

// ErrorObserver is told about every News Ingest failure (a feed fetch or a
// Luna classification) after it has been logged; the Activity Feed uses it
// to show them (issue #273). It must not block.
type ErrorObserver func(symbol string, err error)

// WithErrorObserver registers an ErrorObserver.
func WithErrorObserver(observe ErrorObserver) Option {
	return func(s *Service) { s.observeError = observe }
}

// WithNow overrides time.Now (tests).
func WithNow(now func() time.Time) Option {
	return func(s *Service) { s.now = now }
}

// Service is News Ingest plus its in-memory news cache.
type Service struct {
	feed        Feed
	classifier  Classifier
	symbols     SymbolSource
	maxItems    int
	ttl         time.Duration
	concurrency int
	inSession   func(time.Time) bool
	now         func() time.Time

	observeError ErrorObserver

	mu    sync.Mutex
	cache map[string]*symbolNews
}

// symbolNews is one symbol's cached state. seen remembers every article
// already classified (until it ages out) so a feed that keeps returning
// the same articles does not trigger repeated Luna calls or news flags.
type symbolNews struct {
	items   []domain.NewsContextItem
	seen    map[string]time.Time
	flagged bool
}

// NewService returns a Service that polls feed for the symbols the
// SymbolSource lists and classifies with classifier.
func NewService(feed Feed, classifier Classifier, symbols SymbolSource, opts ...Option) *Service {
	s := &Service{
		feed:        feed,
		classifier:  classifier,
		symbols:     symbols,
		maxItems:    DefaultMaxItems,
		ttl:         DefaultTTL,
		concurrency: DefaultConcurrency,
		inSession:   func(time.Time) bool { return true },
		now:         func() time.Time { return time.Now().UTC() },
		cache:       make(map[string]*symbolNews),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Poll runs one News Ingest cycle (FR-LUNA-1/FR-LUNA-2) over the symbols of
// the SymbolSource, at most concurrency at a time, and does nothing outside
// the session gate. It returns an error only when the symbol list cannot be
// read; per-symbol feed failures and per-item Luna failures are logged and
// skipped (FR-LUNA-4).
func (s *Service) Poll(ctx context.Context) error {
	if !s.inSession(s.now()) {
		return nil
	}
	symbols, err := s.symbols.NewsSymbols(ctx)
	if err != nil {
		return err
	}
	var wg sync.WaitGroup
	sem := make(chan struct{}, s.concurrency)
	for _, symbol := range symbols {
		if ctx.Err() != nil {
			break
		}
		sem <- struct{}{}
		wg.Add(1)
		go func() {
			defer func() { <-sem; wg.Done() }()
			s.pollSymbol(ctx, symbol)
		}()
	}
	wg.Wait()
	return ctx.Err()
}

func (s *Service) pollSymbol(ctx context.Context, symbol string) {
	items, err := s.feed.Fetch(ctx, symbol)
	if errors.Is(err, ErrBackingOff) {
		slog.Debug("newsfeed: feed is backing off, skipping fetch", "symbol", symbol)
		return
	}
	if err != nil {
		slog.Error("newsfeed: fetch failed, no news flag for this cycle", "symbol", symbol, "error", err)
		s.reportError(symbol, err)
		return
	}
	for _, item := range items {
		now := s.now()
		if item.PublishedAt.IsZero() {
			// A feed that omits published_at: treat the article as new
			// (itemKey then dedupes it by id/headline, not by time).
			item.PublishedAt = now
		}
		if now.Sub(item.PublishedAt) >= s.ttl || s.alreadySeen(symbol, item) {
			continue
		}
		class, err := s.classifier.Classify(ctx, item)
		if err != nil {
			// Luna is failing (or misbehaving): stop for this symbol
			// rather than hammering it once per remaining article. The
			// unclassified articles are retried next cycle.
			slog.Error("newsfeed: luna classification failed, no news flag", "symbol", symbol, "error", err)
			s.reportError(symbol, err)
			return
		}
		s.store(symbol, item, class)
	}
}

func (s *Service) reportError(symbol string, err error) {
	if s.observeError != nil {
		s.observeError(symbol, err)
	}
}
