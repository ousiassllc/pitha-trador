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
func (c *Client) doInfo(ctx context.Context, method, path, token string, body, out any) error {
	err := c.doInfoOnce(ctx, method, path, token, body, out)
	if !isTokenRejected(err) {
		return err
	}
	fresh, refreshErr := c.refreshRejectedToken(ctx, token)
	if refreshErr != nil {
		if !errors.Is(refreshErr, errReissueThrottled) {
			slog.Error("marketdata: token reissue after rejection failed", "path", path, "error", refreshErr)
		}
		return err
	}
	return c.doInfoOnce(ctx, method, path, fresh, body, out)
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
