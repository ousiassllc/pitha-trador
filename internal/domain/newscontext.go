package domain

import "time"

// Luna sentiment values (functional.md §4.15 FR-LUNA-2).
const (
	NewsSentimentBullish = "bullish"
	NewsSentimentBearish = "bearish"
	NewsSentimentNeutral = "neutral"
)

// Luna event_type values (functional.md §4.15 FR-LUNA-2:
// 決算/業績修正/M&A/規制/その他).
const (
	NewsEventEarnings   = "決算"
	NewsEventGuidance   = "業績修正"
	NewsEventMA         = "M&A"
	NewsEventRegulation = "規制"
	NewsEventOther      = "その他"
)

// NewsContextItem is one Luna-classified news item as it appears in
// NewsContext (FR-LUNA-2/FR-LUNA-3). It carries only Luna's output and the
// publication time - never the raw headline/body.
type NewsContextItem struct {
	Sentiment   string    `json:"sentiment"`
	EventType   string    `json:"event_type"`
	Summary     string    `json:"summary"`
	PublishedAt time.Time `json:"published_at"`
}

// NewsContext is the recent-news context injected into
// jev_decisions.state_json as its `news_context` field (FR-LUNA-3): an
// instrument's most recent Luna classifications, newest first. It is
// auxiliary context for Jev Scout/Trader only - it never overrides Jev's
// judgment (FR-LUNA-5).
type NewsContext struct {
	Items []NewsContextItem `json:"items"`
}
