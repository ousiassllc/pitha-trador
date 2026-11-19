// Package edinet is the read-only client of the EDINET API v2 (金融庁) document
// list (issue #1096). It asks for the LIST of the documents submitted on one
// day (documents.json?type=2) and nothing else: it has no method that
// downloads a document, so the bodies (XBRL, PDF, CSV) can never be fetched in
// bulk or at all.
//
// EDINET carries the API key in the query string (Subscription-Key), so a URL
// must never be logged or put in an error: every failure leaves this package as
// a *RequestError, a short fixed code and an HTTP status, never the URL, the
// key or a response body. Without a key the client sends no request.
package edinet

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain/disclosuresrc"
	"github.com/ousiassllc/pitha-trador/internal/httpbody"
)

const (
	// DefaultBaseURL is the EDINET API v2 root.
	DefaultBaseURL = "https://api.edinet-fsa.go.jp/api/v2"

	defaultTimeout = 30 * time.Second
	// maxListBytes caps one day's document list (a busy day is a few MiB of JSON).
	maxListBytes = 16 << 20
)

// jst is the zone of EDINET's submitDateTime ("2026-11-12 15:00" is JST).
var jst = time.FixedZone("JST", 9*60*60)

// ErrNotConfigured is returned, without any request, when no API key is set.
var ErrNotConfigured = errors.New("edinet: API key is not set")

// Config configures a Client.
type Config struct {
	// BaseURL defaults to DefaultBaseURL (tests point it at a fake server).
	BaseURL string
	// APIKey is EDINET's Subscription-Key. Held in memory only; never logged,
	// never in an error. Empty: the client sends nothing.
	APIKey string
	// HTTPClient defaults to a client with a 30 s timeout.
	HTTPClient *http.Client
}

// Client reads EDINET's document list.
type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

// New returns a Client configured by cfg.
func New(cfg Config) *Client {
	base := strings.TrimRight(cfg.BaseURL, "/")
	if base == "" {
		base = DefaultBaseURL
	}
	h := cfg.HTTPClient
	if h == nil {
		h = &http.Client{Timeout: defaultTimeout}
	}
	return &Client{baseURL: base, apiKey: cfg.APIKey, http: h}
}

// Configured reports whether an API key is set.
func (c *Client) Configured() bool { return c != nil && c.apiKey != "" }

// RequestError is a failed request: Code is one of the disclosuresrc.Fail* /
// HTTPFailureCode values and Status the HTTP status (0 when there was none). It
// carries no URL, key or response text.
type RequestError struct {
	Code   string
	Status int
}

func (e *RequestError) Error() string {
	if e.Status != 0 {
		return "edinet: request failed: " + e.Code + " (HTTP " + strconv.Itoa(e.Status) + ")"
	}
	return "edinet: request failed: " + e.Code
}

// List is one day's document list as the recorder stores it.
type List struct {
	// Total is the number of documents the list returned.
	Total int
	// Docs are the documents with a securities code and a readable submission time.
	Docs []disclosuresrc.EdinetDocument
	// Skipped are the documents left out (no secCode, e.g. investment trusts,
	// or an unreadable submission time).
	Skipped int
}

type listResponse struct {
	Metadata struct {
		Status string `json:"status"`
	} `json:"metadata"`
	// StatusCode is set on EDINET's gateway error body ({"statusCode":401,...}).
	StatusCode int `json:"statusCode"`
	Results    []struct {
		DocID          string `json:"docID"`
		EdinetCode     string `json:"edinetCode"`
		SecCode        string `json:"secCode"`
		FilerName      string `json:"filerName"`
		DocTypeCode    string `json:"docTypeCode"`
		SubmitDateTime string `json:"submitDateTime"`
		DocDescription string `json:"docDescription"`
	} `json:"results"`
}

// ListDocuments fetches the document list of the JST day `day`
// (YYYY-MM-DD). It sends exactly one request, and none without a key.
func (c *Client) ListDocuments(ctx context.Context, day string) (List, error) {
	if !c.Configured() {
		return List{}, ErrNotConfigured
	}
	if _, err := time.Parse(time.DateOnly, day); err != nil {
		return List{}, &RequestError{Code: disclosuresrc.FailInvalidResponse}
	}
	q := url.Values{"date": {day}, "type": {"2"}, "Subscription-Key": {c.apiKey}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/documents.json?"+q.Encode(), nil)
	if err != nil {
		return List{}, &RequestError{Code: disclosuresrc.FailTransport}
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return List{}, &RequestError{Code: transportCode(err)}
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return List{}, &RequestError{Code: disclosuresrc.HTTPFailureCode(resp.StatusCode), Status: resp.StatusCode}
	}
	body, err := httpbody.ReadAll(resp.Body, maxListBytes)
	if err != nil {
		return List{}, &RequestError{Code: disclosuresrc.FailInvalidResponse}
	}
	return decode(body)
}

// transportCode classifies a transport failure without keeping its text (a
// *url.Error text holds the URL, and the URL the key).
func transportCode(err error) string {
	var ne net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout()) {
		return disclosuresrc.FailTimeout
	}
	return disclosuresrc.FailTransport
}

func decode(body []byte) (List, error) {
	var r listResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return List{}, &RequestError{Code: disclosuresrc.FailInvalidResponse}
	}
	if r.StatusCode != 0 && r.StatusCode != http.StatusOK {
		return List{}, &RequestError{Code: disclosuresrc.HTTPFailureCode(r.StatusCode), Status: r.StatusCode}
	}
	if s := r.Metadata.Status; s != "" && s != "200" {
		n, convErr := strconv.Atoi(s)
		if convErr != nil {
			return List{}, &RequestError{Code: disclosuresrc.FailAPIError}
		}
		return List{}, &RequestError{Code: disclosuresrc.HTTPFailureCode(n), Status: n}
	}
	out := List{Total: len(r.Results)}
	for _, it := range r.Results {
		symbol, okCode := disclosuresrc.NormalizeSymbol(it.SecCode)
		submitted, perr := time.ParseInLocation("2006-01-02 15:04", it.SubmitDateTime, jst)
		if !okCode || perr != nil || it.DocID == "" {
			out.Skipped++
			continue
		}
		out.Docs = append(out.Docs, disclosuresrc.EdinetDocument{
			DocID: it.DocID, Symbol: symbol, SecCode: it.SecCode, EdinetCode: it.EdinetCode, FilerName: it.FilerName,
			DocTypeCode: it.DocTypeCode, Title: it.DocDescription, SubmittedAt: submitted.UTC(),
		})
	}
	return out, nil
}
