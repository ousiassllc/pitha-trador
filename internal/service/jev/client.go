package jev

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// DefaultScoutPath is the Jev API endpoint Scout requests are POSTed to.
const DefaultScoutPath = "/v1/scout"

const (
	// defaultMaxAttempts bounds the total number of Jev API call
	// attempts (the initial call plus every retry) before giving up
	// (overview.md §6 "継続失敗でnew entry停止").
	defaultMaxAttempts = 4
	// defaultRetryBaseDelay is the base exponential-backoff delay used
	// from the second retry onward (overview.md §6 "2回目以降exponential
	// backoff").
	defaultRetryBaseDelay = 500 * time.Millisecond
	defaultHTTPTimeout    = 10 * time.Second
)

// Config configures a Client.
type Config struct {
	// BaseURL is the Jev API base URL, e.g. "https://api.jev.example.com".
	BaseURL string
	// APIKey authenticates every request. It is held only in-memory by
	// Client and is never written to disk (overview.md §6).
	APIKey string
	// HTTPClient is the HTTP client used for calls. Defaults to
	// &http.Client{Timeout: 10 * time.Second}.
	HTTPClient *http.Client
	// MaxAttempts bounds the total number of attempts (initial call +
	// retries) per Client.Scout call. Defaults to 4.
	MaxAttempts int
	// RetryBaseDelay is the base backoff delay used from the second
	// retry onward: RetryBaseDelay * 2^n. Defaults to 500ms.
	RetryBaseDelay time.Duration
}

// APIError is returned when the Jev API responds with a non-200 status.
type APIError struct {
	StatusCode int
	Body       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("jev: api error (status %d): %s", e.StatusCode, e.Body)
}

// Client is the Jev API HTTP client (architecture/overview.md §6).
type Client struct {
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
		baseURL:        cfg.BaseURL,
		apiKey:         cfg.APIKey,
		httpClient:     httpClient,
		maxAttempts:    maxAttempts,
		retryBaseDelay: retryBaseDelay,
	}
}

// Scout POSTs req to the Jev Scout endpoint and returns the parsed
// response together with the total call latency (including retries).
//
// A failed attempt is retried per overview.md §6: the first failure is
// retried immediately (no delay), the second and every subsequent
// failure wait an exponentially growing backoff (RetryBaseDelay * 2^n,
// n starting at 0 on the second retry) before the next attempt. Once
// MaxAttempts is exhausted, Scout returns the last error and the caller
// records no new jev_decisions entry (継続失敗でnew entry停止).
func (c *Client) Scout(ctx context.Context, req ScoutRequest) (ScoutResponse, time.Duration, error) {
	start := time.Now()

	var lastErr error
	for attempt := 1; attempt <= c.maxAttempts; attempt++ {
		resp, err := c.doScout(ctx, req)
		if err == nil {
			return resp, time.Since(start), nil
		}
		lastErr = err

		if attempt == c.maxAttempts {
			break
		}
		if attempt >= 2 {
			backoff := c.retryBaseDelay * time.Duration(1<<uint(attempt-2))
			timer := time.NewTimer(backoff)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ScoutResponse{}, time.Since(start), ctx.Err()
			case <-timer.C:
			}
		}
	}
	return ScoutResponse{}, time.Since(start),
		fmt.Errorf("jev: scout call failed after %d attempts: %w", c.maxAttempts, lastErr)
}

func (c *Client) doScout(ctx context.Context, req ScoutRequest) (ScoutResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return ScoutResponse{}, fmt.Errorf("jev: encode scout request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+DefaultScoutPath, bytes.NewReader(body))
	if err != nil {
		return ScoutResponse{}, fmt.Errorf("jev: build scout request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return ScoutResponse{}, fmt.Errorf("jev: scout request: %w", err)
	}
	defer func() { _ = httpResp.Body.Close() }()

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return ScoutResponse{}, fmt.Errorf("jev: read scout response body: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		return ScoutResponse{}, &APIError{StatusCode: httpResp.StatusCode, Body: string(respBody)}
	}

	var out ScoutResponse
	if err := json.Unmarshal(respBody, &out); err != nil {
		return ScoutResponse{}, fmt.Errorf("jev: decode scout response: %w", err)
	}
	return out, nil
}
