// Package jquants is the J-Quants API V2 read adapter (issue #1093): the listed
// issues, daily bars and minute bars, fetched only when the operator has switched
// it on, stored a key, and said which endpoints are subscribed.
//
// It is OFF by default and inert: with no API key, or with the switch off, or for
// an endpoint the plan flags do not list, it sends no HTTP request at all
// (client_test.go and backfill_test.go count the requests of the transport). It
// never calls a kabu endpoint and is not part of the live market-data path. What
// it fetches goes only to the research_jquants_* tables; it is private-use data
// (J-Quants' terms) that is never sent elsewhere and is deleted on cancellation
// (Purger). The API key is held only in the Client, is sent only in the
// x-api-key header, and never appears in a log, an error or a stored row.
package jquants

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// DefaultBaseURL is the J-Quants API V2 endpoint.
const DefaultBaseURL = "https://api.jquants.com/v2"

// DefaultMinInterval is the least time between two requests: J-Quants' Light
// plan allows 60 requests a minute, and a 429 is counted as a request, so the
// client stays well below it.
const DefaultMinInterval = 1500 * time.Millisecond

// maxBodyBytes bounds one response body read.
const maxBodyBytes = 64 << 20

// maxPages bounds the pages of one query (a loop guard; real queries need a few).
const maxPages = 200

// Fixed error codes, as stored in research_jquants_fetch_log.last_failure_code.
const (
	CodeTransport   = "transport"
	CodeDecode      = "decode"
	CodeUnauthorize = "http_401"
	CodeForbidden   = "http_403"
	CodeRateLimited = "http_429"
	CodeServer      = "http_5xx"
	CodeClient      = "http_4xx"
)

// Errors of the adapter. None of them carries a response body or the key.
var (
	// ErrNoKey: no API key is set, so no request is made.
	ErrNoKey = errors.New("jquants: no API key set; no request is made")
	// ErrCapReached: the day's request cap is used up, so no request is made.
	ErrCapReached = errors.New("jquants: the daily request cap is reached")
	// ErrRateLimited: J-Quants answered 429; the run stops and waits for the next one.
	ErrRateLimited = errors.New("jquants: rate limited (HTTP 429)")
	// ErrAuth: J-Quants refused the key (401 / 403); the run stops.
	ErrAuth = errors.New("jquants: the API key was refused (HTTP 401/403)")
)

// HTTPError is a failed response: its status and fixed code only.
type HTTPError struct {
	Status int
	Code   string
}

func (e *HTTPError) Error() string { return fmt.Sprintf("jquants: HTTP %d (%s)", e.Status, e.Code) }

// Is makes errors.Is(err, ErrAuth) and errors.Is(err, ErrRateLimited) true for
// the matching statuses.
func (e *HTTPError) Is(target error) bool {
	switch target {
	case ErrAuth:
		return e.Status == http.StatusUnauthorized || e.Status == http.StatusForbidden
	case ErrRateLimited:
		return e.Status == http.StatusTooManyRequests
	}
	return false
}

// Ledger counts the requests of a JST day against the cap and records failures
// (CapLedger). The Client reserves a slot before every HTTP request and sends
// the request only if it got one.
type Ledger interface {
	// Reserve counts one request of dataset, or returns ErrCapReached.
	Reserve(ctx context.Context, dataset string) error
	// Failed records a failed request with its fixed code.
	Failed(ctx context.Context, dataset, code string)
}

// Config configures a Client.
type Config struct {
	// APIKey is the J-Quants API key (secret JQUANTS_API_KEY). Empty: inert.
	APIKey string
	// BaseURL overrides DefaultBaseURL (tests point it at an httptest server).
	BaseURL string
	// HTTP is the client to send with (tests inject a counting transport); nil
	// means a client with a 30 s timeout.
	HTTP *http.Client
	// Ledger counts requests against the daily cap; nil means a ledger that never refuses.
	Ledger Ledger
	// MinInterval is the least time between two requests; 0 means DefaultMinInterval,
	// a negative value none (tests).
	MinInterval time.Duration
	// Sleep waits for d or returns the context's error (tests inject a recorder).
	Sleep func(ctx context.Context, d time.Duration) error
	// Now is the clock; nil means time.Now.
	Now func() time.Time
}

