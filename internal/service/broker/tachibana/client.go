package tachibana

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/httpbody"
)

const (
	// defaultRequestsPerSecond is the queue-wide rate when none is configured
	// (config.DefaultTachibanaRequestMaxPerSecond is the Settings default).
	defaultRequestsPerSecond = 1
	// maxRequestsPerSecond is the broker's design limit (秒10件).
	maxRequestsPerSecond = 10

	httpTimeout = 30 * time.Second
	// masterMaxBytes caps the master responses (a few thousand listed
	// symbols); every other response uses httpbody.DefaultMaxBytes.
	masterMaxBytes = 4 * httpbody.DefaultMaxBytes
)

// Config configures a Client.
type Config struct {
	// BaseURL is the API base URL, e.g.
	// https://demo-kabuka.e-shiten.jp/e_api_v4r10/ (config.TachibanaSettings.BaseURL).
	BaseURL string
	// RequestsPerSecond is the queue-wide request budget, 1..10. Out of range
	// values are clamped (default 1).
	RequestsPerSecond int
	// HTTPClient overrides the default IPv4-only client (tests).
	HTTPClient *http.Client
	// Clock overrides the wall clock (tests).
	Clock Clock
}

// Client is the REQUEST I/F client for the REQUEST, MASTER and PRICE virtual
// URLs. One request is in flight at a time, in priority order, at most
// RequestsPerSecond per second; each carries a monotonic p_no and the current
// JST p_sd_date, and goes out as Shift-JIS JSON over HTTPS POST.
type Client struct {
	baseURL string
	http    *http.Client
	clock   Clock

	gate    gate
	limiter *rateWindow
	pNo     int64 // guarded by gate

	mu         sync.Mutex
	urls       virtualURLs
	haveURLs   bool
	validUntil time.Time // the 03:30 close; zero while there is no session
	gen        uint64
	observer   SessionObserver
	changed    chan struct{} // closed (and replaced) when a login installs a new session

	// outOfHours is true while the session is down because the broker is
	// closed: failures then are expected and not feed failures.
	outOfHours atomic.Bool

	boardFailures  domain.FailureStreak
	brokerFailures domain.FailureStreak
}

// NewClient returns a Client for cfg.
func NewClient(cfg Config) *Client {
	limit := cfg.RequestsPerSecond
	switch {
	case limit < 1:
		limit = defaultRequestsPerSecond
	case limit > maxRequestsPerSecond:
		limit = maxRequestsPerSecond
	}
	return &Client{
		baseURL: strings.TrimRight(cfg.BaseURL, "/") + "/",
		http:    orDefaultHTTPClient(cfg.HTTPClient),
		clock:   OrReal(cfg.Clock),
		limiter: newRateWindow(limit),
		changed: make(chan struct{}),
	}
}

// RequestsPerSecond is the effective queue-wide request budget.
func (c *Client) RequestsPerSecond() int { return c.limiter.limit }

// NewHTTPClient is the default HTTP client: IPv4 only (the broker answers
// IPv6 with 10005), no redirects. The EVENT WebSocket dial uses it too.
func NewHTTPClient() *http.Client { return orDefaultHTTPClient(nil) }

// orDefaultHTTPClient is hc, or a client that dials IPv4 only (IPv6 sources
// are refused by the broker with 10005) and never follows redirects (a
// redirect would carry the request to another URL).
func orDefaultHTTPClient(hc *http.Client) *http.Client {
	if hc != nil {
		return hc
	}
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	return &http.Client{
		Timeout: httpTimeout,
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: func(ctx context.Context, _, addr string) (net.Conn, error) {
				return dialer.DialContext(ctx, "tcp4", addr)
			},
			TLSHandshakeTimeout: 10 * time.Second,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// Call sends one request to the virtual URL of t and decodes the answer's
// payload into out (nil to ignore it). The request waits its turn behind
// higher-priority ones; a daytime-restricted priority waits out 8:00〜15:30.
// Failures: broker.ErrNoSession (no session yet, or closed for the night),
// *APIError, *HTTPStatusError, or a transport error without the URL.
func (c *Client) Call(ctx context.Context, t Target, prio Priority, clmID string, fields map[string]string, out any) error {
	release, err := c.enter(ctx, prio)
	if err != nil {
		return err
	}
	defer release()

	urls, gen, ok := c.session()
	if !ok {
		err := c.noSessionError()
		c.record(ctx, err)
		return err
	}
	limit := int64(httpbody.DefaultMaxBytes)
	if t == TargetMaster {
		limit = masterMaxBytes
	}
	err = c.send(ctx, urls.of(t), clmID, fields, out, limit)
	c.afterSession(ctx, gen, err)
	return err
}

// enter takes the gate, honoring the daytime restriction both before queueing
// and again after a long wait in the queue.
func (c *Client) enter(ctx context.Context, prio Priority) (func(), error) {
	for {
		if err := waitOutDaytime(ctx, c.clock, prio); err != nil {
			return nil, err
		}
		release, err := c.gate.acquire(ctx, prio)
		if err != nil {
			return nil, err
		}
		if prio.daytimeRestricted() && InDaytime(c.clock.Now()) {
			release()
			continue
		}
		return release, nil
	}
}

// send is one exchange: rate window, p_no, POST, decode. The caller holds the
// gate.
func (c *Client) send(ctx context.Context, url, clmID string, fields map[string]string, out any, limit int64) error {
	if err := c.limiter.wait(ctx, c.clock); err != nil {
		return err
	}
	if c.pNo >= maxPNo {
		return errors.New("tachibana: p_no exhausted, a new login is required")
	}
	c.pNo++
	body, err := requestBody(c.pNo, c.clock.Now(), clmID, fields)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return transportError(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return transportError(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return &HTTPStatusError{Status: resp.StatusCode}
	}
	raw, err := httpbody.ReadAll(resp.Body, limit)
	if err != nil {
		return fmt.Errorf("tachibana: read response body: %w", err)
	}
	err = decodeResponse(raw, out)
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.Kind() == KindSequence {
		// Resending cannot fix a numbering problem: report it loudly.
		slog.Error("tachibana: p_no was rejected (p_errno=6), this is a numbering bug", "p_no", c.pNo)
	}
	return err
}
