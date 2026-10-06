package newsfeed

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/httpbody"
	"github.com/ousiassllc/pitha-trador/internal/service/assist"
)

const (
	// DefaultYanoshinBaseURL is the unofficial やのしん TDnet WebAPI
	// (https://webapi.yanoshin.jp/tdnet/). It needs no API key.
	DefaultYanoshinBaseURL = "https://webapi.yanoshin.jp"

	// yanoshinTimeout is deliberately short: the service is unofficial, so a
	// slow response must be abandoned quickly rather than hold a poll cycle
	// (FR-LUNA-4; the GuardedFeed backoff then lowers the query rate).
	yanoshinTimeout = 5 * time.Second
	// yanoshinLimit caps the disclosures requested per symbol; only the
	// newest few matter (DefaultMaxItems, DefaultTTL).
	yanoshinLimit = 10
)

// jst is the zone of the やのしん `pubdate` ("2026-10-05 15:30:00" is TSE
// time). A fixed zone avoids depending on the host's tzdata.
var jst = time.FixedZone("JST", 9*60*60)

// YanoshinConfig configures a YanoshinClient.
type YanoshinConfig struct {
	// BaseURL defaults to DefaultYanoshinBaseURL (tests point it at a fake).
	BaseURL string
	// HTTPClient defaults to &http.Client{Timeout: 5s}.
	HTTPClient *http.Client
}

// YanoshinClient is the default Feed: it reads the やのしん TDnet WebAPI's
// per-symbol disclosure index and normalizes it onto the internal
// `{id, headline, body, published_at}` contract. Only the index and the
// document link are used; neither release.tdnet.info nor the PDF is fetched.
type YanoshinClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewYanoshinClient returns a YanoshinClient configured by cfg.
func NewYanoshinClient(cfg YanoshinConfig) *YanoshinClient {
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: yanoshinTimeout}
	}
	base := strings.TrimRight(cfg.BaseURL, "/")
	if base == "" {
		base = DefaultYanoshinBaseURL
	}
	return &YanoshinClient{baseURL: base, httpClient: httpClient}
}

// yanoshinDisclosure is one disclosure. The `json` format nests it under
// `Tdnet` (observed; the docs also show `TDnet`), `json2` puts the same
// fields directly on the item.
type yanoshinDisclosure struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	DocumentURL string `json:"document_url"`
	PubDate     string `json:"pubdate"`
	CompanyCode string `json:"company_code"`
}

type yanoshinItem struct {
	yanoshinDisclosure
	Tdnet *yanoshinDisclosure `json:"Tdnet"`
	TDnet *yanoshinDisclosure `json:"TDnet"`
}

// disclosure returns the nested disclosure when present, else the flat one.
func (i yanoshinItem) disclosure() yanoshinDisclosure {
	switch {
	case i.Tdnet != nil:
		return *i.Tdnet
	case i.TDnet != nil:
		return *i.TDnet
	}
	return i.yanoshinDisclosure
}

type yanoshinResponse struct {
	Items []yanoshinItem `json:"items"`
}

// Fetch returns the newest disclosures about symbol (the request symbol is
// authoritative; the response's company_code is five digits and unused).
func (c *YanoshinClient) Fetch(ctx context.Context, symbol string) ([]assist.NewsItem, error) {
	endpoint := fmt.Sprintf("%s/webapi/tdnet/list/%s.json?limit=%d", c.baseURL, url.PathEscape(symbol), yanoshinLimit)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("newsfeed: build yanoshin request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("newsfeed: fetch yanoshin %q: %w", symbol, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := httpbody.ReadAll(resp.Body, maxFeedBytes)
	if err != nil {
		return nil, fmt.Errorf("newsfeed: read yanoshin response for %q: %w", symbol, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("newsfeed: yanoshin returned status %d for %q", resp.StatusCode, symbol)
	}
	var decoded yanoshinResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		return nil, fmt.Errorf("newsfeed: decode yanoshin response for %q: %w", symbol, err)
	}

	items := make([]assist.NewsItem, 0, len(decoded.Items))
	for _, it := range decoded.Items {
		d := it.disclosure()
		if d.Title == "" {
			continue // nothing to classify or dedupe by
		}
		published, err := time.ParseInLocation(time.DateTime, d.PubDate, jst)
		if err != nil {
			published = time.Time{} // Service treats a missing time as "now"
		}
		items = append(items, assist.NewsItem{
			ID:          d.ID,
			Symbol:      symbol,
			Headline:    d.Title,
			Body:        yanoshinBody(d),
			PublishedAt: published.UTC(),
		})
	}
	return items, nil
}

// yanoshinBody is the disclosure's body: the index carries no text, so it
// is a short note plus the document link (the PDF itself is never fetched).
func yanoshinBody(d yanoshinDisclosure) string {
	if d.DocumentURL == "" {
		return "TDnet 適時開示"
	}
	return "TDnet 適時開示: " + d.DocumentURL
}
