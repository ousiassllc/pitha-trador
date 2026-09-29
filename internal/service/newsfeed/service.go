// Package newsfeed implements News Ingest (docs/architecture/overview.md
// §12, functional.md §4.15 FR-LUNA-1〜5): it periodically fetches news
// for every active instrument from the external news feed, classifies each
// new item with Luna, and keeps the results in an in-memory, TTL-bounded
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
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/assist"
)

const (
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

// InstrumentSource lists the instruments News Ingest polls
// (repository.InstrumentRepository).
type InstrumentSource interface {
	ListActive(ctx context.Context) ([]domain.Instrument, error)
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

// WithNow overrides time.Now (tests).
func WithNow(now func() time.Time) Option {
	return func(s *Service) { s.now = now }
}

// Service is News Ingest plus its in-memory news cache.
type Service struct {
	feed        Feed
	classifier  Classifier
	instruments InstrumentSource
	maxItems    int
	ttl         time.Duration
	now         func() time.Time

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

// NewService returns a Service that polls feed for instruments' active
// symbols and classifies with classifier.
func NewService(feed Feed, classifier Classifier, instruments InstrumentSource, opts ...Option) *Service {
	s := &Service{
		feed:        feed,
		classifier:  classifier,
		instruments: instruments,
		maxItems:    DefaultMaxItems,
		ttl:         DefaultTTL,
		now:         func() time.Time { return time.Now().UTC() },
		cache:       make(map[string]*symbolNews),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Poll runs one News Ingest cycle (FR-LUNA-1/FR-LUNA-2). It returns an
// error only when the active instrument list cannot be read; per-symbol
// feed failures and per-item Luna failures are logged and skipped
// (FR-LUNA-4).
func (s *Service) Poll(ctx context.Context) error {
	instruments, err := s.instruments.ListActive(ctx)
	if err != nil {
		return err
	}
	for _, in := range instruments {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		s.pollSymbol(ctx, in.Symbol)
	}
	return nil
}

func (s *Service) pollSymbol(ctx context.Context, symbol string) {
	items, err := s.feed.Fetch(ctx, symbol)
	if err != nil {
		slog.Error("newsfeed: fetch failed, no news flag for this cycle", "symbol", symbol, "error", err)
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
			return
		}
		s.store(symbol, item, class)
	}
}

func itemKey(item assist.NewsItem) string {
	if item.ID != "" {
		return item.ID
	}
	return item.Headline
}

func (s *Service) alreadySeen(symbol string, item assist.NewsItem) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.cache[symbol]
	if !ok {
		return false
	}
	_, seen := entry.seen[itemKey(item)]
	return seen
}

func (s *Service) store(symbol string, item assist.NewsItem, class assist.Classification) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, ok := s.cache[symbol]
	if !ok {
		entry = &symbolNews{seen: make(map[string]time.Time)}
		s.cache[symbol] = entry
	}
	s.prune(entry)

	entry.seen[itemKey(item)] = item.PublishedAt
	entry.items = append(entry.items, domain.NewsContextItem{
		Sentiment:   class.Sentiment,
		EventType:   class.EventType,
		Summary:     class.Summary,
		PublishedAt: item.PublishedAt,
	})
	sort.SliceStable(entry.items, func(i, j int) bool { return entry.items[i].PublishedAt.After(entry.items[j].PublishedAt) })
	if len(entry.items) > s.maxItems {
		entry.items = entry.items[:s.maxItems]
	}
	entry.flagged = true
}

// prune drops items and seen markers older than the TTL. The caller holds
// s.mu.
func (s *Service) prune(entry *symbolNews) {
	cutoff := s.now().Add(-s.ttl)
	kept := entry.items[:0]
	for _, it := range entry.items {
		if it.PublishedAt.After(cutoff) {
			kept = append(kept, it)
		}
	}
	entry.items = kept
	for key, published := range entry.seen {
		if !published.After(cutoff) {
			delete(entry.seen, key)
		}
	}
	if len(entry.items) == 0 {
		entry.flagged = false
	}
}

// NewsContext returns symbol's cached, unexpired Luna classifications,
// newest first (FR-LUNA-3). ok is false when there are none, in which case
// callers must leave state_json's news_context unset.
func (s *Service) NewsContext(symbol string) (domain.NewsContext, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.cache[symbol]
	if !ok {
		return domain.NewsContext{}, false
	}
	s.prune(entry)
	if len(entry.items) == 0 {
		return domain.NewsContext{}, false
	}
	return domain.NewsContext{Items: append([]domain.NewsContextItem(nil), entry.items...)}, true
}

// TakeNewsFlag reports whether symbol has had news classified since the
// last call, clearing the flag (FR-SCAN-1's "ニュースフラグ発生"): each
// new article triggers at most one event-driven re-evaluation rather than
// re-triggering every cycle until it expires.
func (s *Service) TakeNewsFlag(symbol string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.cache[symbol]
	if !ok {
		return false
	}
	s.prune(entry)
	flagged := entry.flagged
	entry.flagged = false
	return flagged
}