// Client reads the J-Quants API V2. Safe for sequential use by one task.
type Client struct {
	apiKey   string
	baseURL  string
	http     *http.Client
	ledger   Ledger
	interval time.Duration
	sleep    func(ctx context.Context, d time.Duration) error
	now      func() time.Time
	last     time.Time
}

// NewClient returns a Client over cfg.
func NewClient(cfg Config) *Client {
	c := &Client{apiKey: cfg.APIKey, baseURL: cfg.BaseURL, http: cfg.HTTP, ledger: cfg.Ledger, interval: cfg.MinInterval, sleep: cfg.Sleep, now: cfg.Now}
	if c.baseURL == "" {
		c.baseURL = DefaultBaseURL
	}
	if c.http == nil {
		c.http = &http.Client{Timeout: 30 * time.Second}
	}
	if c.ledger == nil {
		c.ledger = noLedger{}
	}
	if c.interval == 0 {
		c.interval = DefaultMinInterval
	}
	if c.sleep == nil {
		c.sleep = sleepContext
	}
	if c.now == nil {
		c.now = time.Now
	}
	return c
}

// HasKey reports whether a key is set (the only thing a caller may learn of it).
func (c *Client) HasKey() bool { return c.apiKey != "" }

type noLedger struct{}

func (noLedger) Reserve(context.Context, string) error  { return nil }
func (noLedger) Failed(context.Context, string, string) {}

func sleepContext(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// page is the envelope of every list response.
type page struct {
	Data          []json.RawMessage `json:"data"`
	PaginationKey string            `json:"pagination_key"`
}

// fetch GETs path with query, following pagination_key, and returns every data
// element. Every HTTP request first takes a slot from the ledger and is paced.
// No key means no request at all.
func (c *Client) fetch(ctx context.Context, dataset, path string, query url.Values) ([]json.RawMessage, error) {
	if c.apiKey == "" {
		return nil, ErrNoKey
	}
	var all []json.RawMessage
	key := ""
	for range maxPages {
		q := url.Values{}
		for k, v := range query {
			q[k] = v
		}
		if key != "" {
			q.Set("pagination_key", key)
		}
		p, err := c.get(ctx, dataset, path, q)
		if err != nil {
			return nil, err
		}
		all = append(all, p.Data...)
		if p.PaginationKey == "" {
			return all, nil
		}
		key = p.PaginationKey
	}
	c.ledger.Failed(ctx, dataset, CodeDecode)
	return nil, &HTTPError{Status: http.StatusOK, Code: CodeDecode}
}

// get sends one request: slot, pause, GET with the key header, status, decode.
// Failures are returned as fixed errors (never the response body, never the key).
func (c *Client) get(ctx context.Context, dataset, path string, q url.Values) (page, error) {
	if err := c.ledger.Reserve(ctx, dataset); err != nil {
		return page{}, err
	}
	if err := c.pace(ctx); err != nil {
		return page{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path+"?"+q.Encode(), nil)
	if err != nil {
		c.ledger.Failed(ctx, dataset, CodeTransport)
		return page{}, &HTTPError{Code: CodeTransport}
	}
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return page{}, ctx.Err()
		}
		c.ledger.Failed(ctx, dataset, CodeTransport) // the transport error is dropped: it carries the URL
		return page{}, &HTTPError{Code: CodeTransport}
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		code := statusCode(resp.StatusCode)
		c.ledger.Failed(ctx, dataset, code)
		return page{}, &HTTPError{Status: resp.StatusCode, Code: code}
	}
	var p page
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBodyBytes)).Decode(&p); err != nil {
		c.ledger.Failed(ctx, dataset, CodeDecode)
		return page{}, &HTTPError{Status: resp.StatusCode, Code: CodeDecode}
	}
	return p, nil
}

// pace waits until MinInterval has passed since the previous request.
func (c *Client) pace(ctx context.Context) error {
	if c.interval > 0 && !c.last.IsZero() {
		if wait := c.interval - c.now().Sub(c.last); wait > 0 {
			if err := c.sleep(ctx, wait); err != nil {
				return err
			}
		}
	}
	c.last = c.now()
	return nil
}

func statusCode(status int) string {
	switch {
	case status == http.StatusUnauthorized:
		return CodeUnauthorize
	case status == http.StatusForbidden:
		return CodeForbidden
	case status == http.StatusTooManyRequests:
		return CodeRateLimited
	case status >= 500:
		return CodeServer
	}
	return CodeClient
}
