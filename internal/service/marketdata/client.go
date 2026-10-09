package marketdata

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/safego"
	"github.com/ousiassllc/pitha-trador/internal/service/marketdata/infolimit"
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
	// InfoAPIMaxPerSecond caps GetBoard/GetSymbol/RegisterSymbols (default 8, max 10).
	InfoAPIMaxPerSecond int
	// Clock drives the info-API limiter and stamps token failure streaks
	// (TokenStatus.Since); tests inject infolimit.ManualClock.
	Clock infolimit.Clock
}

// Client is a kabuステーションAPI REST client: token issuance/holding,
// universe registration for PUSH, and 時価情報・板情報(board) polling
// (docs/architecture/overview.md §5).
type Client struct {
	baseURL     string
	apiPassword string
	httpClient  *http.Client
	status      *StatusTracker
	now         func() time.Time

	boardFailures  domain.FailureStreak
	brokerFailures domain.FailureStreak
	limiter        *infolimit.Limiter

	mu          sync.RWMutex
	token       string
	tokenStatus TokenStatus
	// authFailure is the rejection that opened the information-API auth
	// circuit breaker (tokenrefresh.go), nil while closed; authRetryAt is
	// when the next probe call may pass. Both are guarded by mu.
	authFailure error
	authRetryAt time.Time
	reissueMu   sync.Mutex // serializes reactive reissues (tokenrefresh.go); guards lastReissue
	lastReissue time.Time

	// regMu guards pinned (PUSH-registered) and transient (REST-registered)
	// symbols; see rotation.go.
	regMu     sync.Mutex
	pinned    map[RegisterSymbol]struct{}
	transient map[RegisterSymbol]struct{}
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
	now := time.Now
	if cfg.Clock != nil {
		now = cfg.Clock.Now
	}
	return &Client{
		baseURL:     baseURL,
		apiPassword: cfg.APIPassword,
		httpClient:  httpClient,
		status:      status,
		now:         now,
		limiter:     infolimit.New(cfg.InfoAPIMaxPerSecond, cfg.Clock),
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
	c.tokenStatus = status.streak(c.tokenStatus, c.now())
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
	if c.tokenStatus.Failed() {
		return c.tokenStatus
	}
	// /token succeeds yet the information APIs keep rejecting the fresh token
	// (auth circuit breaker open, tokenrefresh.go).
	var api *APIError
	if errors.As(c.authFailure, &api) {
		return TokenStatus{Issue: TokenIssueRejected, Code: api.Code}
	}
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
