package jev

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// DefaultScoutPath is the Jev API endpoint Scout requests are POSTed to.
const DefaultScoutPath = "/v1/scout"

// DefaultTraderPath is the Jev API endpoint Trader requests are POSTed
// to.
const DefaultTraderPath = "/v1/trader"

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
	// defaultErrorRateWindow/defaultErrorRateThreshold configure the
	// rolling error-rate alert non-functional.md §5.2 requires ("Jev API
	// エラー率上昇（しきい値超過）", errorrate.go): the most recent 20
	// calls, alerting once the failure rate reaches 50%.
	defaultErrorRateWindow    = 20
	defaultErrorRateThreshold = 0.5
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
	// Alerts defaults to NoopAlertNotifier{} (errorrate.go).
	Alerts AlertNotifier
	// ErrorRateWindow is how many of the most recent calls
	// errorRateTracker considers. Defaults to 20.
	ErrorRateWindow int
	// ErrorRateThreshold is the failure-rate fraction (0-1) that raises
	// Alerts.JevAPIErrorRateExceeded. Defaults to 0.5 (50%).
	ErrorRateThreshold float64
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
	baseURL            string
	apiKey             string
	httpClient         *http.Client
	maxAttempts        int
	retryBaseDelay     time.Duration
	alerts             AlertNotifier
	errorRate          *errorRateTracker
	errorRateThreshold float64
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
	alerts := cfg.Alerts
	if alerts == nil {
		alerts = NoopAlertNotifier{}
	}
	errorRateWindow := cfg.ErrorRateWindow
	if errorRateWindow <= 0 {
		errorRateWindow = defaultErrorRateWindow
	}
	errorRateThreshold := cfg.ErrorRateThreshold
	if errorRateThreshold <= 0 {
		errorRateThreshold = defaultErrorRateThreshold
	}
	return &Client{
		baseURL:            cfg.BaseURL,
		apiKey:             cfg.APIKey,
		httpClient:         httpClient,
		maxAttempts:        maxAttempts,
		retryBaseDelay:     retryBaseDelay,
		alerts:             alerts,
		errorRate:          newErrorRateTracker(errorRateWindow),
		errorRateThreshold: errorRateThreshold,
	}
}

// Healthy implements internal/service/risk.HealthChecker for the
// jev_api_down Kill Switch (FR-RISK-2 "Jev API連続失敗" trigger, FR-RISK-7
// auto-resume check): Jev API counts as down while its rolling call error
// rate is at or above ErrorRateThreshold (errorrate.go, the same signal
// as the §5.2 Slack alert) and as recovered once the window drops back
// below it.
func (c *Client) Healthy(context.Context) (bool, error) {
	return !c.errorRate.isBreached(), nil
}

// Scout POSTs req to the Jev Scout endpoint and returns the parsed
// response together with the total call latency (including retries).
//
// A failed attempt is retried per overview.md §6: the first failure is
// retried immediately (no delay), the second and every subsequent
// failure wait an exponentially growing backoff (RetryBaseDelay * 2^n,
// n starting at 0 on the second retry) before the next attempt. Once
// MaxAttempts is exhausted, Scout returns the last error and the caller
// records no new jev_decisions entry (継続失敗でnew entry停止). Trader
// shares this same retry policy.
func (c *Client) Scout(ctx context.Context, req ScoutRequest) (ScoutResponse, time.Duration, error) {
	return call[ScoutRequest, ScoutResponse](ctx, c, DefaultScoutPath, "scout", req)
}

// Trader POSTs req to the Jev Trader endpoint and returns the parsed
// response together with the total call latency (including retries),
// following the same retry policy Scout's doc comment describes.
func (c *Client) Trader(ctx context.Context, req TraderRequest) (TraderResponse, time.Duration, error) {
	return call[TraderRequest, TraderResponse](ctx, c, DefaultTraderPath, "trader", req)
}

// call POSTs req to path (retrying failed attempts per c's retry
// policy - see Scout's doc comment) and returns the decoded Resp
// together with the total call latency. label names the endpoint in the
// final error message (e.g. "scout", "trader") and the structured JSON
// log line this emits for every call (non-functional.md §5.1 "Jev API
// latency / エラー率" - counting log lines by label doubles as
// "Scout呼び出し回数、Trader呼び出し回数" without a separate counter).
// A call that ultimately fails also feeds c.errorRate; the call that
// first pushes its rolling error rate to c.errorRateThreshold notifies
// c.alerts (§5.2 "Jev APIエラー率上昇（しきい値超過）", errorrate.go).
func call[Req, Resp any](ctx context.Context, c *Client, path, label string, req Req) (resp Resp, latency time.Duration, err error) {
	start := time.Now()
	defer func() {
		latency = time.Since(start)
		attrs := []any{"label", label, "duration_ms", latency.Milliseconds()}
		if err != nil {
			slog.Error("jev: api call failed", append(attrs, "error", err)...)
		} else {
			slog.Info("jev: api call completed", attrs...)
		}
		if rate, newlyBreached := c.errorRate.record(err != nil, c.errorRateThreshold); newlyBreached {
			if alertErr := c.alerts.JevAPIErrorRateExceeded(ctx, rate, c.errorRateThreshold); alertErr != nil {
				slog.Error("jev: error rate alert failed", "error", alertErr)
			}
		}
	}()

	var lastErr error
	for attempt := 1; attempt <= c.maxAttempts; attempt++ {
		r, callErr := doCall[Req, Resp](ctx, c, path, req)
		if callErr == nil {
			resp = r
			return resp, 0, nil
		}
		lastErr = callErr

		if attempt == c.maxAttempts {
			break
		}
		if attempt >= 2 {
			backoff := c.retryBaseDelay * time.Duration(1<<uint(attempt-2))
			timer := time.NewTimer(backoff)
			select {
			case <-ctx.Done():
				timer.Stop()
				err = ctx.Err()
				return resp, 0, err
			case <-timer.C:
			}
		}
	}
	err = fmt.Errorf("jev: %s call failed after %d attempts: %w", label, c.maxAttempts, lastErr)
	return resp, 0, err
}

func doCall[Req, Resp any](ctx context.Context, c *Client, path string, req Req) (Resp, error) {
	var zero Resp

	body, err := json.Marshal(req)
	if err != nil {
		return zero, fmt.Errorf("jev: encode request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return zero, fmt.Errorf("jev: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return zero, fmt.Errorf("jev: request: %w", err)
	}
	defer func() { _ = httpResp.Body.Close() }()

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return zero, fmt.Errorf("jev: read response body: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		return zero, &APIError{StatusCode: httpResp.StatusCode, Body: string(respBody)}
	}

	var out Resp
	if err := json.Unmarshal(respBody, &out); err != nil {
		return zero, fmt.Errorf("jev: decode response: %w", err)
	}
	return out, nil
}
