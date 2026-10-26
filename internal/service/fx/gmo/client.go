package gmo

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
)

// Client reads rates from GMO's public FX API. It is safe for concurrent use;
// all callers share one call-rate limit.
type Client struct {
	cfg     Config
	http    *http.Client
	clock   Clock
	logger  *slog.Logger
	limiter *limiter
}

// Option customises a Client (mainly for tests).
type Option func(*Client)

// WithHTTPClient replaces the default HTTP client (10s timeout).
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.http = h } }

// WithClock replaces the wall clock.
func WithClock(clock Clock) Option { return func(c *Client) { c.clock = clock } }

// WithLogger replaces slog.Default().
func WithLogger(l *slog.Logger) Option { return func(c *Client) { c.logger = l } }

// New validates cfg and builds a Client.
func New(cfg Config, opts ...Option) (*Client, error) {
	cfg = cfg.withDefaults()
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	cfg.Symbols = slices.Clone(cfg.Symbols)
	c := &Client{cfg: cfg, http: &http.Client{Timeout: defaultHTTPTimeout}, clock: realClock{}, logger: slog.Default()}
	for _, o := range opts {
		o(c)
	}
	c.limiter = newLimiter(c.clock, cfg.MinInterval)
	return c, nil
}

// Symbols returns the configured pairs.
func (c *Client) Symbols() []string { return slices.Clone(c.cfg.Symbols) }

// Ticker returns symbol's current quote. symbol must be configured.
func (c *Client) Ticker(ctx context.Context, symbol string) (Ticker, error) {
	if !slices.Contains(c.cfg.Symbols, symbol) {
		return Ticker{}, fmt.Errorf("%w: %q", ErrUnsupportedSymbol, symbol)
	}
	all, err := c.fetchTickers(ctx)
	if err != nil {
		return Ticker{}, err
	}
	return all[symbol], c.missing(all, symbol)
}

// Tickers returns the current quote of every configured pair, in
// Config.Symbols order, from a single API call.
func (c *Client) Tickers(ctx context.Context) ([]Ticker, error) {
	all, err := c.fetchTickers(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Ticker, 0, len(c.cfg.Symbols))
	for _, s := range c.cfg.Symbols {
		if err := c.missing(all, s); err != nil {
			return nil, err
		}
		out = append(out, all[s])
	}
	return out, nil
}

func (c *Client) missing(all map[string]Ticker, symbol string) error {
	if _, ok := all[symbol]; !ok {
		return fmt.Errorf("%w: ticker has no %s", ErrInvalidResponse, symbol)
	}
	return nil
}

// Klines returns the candles of one pair, price type, interval and period
// (see KlinesRequest.At), oldest first as GMO returns them. An empty result
// is valid (e.g. a trading day without quotes).
func (c *Client) Klines(ctx context.Context, req KlinesRequest) ([]Candle, error) {
	if !slices.Contains(c.cfg.Symbols, req.Symbol) {
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedSymbol, req.Symbol)
	}
	if req.Interval != Interval1Min && req.Interval != Interval1Day {
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedInterval, req.Interval)
	}
	if req.PriceType != PriceBid && req.PriceType != PriceAsk {
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedPriceType, req.PriceType)
	}
	date, err := req.dateParam()
	if err != nil {
		return nil, err
	}
	q := url.Values{}
	q.Set("symbol", req.Symbol)
	q.Set("priceType", string(req.PriceType))
	q.Set("interval", string(req.Interval))
	q.Set("date", date)
	data, err := c.get(ctx, "/v1/klines", q)
	if err != nil {
		return nil, err
	}
	return decodeKlines(data)
}

func (c *Client) fetchTickers(ctx context.Context) (map[string]Ticker, error) {
	data, err := c.get(ctx, "/v1/ticker", nil)
	if err != nil {
		return nil, err
	}
	return decodeTickers(data)
}
