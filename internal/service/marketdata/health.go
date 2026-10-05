package marketdata

import (
	"context"
	"errors"
	"net/http"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// unhealthyAfterBoardFailures is how many consecutive GetBoard failures
// countsAsFeedFailure accepts (no token, transport error, 5xx, auth/token
// 4xx, undecodable body) Healthy treats as FR-RISK-2's 市場データ停止.
const unhealthyAfterBoardFailures = 5

// codeAPIKeyMismatch is kabuステーションAPI 4001009 (APIキー不一致). With
// the 4001007/4001008/4001013/4001017 codes in tokenstatus.go it marks an
// auth/session error that makes the token unusable for every symbol, as
// opposed to a per-symbol request error such as 4002001 (銘柄が見つからない).
const codeAPIKeyMismatch = 4001009

// countsAsFeedFailure reports whether a GetBoard error is evidence that the
// market data feed itself is down, as opposed to a problem with one
// symbol's request. Only the former extends the market_data_down streak:
// ErrNoToken, transport/decoding errors, HTTP 5xx and auth/token 4xx count;
// any other 4xx (4002001 unknown symbol, delisted or halted instruments, ...)
// does not, so a handful of invalid symbols cannot trip the Kill Switch
// while the feed is fine. 429/4001006 is handled before this (ErrRateLimited).
func countsAsFeedFailure(err error) bool {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return true
	}
	switch {
	case apiErr.StatusCode >= http.StatusInternalServerError:
		return true
	case apiErr.StatusCode == http.StatusUnauthorized, apiErr.StatusCode == http.StatusForbidden:
		return true
	}
	switch apiErr.Code {
	case codeNotLoggedIn, codeNotLoggedInV2, codeAPIKeyMismatch, codeAPIDisabled, codeBadPassword:
		return true
	}
	return apiErr.StatusCode < http.StatusBadRequest // body-level failure on an HTTP 200
}

// Healthy implements internal/service/risk.HealthChecker for the
// market_data_down Kill Switch (FR-RISK-2 trigger, FR-RISK-7 auto-resume
// check): the market data feed counts as stopped once
// unhealthyAfterBoardFailures consecutive feed-level GetBoard failures
// (see countsAsFeedFailure) have occurred, and
// as recovered by the first success after that.
func (c *Client) Healthy(context.Context) (bool, error) {
	return c.boardFailures.ConsecutiveFailures() < unhealthyAfterBoardFailures, nil
}

// BrokerFailures is the streak of consecutive kabuステーションAPI calls
// (any endpoint) that got an HTTP 5xx response, feeding FR-RISK-2's
// broker_api_error Kill Switch (risk.Config.BrokerAPIFailures). Only 5xx
// counts: a 4xx (unknown symbol, wrong API password, ...) is a request
// problem, not a Broker API fault, and a transport error is already
// covered by the market_data_down stop detection above.
func (c *Client) BrokerFailures() *domain.FailureStreak { return &c.brokerFailures }

// recordBrokerOutcome feeds one kabuステーションAPI call's outcome into
// the broker_api_error streak (see BrokerFailures).
func (c *Client) recordBrokerOutcome(err error) {
	var apiErr *APIError
	switch {
	case err == nil:
		c.brokerFailures.Succeed()
	case errors.As(err, &apiErr) && apiErr.StatusCode >= http.StatusInternalServerError:
		c.brokerFailures.Fail()
	}
}
