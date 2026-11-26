package trialwire

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/httpbody"
	"github.com/ousiassllc/pitha-trador/internal/service/jev"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/systemone"
)

const (
	// DefaultBaseURL is OpenRouter's API root, used when Config.BaseURL is
	// empty. It is the trial's own default; the live jev.DefaultBaseURL is a
	// different constant and is not used here.
	DefaultBaseURL = "https://openrouter.ai/api"
	// DefaultEndpoint is the System One path, the same one the live client
	// uses (jev.Endpoint; a test pins them equal). Whether OpenRouter's
	// System One path accepts a non-Jev model is unverified (issue #1057).
	DefaultEndpoint = "/v1/systemone"
	// DecisionsEndpoint is OpenRouter's Decisions API path, to switch to when
	// DefaultEndpoint does not accept a model. It is an Alpha API: its shape
	// may change.
	DecisionsEndpoint = "/alpha/decisions"
	// ChatCompletionsEndpoint is the chat Completions path, for a general chat model
	// (issue #1224): the questions are sent as chat messages with a structured JSON
	// output (chat.go) and the answer is read back from the message. It is relative
	// to the base URL (OpenRouter's is .../api), so it is not "/api/v1/chat/completions".
	ChatCompletionsEndpoint = "/v1/chat/completions"

	defaultHTTPTimeout = 30 * time.Second
)

// Config configures a Client.
type Config struct {
	// BaseURL is the API root without the endpoint path; trailing slashes are
	// trimmed. Defaults to DefaultBaseURL.
	BaseURL string
	// Endpoint is the path appended to BaseURL and must start with "/".
	// Defaults to DefaultEndpoint.
	Endpoint string
	// APIKey authenticates every request (Bearer). It is held in memory only
	// and never logged or put in an error.
	APIKey string
	// Model is the model id sent with every request, e.g. "cloudflare/clef-flash".
	Model string
	// Provider is the routing preference sent with every request
	// (provider.only, allow_fallbacks, data_collection, zdr). nil sends none.
	Provider *systemone.Provider
	// HTTPClient is this client's own HTTP client (a trial arm never shares
	// one with another arm or with the live Jev). Defaults to one with a 30s
	// timeout.
	HTTPClient *http.Client
}

// Client calls one trial arm's model.
type Client struct {
	url string
	// chat is true for ChatCompletionsEndpoint: the questions go as chat messages
	// with a structured output instead of as System One's {state, questions}.
	chat     bool
	apiKey   string
	model    string
	provider *systemone.Provider
	http     *http.Client
}

// New returns a Client configured by cfg.
func New(cfg Config) *Client {
	base := cfg.BaseURL
	if base == "" {
		base = DefaultBaseURL
	}
	endpoint := cfg.Endpoint
	if endpoint == "" {
		endpoint = DefaultEndpoint
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultHTTPTimeout}
	}
	return &Client{url: strings.TrimRight(base, "/") + endpoint, chat: endpoint == ChatCompletionsEndpoint, apiKey: cfg.APIKey, model: cfg.Model, provider: cfg.Provider, http: httpClient}
}

// Configured reports whether an API key is set; a trial arm without one is
// never called.
func (c *Client) Configured() bool { return c != nil && c.apiKey != "" }

// do POSTs req once and reads the answers with systemone.DecodeTrial. A non-200
// answer is returned as a *jev.APIError (Body size-capped, Error() an excerpt),
// an answer that is not a valid answer set wraps systemone.ErrInvalidResponse.
// The returned request size is set whenever the body was built, on failure too.
func (c *Client) do(ctx context.Context, req systemone.Request) (res systemone.Result, requestBytes int, err error) {
	body, err := json.Marshal(req)
	if err != nil {
		return systemone.Result{}, 0, fmt.Errorf("trialwire: encode request: %w", err)
	}
	respBody, err := c.post(ctx, body)
	if err != nil {
		return systemone.Result{}, len(body), err
	}
	res, err = systemone.DecodeTrial(req, respBody)
	return res, len(body), err
}

// post POSTs body once and returns the response body of a 200 answer. A non-200
// answer is a *jev.APIError (Body size-capped, Error() an excerpt).
func (c *Client) post(ctx context.Context, body []byte) ([]byte, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("trialwire: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	httpResp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("trialwire: request: %w", err)
	}
	defer func() { _ = httpResp.Body.Close() }()
	respBody, err := httpbody.ReadAll(httpResp.Body, httpbody.DefaultMaxBytes)
	if err != nil {
		return nil, fmt.Errorf("trialwire: read response body: %w", err)
	}
	if httpResp.StatusCode != http.StatusOK {
		return nil, &jev.APIError{StatusCode: httpResp.StatusCode, Body: string(respBody)}
	}
	return respBody, nil
}
