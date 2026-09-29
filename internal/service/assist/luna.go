package assist

import (
	"context"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// LunaClassifyPath is the Luna API endpoint a news item is POSTed to.
const LunaClassifyPath = "/v1/classify"

// NewsItem is one news article News Ingest (internal/service/newsfeed)
// fetched for Symbol.
type NewsItem struct {
	ID          string    `json:"id"`
	Symbol      string    `json:"symbol"`
	Headline    string    `json:"headline"`
	Body        string    `json:"body"`
	PublishedAt time.Time `json:"published_at"`
}

// LunaRequest is the JSON body POSTed to the Luna API for one news item
// (FR-LUNA-2: one API call per news item).
type LunaRequest struct {
	Symbol      string    `json:"symbol"`
	Headline    string    `json:"headline"`
	Body        string    `json:"body"`
	PublishedAt time.Time `json:"published_at"`
}

// Classification is Luna's answer for one news item (FR-LUNA-2).
type Classification struct {
	Sentiment string `json:"sentiment"`
	EventType string `json:"event_type"`
	Summary   string `json:"summary"`
}

// Luna is the Sense adapter: it classifies a news item into
// sentiment/event_type/summary by calling the external Luna AI API
// (FR-LUNA-2, overview.md §13). Its output is auxiliary context for Jev
// only (FR-LUNA-5).
type Luna struct {
	client *Client
}

// NewLuna returns a Luna adapter that calls client.
func NewLuna(client *Client) *Luna {
	return &Luna{client: client}
}

// Classify sends item to the Luna API and returns its validated
// classification. A response with an unknown sentiment/event_type or an
// empty summary is an error, not a best-effort guess: FR-LUNA-4 treats any
// Luna failure as "no news flag".
func (l *Luna) Classify(ctx context.Context, item NewsItem) (Classification, error) {
	var out Classification
	req := LunaRequest{Symbol: item.Symbol, Headline: item.Headline, Body: item.Body, PublishedAt: item.PublishedAt}
	if err := l.client.PostJSON(ctx, LunaClassifyPath, req, &out); err != nil {
		return Classification{}, fmt.Errorf("assist: luna classify %q: %w", item.Symbol, err)
	}
	if err := out.validate(); err != nil {
		return Classification{}, fmt.Errorf("assist: luna classify %q: %w", item.Symbol, err)
	}
	return out, nil
}

func (c Classification) validate() error {
	switch c.Sentiment {
	case domain.NewsSentimentBullish, domain.NewsSentimentBearish, domain.NewsSentimentNeutral:
	default:
		return fmt.Errorf("invalid sentiment %q", c.Sentiment)
	}
	switch c.EventType {
	case domain.NewsEventEarnings, domain.NewsEventGuidance, domain.NewsEventMA, domain.NewsEventRegulation, domain.NewsEventOther:
	default:
		return fmt.Errorf("invalid event_type %q", c.EventType)
	}
	if c.Summary == "" {
		return fmt.Errorf("empty summary")
	}
	return nil
}
