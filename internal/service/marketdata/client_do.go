package marketdata

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/httpbody"
)

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
