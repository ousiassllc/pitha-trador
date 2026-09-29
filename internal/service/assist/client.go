package assist

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

const (
	// defaultMaxAttempts bounds the total number of external AI API call
	// attempts (the initial call plus every retry) before giving up -
	// the same retry policy internal/service/jev.Client uses
	// (overview.md §6 "2回目以降exponential backoff").
	defaultMaxAttempts    = 4
	defaultRetryBaseDelay = 500 * time.Millisecond
	defaultHTTPTimeout    = 30 * time.Second
	// maxResponseBytes caps how much of an AI API response is read, so a
	// misbehaving endpoint cannot exhaust memory.
	maxResponseBytes = 1 << 20
)

// ErrNotConfigured is returned by Client.PostJSON (and therefore by
// Luna/Sol/Opus) when the client has no BaseURL, i.e. the operator has
// not yet filled in the matching *_BASE_URL on the Settings screen. Every
// caller treats it like any other API failure: the feature degrades
// (FR-LUNA-4, overview.md §8) instead of the process stopping.
var ErrNotConfigured = errors.New("assist: external AI API is not configured")

// APIError is returned when an external AI API responds with a non-200
// status.
type APIError struct {
	StatusCode int
	Body       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("assist: api error (status %d): %s", e.StatusCode, e.Body)
}

// Config configures a Client.
type Config struct {
	// Label names the adapter ("luna", "sol", "opus") in log lines and
	// error messages.
	Label string
	// BaseURL is the AI API base URL, e.g. "https://api.luna.example.com".
	BaseURL string
	// APIKey authenticates every request as a Bearer token. It is held
	// only in memory and never written to disk (overview.md §6).
	APIKey string
	// HTTPClient defaults to &http.Client{Timeout: 30s}.
	HTTPClient *http.Client
	// MaxAttempts bounds the total attempts per call. Defaults to 4.
	MaxAttempts int
	// RetryBaseDelay is the base backoff delay used from the second retry
	// onward: RetryBaseDelay * 2^n. Defaults to 500ms.
	RetryBaseDelay time.Duration
}

// Client is the HTTP client shared by the Luna, Sol and Opus adapters
// (overview.md §2 "Luna/Sol/Opusアダプタ | 独自HTTPクライアント"): JSON
// POST with Bearer authentication, plus Jev's retry policy - the first
// failure retries immediately, later failures back off exponentially.
type Client struct {
	label          string
	baseURL        string
	apiKey         string
	httpClient     *http.Client
	maxAttempts    int
	retryBaseDelay time.Duration
}

// NewClient returns a Client configured by cfg.
func NewClient(cfg Config) *Client {
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultHTTPTimeout}
	}
	maxAttempts := cfg.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = defaultMaxAttempts
	}
	retryBaseDelay := cfg.RetryBaseDelay
	if retryBaseDelay <= 0 {
		retryBaseDelay = defaultRetryBaseDelay
	}
	return &Client{
		label:          cfg.Label,
		baseURL:        strings.TrimRight(cfg.BaseURL, "/"),
		apiKey:         cfg.APIKey,
		httpClient:     httpClient,
		maxAttempts:    maxAttempts,
		retryBaseDelay: retryBaseDelay,
	}
}

// Configured reports whether c has a BaseURL to call.
func (c *Client) Configured() bool {
	return c != nil && c.baseURL != ""
}

// PostJSON POSTs req as JSON to c's BaseURL+path and decodes the 200
// response into resp. Failed attempts are retried per Config; once
// MaxAttempts is exhausted the last error is returned wrapped. An
// unconfigured client fails immediately with ErrNotConfigured (no retry).
func (c *Client) PostJSON(ctx context.Context, path string, req, resp any) error {
	if !c.Configured() {
		return ErrNotConfigured
	}

	var lastErr error
	for attempt := 1; attempt <= c.maxAttempts; attempt++ {
		lastErr = c.doPost(ctx, path, req, resp)
		if lastErr == nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if attempt == c.maxAttempts {
			break
		}
		if attempt >= 2 {
			backoff := c.retryBaseDelay * time.Duration(1<<uint(attempt-2))
			timer := time.NewTimer(backoff)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
	}
	slog.Error("assist: api call failed", "label", c.label, "path", path, "error", lastErr)
	return fmt.Errorf("assist: %s call failed after %d attempts: %w", c.label, c.maxAttempts, lastErr)
}

func (c *Client) doPost(ctx context.Context, path string, req, resp any) error {
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("assist: encode request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("assist: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("assist: request: %w", err)
	}
	defer func() { _ = httpResp.Body.Close() }()

	respBody, err := io.ReadAll(io.LimitReader(httpResp.Body, maxResponseBytes))
	if err != nil {
		return fmt.Errorf("assist: read response body: %w", err)
	}
	if httpResp.StatusCode != http.StatusOK {
		return &APIError{StatusCode: httpResp.StatusCode, Body: string(respBody)}
	}
	if err := json.Unmarshal(respBody, resp); err != nil {
		return fmt.Errorf("assist: decode response: %w", err)
	}
	return nil
}
