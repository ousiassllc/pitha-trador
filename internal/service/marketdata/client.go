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

	mu    sync.RWMutex
	token string
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
	var resp tokenResponse
	if err := c.do(ctx, http.MethodPost, "/token", "", tokenRequest{APIPassword: c.apiPassword}, &resp); err != nil {
		return "", err
	}
	if resp.ResultCode != 0 {
		return "", &APIError{StatusCode: http.StatusOK, Code: resp.ResultCode}
	}

	c.mu.Lock()
	c.token = resp.Token
	c.mu.Unlock()
	return resp.Token, nil
}

// Start issues an initial token synchronously, then reissues it every
// interval in a background goroutine until ctx is done (overview.md §5
// "有効期限があるため...定期的に再発行"). If a reissue fails, Start keeps
// the previous token in memory and logs the error rather than clearing
// it, since the previous token remains usable until kabuステーション
// invalidates it.
func (c *Client) Start(ctx context.Context, interval time.Duration) error {
	if _, err := c.IssueToken(ctx); err != nil {
		return fmt.Errorf("marketdata: initial token issuance: %w", err)
	}

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if _, err := c.IssueToken(ctx); err != nil {
					slog.Error("marketdata: token reissue failed, keeping previous token", "error", err)
				}
			}
		}
	}()
	return nil
}

// RegisterSymbol identifies one instrument for PUSH registration
// (kabu_STATION_API.yaml components.schemas.RequestRegister).
type RegisterSymbol struct {
	Symbol   string `json:"Symbol"`
	Exchange int    `json:"Exchange"`
}

type registerRequest struct {
	Symbols []RegisterSymbol `json:"Symbols"`
}

// RegisterSuccess lists the symbols currently registered for PUSH after a
// RegisterSymbols call.
type RegisterSuccess struct {
	RegistList []RegisterSymbol `json:"RegistList"`
}

// RegisterSymbols registers symbols with kabuステーションAPI so their
// price/board updates are delivered over the PUSH WebSocket
// (overview.md §5 "銘柄登録・PUSH購読"). It requires a token (IssueToken
// or Start must be called first).
func (c *Client) RegisterSymbols(ctx context.Context, symbols []RegisterSymbol) (RegisterSuccess, error) {
	token, ok := c.Token()
	if !ok {
		return RegisterSuccess{}, ErrNoToken
	}

	var resp RegisterSuccess
	if err := c.do(ctx, http.MethodPut, "/register", token, registerRequest{Symbols: symbols}, &resp); err != nil {
		return RegisterSuccess{}, err
	}
	return resp, nil
}

// Board is the 時価情報・板情報 response for one symbol
// (kabu_STATION_API.yaml components.schemas.BoardSuccess). Bid/Ask
// price/quantity fields are nil when kabuステーションAPI has not yet
// resolved a value for a newly-registered symbol (FR-FE-2 欠損値扱い).
//
// Note: kabuステーションAPI's "Bid"/"Ask" field names are reported
// swapped from their conventional meaning (BoardSuccess's description:
// BidPrice is actually the best sell/offer quote, AskPrice the best
// buy/bid quote). This struct keeps the raw field names as documented by
// the API so callers can cross-reference the official reference.
type Board struct {
	Symbol        string   `json:"Symbol"`
	SymbolName    string   `json:"SymbolName"`
	CurrentPrice  float64  `json:"CurrentPrice"`
	VWAP          float64  `json:"VWAP"`
	TradingVolume float64  `json:"TradingVolume"`
	TradingValue  float64  `json:"TradingValue"`
	BidPrice      *float64 `json:"BidPrice"`
	BidQty        *float64 `json:"BidQty"`
	AskPrice      *float64 `json:"AskPrice"`
	AskQty        *float64 `json:"AskQty"`
}

// GetBoard fetches 時価情報・板情報 (current price, session VWAP,
// cumulative volume/turnover, and best bid/ask) for symbol@exchange over
// REST (overview.md §4 Market Data Client, §5). It records the result
// with the Client's StatusTracker: fresh on success, stale on any error.
func (c *Client) GetBoard(ctx context.Context, symbol string, exchange int) (Board, error) {
	token, ok := c.Token()
	if !ok {
		return Board{}, ErrNoToken
	}

	var board Board
	path := fmt.Sprintf("/board/%s@%d", symbol, exchange)
	if err := c.do(ctx, http.MethodGet, path, token, nil, &board); err != nil {
		c.status.MarkStale(symbol, err)
		return Board{}, err
	}
	c.status.MarkFresh(symbol, time.Now().UTC())
	return board, nil
}

// do performs one kabuステーションAPI request, marshaling body (if
// non-nil) as the JSON request payload, attaching token as X-API-KEY (if
// non-empty), and decoding a 200 response body into out. Non-200
// responses are decoded as an ErrorResponse and returned as *APIError.
func (c *Client) do(ctx context.Context, method, path, token string, body, out any) error {
	var reqBody io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marketdata: encode request body: %w", err)
		}
		reqBody = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reqBody)
	if err != nil {
		return fmt.Errorf("marketdata: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("X-API-KEY", token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("marketdata: request %s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("marketdata: read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var apiErr struct {
			Code    int    `json:"Code"`
			Message string `json:"Message"`
		}
		_ = json.Unmarshal(respBody, &apiErr)
		return &APIError{StatusCode: resp.StatusCode, Code: apiErr.Code, Message: apiErr.Message}
	}

	if out == nil {
		return nil
	}
	if err := json.Unmarshal(respBody, out); err != nil {
		return fmt.Errorf("marketdata: decode response body: %w", err)
	}
	return nil
}
