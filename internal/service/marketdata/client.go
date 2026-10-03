package marketdata

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/httpbody"
	"github.com/ousiassllc/pitha-trador/internal/safego"
)

// DefaultBaseURL is kabuステーションAPIの既定ローカルエンドポイント
// (docs/architecture/overview.md §5: "本番環境ではポート番号が18080")。
const DefaultBaseURL = "http://localhost:18080/kabusapi"

// kabuステーションAPIの市場コード定義値
// (kabu_STATION_API.yaml components.schemas.RequestRegister)。
const (
	ExchangeTSE       = 1  // 東証
	ExchangeNSE       = 3  // 名証
	ExchangeFSE       = 5  // 福証
	ExchangeSSE       = 6  // 札証
	ExchangeWholeDay  = 2  // 日通し
	ExchangeDaytime   = 23 // 日中
	ExchangeNighttime = 24 // 夜間
)

// ErrNoToken is returned by methods that require a token when none has
// been issued yet (call IssueToken or Start first).
var ErrNoToken = errors.New("marketdata: no token issued yet")

// Config configures a Client.
type Config struct {
	// BaseURL is the kabuステーションAPI base URL, e.g.
	// "http://localhost:18080/kabusapi". Defaults to DefaultBaseURL.
	BaseURL string
	// APIPassword authenticates token issuance (/token). It is held only
	// in-memory by Client and is never written to disk (overview.md §5).
	APIPassword string
	// HTTPClient is the HTTP client used for REST calls. Defaults to
	// &http.Client{Timeout: 10 * time.Second}.
	HTTPClient *http.Client
	// Status receives freshness updates for every symbol touched by
	// GetBoard. Defaults to a fresh NewStatusTracker(); pass a shared
	// tracker to combine REST and PUSH freshness (see PushClient).
	Status *StatusTracker
}

// Client is a kabuステーションAPI REST client: token issuance/holding,
// universe registration for PUSH, and 時価情報・板情報(board) polling
// (docs/architecture/overview.md §5).
type Client struct {
	baseURL     string
	apiPassword string
	httpClient  *http.Client
	status      *StatusTracker

	boardFailures  domain.FailureStreak
	brokerFailures domain.FailureStreak

	mu          sync.RWMutex
	token       string
	tokenStatus TokenStatus
}

// NewClient returns a Client configured by cfg. The returned Client holds
// no token until IssueToken or Start is called.
func NewClient(cfg Config) *Client {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	status := cfg.Status
	if status == nil {
		status = NewStatusTracker()
	}
	return &Client{
		baseURL:     baseURL,
		apiPassword: cfg.APIPassword,
		httpClient:  httpClient,
		status:      status,
	}
}

// Status returns the StatusTracker this Client reports symbol freshness
// to, shared across REST polls and, if wired via PushClient, PUSH
// messages.
func (c *Client) Status() *StatusTracker { return c.status }

// Token returns the currently held token and whether one has been issued.
func (c *Client) Token() (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.token, c.token != ""
}

type tokenRequest struct {
	APIPassword string `json:"APIPassword"`
}

type tokenResponse struct {
	ResultCode int    `json:"ResultCode"`
	Token      string `json:"Token"`
}

// IssueToken POSTs the API password to /token and holds the returned
// token in memory only (never persisted to disk, overview.md §5). It
// returns the newly issued token.
func (c *Client) IssueToken(ctx context.Context) (string, error) {
	token, err := c.issueToken(ctx)
	status := classifyTokenError(err)
	c.mu.Lock()
	c.tokenStatus = status
	if err == nil {
		c.token = token
	}
	c.mu.Unlock()
	return token, err
}

func (c *Client) issueToken(ctx context.Context) (string, error) {
	var resp tokenResponse
	if err := c.do(ctx, http.MethodPost, "/token", "", tokenRequest{APIPassword: c.apiPassword}, &resp); err != nil {
		return "", err
	}
	if resp.ResultCode != 0 {
		return "", &APIError{StatusCode: http.StatusOK, Code: resp.ResultCode}
	}
	return resp.Token, nil
}

// TokenStatus reports the outcome of the most recent token issuance:
// which cause (kabuステーション未起動 / 未ログイン / APIパスワード不正 ...)
// a failure had, so the UI can say what to fix (issue #295).
func (c *Client) TokenStatus() TokenStatus {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.tokenStatus
}

// tokenRetryInitial is the first wait before retrying a failed token
// issuance while no token is held (Start); it doubles per failure up to
// the reissue interval.
const tokenRetryInitial = 30 * time.Second

