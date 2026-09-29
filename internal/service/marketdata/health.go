package marketdata

import (
	"context"
	"errors"
	"net/http"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// unhealthyAfterBoardFailures is how many consecutive GetBoard failures
// (any cause: no token, transport error, non-200, undecodable body) Healthy
// treats as FR-RISK-2's 市場データ停止.
const unhealthyAfterBoardFailures = 5

// Healthy implements internal/service/risk.HealthChecker for the
// market_data_down Kill Switch (FR-RISK-2 trigger, FR-RISK-7 auto-resume
// check): the market data feed counts as stopped once
// unhealthyAfterBoardFailures consecutive GetBoard calls have failed, and
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
