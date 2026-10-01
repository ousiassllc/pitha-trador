package jev

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Endpoint is the TypeSafe AI evaluation endpoint (appended to
// Config.BaseURL) that both Scout and Trader requests are POSTed to.
const Endpoint = "/v1/systemone"

// DefaultBaseURL is the production TypeSafe AI host, used when
// Config.BaseURL is empty (issue #271; the same pattern as
// marketdata.DefaultBaseURL).
const DefaultBaseURL = "https://api.typesafe.ai"

// DefaultModel is the model alias sent with every request when
// Config.Model is empty; the response reports the concrete model that
// served it (e.g. "jev-1.13.0").
const DefaultModel = "jev-latest"

const (
	// defaultMaxAttempts bounds the total number of Jev API call
	// attempts (the initial call plus every retry) before giving up
	// (overview.md §6 "継続失敗でnew entry停止").
	defaultMaxAttempts = 4
	// defaultRetryBaseDelay is the base exponential-backoff delay used
	// from the second retry onward (overview.md §6 "2回目以降exponential
	// backoff"), or from the first retry for 429/529 (evaluate.go).
	defaultRetryBaseDelay = 500 * time.Millisecond
	// defaultHTTPTimeout is the per-attempt HTTP timeout
	// (non-functional.md §2.2 "Jev Scout/Trader 1回呼び出し ... タイムアウト5秒").
	// With defaultMaxAttempts and the backoff above, a fully failing call
	// is bounded by 4*5s + 0.5s + 1s = 21.5s (23.5s for 429/529, which
	// back off from the first retry: 0.5s + 1s + 2s). That can exceed the 15-30s
	// re-evaluation cycle (§2.1), which is fine: Scout/Trader calls run as
	// asynchronous jobs and do not block the cycle.
	defaultHTTPTimeout = 5 * time.Second
	// defaultErrorRateWindow/defaultErrorRateThreshold configure the
	// rolling error-rate alert non-functional.md §5.2 requires ("Jev API
	// エラー率上昇（しきい値超過）", errorrate.go): the most recent 20
	// calls, alerting once the failure rate reaches 50%.
	defaultErrorRateWindow    = 20
	defaultErrorRateThreshold = 0.5
)

// Config configures a Client.
type Config struct {
	// BaseURL is the Jev API host, without a path, e.g.
	// "https://api.typesafe.ai" (DefaultBaseURL, used when empty);
	// Endpoint is appended to it. Trailing
	// slashes are trimmed; a path prefix (e.g. a reverse proxy at
	// "https://host/api") is kept as is and Endpoint is appended after it.
	BaseURL string
	// APIKey authenticates every request. It is held only in-memory by
	// Client and is never written to disk (overview.md §6).
	APIKey string
	// Model is the model alias sent with every request. Defaults to
	// DefaultModel when empty.
	Model string
	// HTTPClient is the HTTP client used for calls. Defaults to
	// &http.Client{Timeout: 5 * time.Second} (per attempt).
	HTTPClient *http.Client
	// MaxAttempts bounds the total number of attempts (initial call +
	// retries) per Client.Scout/Trader call. Defaults to 4.
	MaxAttempts int
	// RetryBaseDelay is the base backoff delay: RetryBaseDelay * 2^n,
	// where n starts at 0 on the second retry for transport errors and
	// 5xx, and on the first retry for 429/529. Defaults to 500ms.
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
	model              string
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
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	model := cfg.Model
	if model == "" {
		model = DefaultModel
	}
	return &Client{
		baseURL:            strings.TrimRight(baseURL, "/"),
		model:              model,
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
