package newsfeed

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/assist"
)

const (
	defaultFeedTimeout = 15 * time.Second
	maxFeedBytes       = 4 << 20
)

// FeedConfig configures a FeedClient.
type FeedConfig struct {
	// URL is NEWS_FEED_URL: the external news feed endpoint. News Ingest
	// issues `GET <URL>?symbol=<symbol>` and expects
	// {"items":[{"id","headline","body","published_at"}]} back.
	URL string
	// APIKey is NEWS_FEED_API_KEY, sent as a Bearer token when non-empty.
	APIKey string
	// HTTPClient defaults to &http.Client{Timeout: 15s}.
	HTTPClient *http.Client
}

// FeedClient fetches per-symbol news from the external news feed
// (FR-LUNA-1).
type FeedClient struct {
	url        string
	apiKey     string
	httpClient *http.Client
}

// NewFeedClient returns a FeedClient configured by cfg.
func NewFeedClient(cfg FeedConfig) *FeedClient {
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultFeedTimeout}
	}
	return &FeedClient{url: cfg.URL, apiKey: cfg.APIKey, httpClient: httpClient}
}

// Configured reports whether NEWS_FEED_URL is set.
func (f *FeedClient) Configured() bool {
	return f != nil && f.url != ""
}

type feedResponse struct {
	Items []feedItem `json:"items"`
}

type feedItem struct {
	ID          string    `json:"id"`
	Headline    string    `json:"headline"`
	Body        string    `json:"body"`
	PublishedAt time.Time `json:"published_at"`
}

// Fetch returns the feed's current articles about symbol.
func (f *FeedClient) Fetch(ctx context.Context, symbol string) ([]assist.NewsItem, error) {
	u, err := url.Parse(f.url)
	if err != nil {
		return nil, fmt.Errorf("newsfeed: parse feed url: %w", err)
	}
	q := u.Query()
	q.Set("symbol", symbol)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("newsfeed: build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if f.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+f.apiKey)
	}

	resp, err := f.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("newsfeed: fetch %q: %w", symbol, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxFeedBytes))
	if err != nil {
		return nil, fmt.Errorf("newsfeed: read feed response for %q: %w", symbol, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("newsfeed: feed returned status %d for %q", resp.StatusCode, symbol)
	}

	var decoded feedResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		return nil, fmt.Errorf("newsfeed: decode feed response for %q: %w", symbol, err)
	}

	items := make([]assist.NewsItem, 0, len(decoded.Items))
	for _, it := range decoded.Items {
		items = append(items, assist.NewsItem{
			ID:          it.ID,
			Symbol:      symbol,
			Headline:    it.Headline,
			Body:        it.Body,
			PublishedAt: it.PublishedAt,
		})
	}
	return items, nil
}
