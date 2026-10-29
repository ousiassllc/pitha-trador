package gmo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/ousiassllc/pitha-trador/internal/httpbody"
	"github.com/ousiassllc/pitha-trador/internal/textutil"
)

// get performs one logical GET: it waits for the call-rate limit before every
// attempt, retries transient failures with exponential backoff and, after
// Config.MaxAttempts failures in a row, logs an error and returns
// ErrRetriesExhausted wrapping the last failure. When the server sends a
// Retry-After, the wait before the next attempt is the larger of the backoff
// and that value (capped at MaxRetryAfter), so it is never shorter than the
// backoff alone. Permanent failures and context cancellation are returned
// immediately. It returns the "data" field.
func (c *Client) get(ctx context.Context, path string, query url.Values) (json.RawMessage, error) {
	backoff := c.cfg.InitialBackoff
	var last error
	for attempt := 1; attempt <= c.cfg.MaxAttempts; attempt++ {
		if err := c.limiter.wait(ctx); err != nil {
			return nil, err
		}
		data, err := c.once(ctx, path, query)
		if err == nil {
			return data, nil
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		if !isRetryable(err) {
			c.logger.Warn("gmo: request failed (not retryable)", "path", path, "error", err)
			return nil, err
		}
		last = err
		if attempt == c.cfg.MaxAttempts {
			break
		}
		wait := retryWait(backoff, err)
		c.logger.Warn("gmo: request failed, retrying", "path", path, "attempt", attempt,
			"max_attempts", c.cfg.MaxAttempts, "backoff", backoff.String(), "wait", wait.String(), "error", err)
		if err := c.clock.Sleep(ctx, wait); err != nil {
			return nil, err
		}
		backoff = min(backoff*2, c.cfg.MaxBackoff)
	}
	c.logger.Error("gmo: giving up after repeated failures", "path", path,
		"attempts", c.cfg.MaxAttempts, "error", last)
	return nil, fmt.Errorf("%w after %d attempts: %w", ErrRetriesExhausted, c.cfg.MaxAttempts, last)
}

// once sends one request and unwraps GMO's envelope.
func (c *Client) once(ctx context.Context, path string, query url.Values) (json.RawMessage, error) {
	u := c.cfg.BaseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("gmo: build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, &transportError{err}
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := httpbody.ReadAll(resp.Body, httpbody.DefaultMaxBytes)
	if err != nil {
		if errors.Is(err, httpbody.ErrTooLarge) {
			return nil, invalid("%v", err)
		}
		return nil, &transportError{err}
	}

	var env envelope
	parseErr := json.Unmarshal(body, &env)
	retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"), c.clock.Now())
	if resp.StatusCode != http.StatusOK {
		return nil, &APIError{HTTPStatus: resp.StatusCode, Status: env.Status, Messages: env.Messages, RetryAfter: retryAfter}
	}
	if parseErr != nil {
		return nil, invalid("body: %v (%s)", parseErr, textutil.Excerpt(string(body), textutil.ErrorBodyExcerptBytes))
	}
	if env.Status != 0 {
		return nil, &APIError{HTTPStatus: resp.StatusCode, Status: env.Status, Messages: env.Messages, RetryAfter: retryAfter}
	}
	return env.Data, nil
}
