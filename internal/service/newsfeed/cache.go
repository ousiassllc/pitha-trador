package newsfeed

import (
	"sort"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/assist"
)

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