// Start issues an initial token synchronously, then reissues it every
// interval in a background goroutine until ctx is done (overview.md §5
// "有効期限があるため...定期的に再発行"). If the initial issuance fails
// (kabuステーション not running or logged in yet, issue #295), Start
// returns that error but still launches the goroutine, which retries with
// a doubling delay (tokenRetryInitial, capped at interval) until a token is
// obtained, so starting kabuステーション after this app recovers without a
// restart. If a reissue fails while a token is held, Start keeps the
// previous token in memory and logs the error rather than clearing it,
// since the previous token remains usable until kabuステーション
// invalidates it.
func (c *Client) Start(ctx context.Context, interval time.Duration) error {
	_, initialErr := c.IssueToken(ctx)

	go func() {
		retry := min(tokenRetryInitial, interval)
		wait := interval
		if initialErr != nil {
			wait = retry
		}
		timer := time.NewTimer(wait)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				// Per-cycle guard: a panic is logged and the loop keeps reissuing.
				err := safego.Try("marketdata token reissue", func() error {
					_, err := c.IssueToken(ctx)
					return err
				})
				switch _, held := c.Token(); {
				case err == nil:
					retry = min(tokenRetryInitial, interval)
					wait = interval
				case held:
					status := c.TokenStatus()
					slog.Error("marketdata: token reissue failed, keeping previous token", "issue", status.Issue, "guidance", status.Guidance(), "error", err)
					wait = interval
				default:
					status := c.TokenStatus()
					slog.Error("marketdata: token issuance failed, will retry", "issue", status.Issue, "guidance", status.Guidance(), "retry_in", retry.String(), "error", err)
					wait = retry
					retry = min(retry*2, interval)
				}
				timer.Reset(wait)
			}
		}
	}()

	if initialErr != nil {
		return fmt.Errorf("marketdata: initial token issuance: %w", initialErr)
	}
	return nil
}

// do performs one kabuステーションAPI request, marshaling body (if
// non-nil) as the JSON request payload, attaching token as X-API-KEY (if
// non-empty), and decoding a 200 response body into out. Non-200
// responses are decoded as an ErrorResponse and returned as *APIError.
//
// Every call emits a structured JSON log line (method, path, duration,
// error) satisfying non-functional.md §5.1's "Market data fetch latency
// / エラー率" and "kabuステーションAPI（Broker）latency / エラー" - both
// line items name this same client, since Broker order placement (Phase
// 7) has not started yet and GetBoard/RegisterSymbols/IssueToken already
// cover every kabuステーションAPI call this build makes.
func (c *Client) do(ctx context.Context, method, path, token string, body, out any) (err error) {
	start := time.Now()
	defer func() {
		attrs := []any{"method", method, "path", path, "duration_ms", time.Since(start).Milliseconds()}
		c.recordBrokerOutcome(err)
		if err != nil {
			slog.Error("marketdata: api call failed", append(attrs, "error", err)...)
			return
		}
		slog.Info("marketdata: api call completed", attrs...)
	}()

	var reqBody io.Reader
	if body != nil {
		encoded, encodeErr := json.Marshal(body)
		if encodeErr != nil {
			err = fmt.Errorf("marketdata: encode request body: %w", encodeErr)
			return err
		}
		reqBody = bytes.NewReader(encoded)
	}

	req, reqErr := http.NewRequestWithContext(ctx, method, c.baseURL+path, reqBody)
	if reqErr != nil {
		err = fmt.Errorf("marketdata: build request: %w", reqErr)
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("X-API-KEY", token)
	}

	resp, doErr := c.httpClient.Do(req)
	if doErr != nil {
		err = fmt.Errorf("marketdata: request %s %s: %w", method, path, doErr)
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, readErr := httpbody.ReadAll(resp.Body, httpbody.DefaultMaxBytes)
	if readErr != nil {
		err = fmt.Errorf("marketdata: read response body: %w", readErr)
		return err
	}

	if resp.StatusCode != http.StatusOK {
		var apiErr struct {
			Code    int    `json:"Code"`
			Message string `json:"Message"`
		}
		_ = json.Unmarshal(respBody, &apiErr)
		err = &APIError{StatusCode: resp.StatusCode, Code: apiErr.Code, Message: apiErr.Message}
		return err
	}

	if out == nil {
		return nil
	}
	if decodeErr := json.Unmarshal(respBody, out); decodeErr != nil {
		err = fmt.Errorf("marketdata: decode response body: %w", decodeErr)
		return err
	}
	return nil
}
