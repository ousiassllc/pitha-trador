package newsfeed

import (
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// TDnetScoreNeutral is the score of a symbol with no disclosure in the TTL
// window: 0 means "no information", which is the same value the metalabel
// model sees for a bullish and a bearish disclosure that cancel out. It is
// deliberately not "not available": a symbol that simply has no news is the
// common case and must still be scorable (metalabel.Features.TDnetScore).
const TDnetScoreNeutral = 0.0

// sentimentSign maps a Luna sentiment to its direction. An unknown or empty
// sentiment contributes nothing, like neutral.
func sentimentSign(sentiment string) float64 {
	switch sentiment {
	case domain.NewsSentimentBullish:
		return 1
	case domain.NewsSentimentBearish:
		return -1
	default:
		return 0
	}
}

// ScoreNewsContext is the TDnet disclosure score of a symbol's Luna
// classifications as of at, in [-1, 1] (positive favours LONG, negative
// SHORT; it is a symbol-level feature, not signed towards a candidate side).
//
// It is the sum over the items published in (at-ttl, at] of sentiment (+1
// bullish, -1 bearish, 0 neutral) times a linear freshness weight (1 when just
// published, 0 at ttl), clamped to [-1, 1]: a fresh disclosure counts fully,
// an old one fades, and several agreeing disclosures reinforce each other.
// Items published after at are ignored, so a score for a past instant never
// sees the future. No such item scores TDnetScoreNeutral (0).
//
// It is a pure function so the training side can recompute the score a past
// decision saw from the news_context persisted in jev_decisions.state_json
// (FR-LUNA-3) with the very same rule the live path uses.
func ScoreNewsContext(items []domain.NewsContextItem, at time.Time, ttl time.Duration) float64 {
	var sum float64
	for _, it := range items {
		age := at.Sub(it.PublishedAt)
		if age < 0 || age >= ttl {
			continue
		}
		sum += sentimentSign(it.Sentiment) * (1 - float64(age)/float64(ttl))
	}
	return max(-1, min(1, sum))
}

// TDnetScore returns the TDnet disclosure score of symbol as of at
// (ScoreNewsContext over the symbol's cached disclosures, DefaultTTL window
// unless WithTTL). The score reads only the in-memory cache that Poll fills
// (the newest DefaultMaxItems disclosures per symbol, kept for the TTL), so
// at should be the current time or close to it. A symbol with no cached
// disclosure scores TDnetScoreNeutral.
func (s *Service) TDnetScore(symbol string, at time.Time) float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.cache[symbol]
	if !ok {
		return TDnetScoreNeutral
	}
	s.prune(entry)
	return ScoreNewsContext(entry.items, at, s.ttl)
}
