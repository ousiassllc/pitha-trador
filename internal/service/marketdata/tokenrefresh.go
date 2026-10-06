package marketdata

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"
)

// minReactiveReissueInterval spaces reactive token reissues (a 401 /
// 4001009 seen by doInfo) so a token the server keeps rejecting cannot make
// every information-API call hit /token (issue #621).
const minReactiveReissueInterval = 10 * time.Second

var errReissueThrottled = errors.New("marketdata: reactive token reissue throttled")

// isTokenRejected reports whether err says the held token is no longer
// valid: HTTP 401 or 4001009 (APIキー不一致). kabuステーション invalidates
// the token on restart, logout or when another token is issued.
func isTokenRejected(err error) bool {
	var api *APIError
	return errors.As(err, &api) && (api.StatusCode == http.StatusUnauthorized || api.Code == codeAPIKeyMismatch)
}

// doInfo is doInfoOnce plus a one-shot recovery from a revoked token
// (issue #621): on 401 / 4001009 it reissues the token (single-flight, see
// refreshRejectedToken) and retries the request once with the new token.
// If the reissue fails, or yields nothing new, the original error is returned.
//
// While every token keeps being rejected, an auth circuit breaker (see
// enterInfo) fails calls immediately instead of spending one rate-limited
// request per symbol: a full scan over thousands of symbols would
// otherwise crawl for minutes without a single result.
func (c *Client) doInfo(ctx context.Context, method, path, token string, body, out any) error {
	if err := c.enterInfo(); err != nil {
		return err
	}
	err := c.doInfoOnce(ctx, method, path, token, body, out)
	if err == nil {
		c.clearAuthFailure()
		return nil
	}
	if !isTokenRejected(err) {
		return err
	}
	fresh, refreshErr := c.refreshRejectedToken(ctx, token)
	if refreshErr != nil {
		if !errors.Is(refreshErr, errReissueThrottled) {
			slog.Error("marketdata: token reissue after rejection failed", "path", path, "error", refreshErr)
		}
		c.tripAuthFailure(err)
		return err
	}
	err = c.doInfoOnce(ctx, method, path, fresh, body, out)
	switch {
	case err == nil:
		c.clearAuthFailure()
	case isTokenRejected(err):
		slog.Error("marketdata: freshly issued token is also rejected; kabu station accepts /token but refuses information APIs",
			"path", path, "error", err)
		c.tripAuthFailure(err)
	}
	return err
}

// authBreakerCooldown is how long information-API calls fail fast after a
// freshly issued token was still rejected, before one call probes again.
const authBreakerCooldown = 30 * time.Second

// enterInfo gates an information-API call on the auth circuit breaker. It
// returns the recorded rejection while the breaker is open. Once the
// cooldown has elapsed it lets exactly one caller through as the probe
// (re-arming the cooldown for everyone else); a successful call closes the
// breaker (clearAuthFailure).
func (c *Client) enterInfo() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.authFailure == nil {
		return nil
	}
	now := c.limiter.Clock().Now()
	if now.Before(c.authRetryAt) {
		return c.authFailure
	}
	c.authRetryAt = now.Add(authBreakerCooldown)
	return nil
}

func (c *Client) tripAuthFailure(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.authFailure = err
	c.authRetryAt = c.limiter.Clock().Now().Add(authBreakerCooldown)
}

func (c *Client) clearAuthFailure() {
	c.mu.Lock()
	c.authFailure = nil
	c.mu.Unlock()
}

// refreshRejectedToken returns a token newer than rejected. Concurrent
// callers that all saw the same rejected token share one /token call: the
// first reissues, the others find the already replaced token. Reissues are
// at least minReactiveReissueInterval apart, and a reissue that returns the
// rejected token again is reported as an error (retrying cannot help).
func (c *Client) refreshRejectedToken(ctx context.Context, rejected string) (string, error) {
	c.reissueMu.Lock()
	defer c.reissueMu.Unlock()

	if current, ok := c.Token(); ok && current != rejected {
		return current, nil
	}
	now := c.limiter.Clock().Now()
	if !c.lastReissue.IsZero() && now.Sub(c.lastReissue) < minReactiveReissueInterval {
		return "", errReissueThrottled
	}
	c.lastReissue = now

	slog.Warn("marketdata: token rejected by kabu station, reissuing")
	fresh, err := c.IssueToken(ctx)
	if err != nil {
		return "", err
	}
	if fresh == rejected {
		return "", errors.New("marketdata: reissued token equals the rejected token")
	}
	return fresh, nil
}
