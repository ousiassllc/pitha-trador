package broker

import (
	"context"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// Health is the adapter's failure signal for the Risk Engine's FR-RISK-2
// detectors.
type Health interface {
	// BoardFailures is the streak of consecutive feed-level 板取得 failures
	// (no session, transport error, 5xx, auth errors - not a per-symbol
	// request error): market_data_down.
	BoardFailures() *domain.FailureStreak
	// BrokerFailures is the streak of consecutive broker API calls that got
	// a server-side (HTTP 5xx) failure: broker_api_error
	// (risk.Config.BrokerAPIFailures).
	BrokerFailures() *domain.FailureStreak
}

// UnhealthyAfterBoardFailures is how many consecutive feed-level 板取得
// failures make MarketDataChecker report the market data feed as stopped
// (FR-RISK-2 市場データ停止).
const UnhealthyAfterBoardFailures = 5

// MarketDataChecker implements risk.HealthChecker for the market_data_down
// Kill Switch (FR-RISK-2 trigger, FR-RISK-7 auto-resume check): the feed
// counts as stopped once UnhealthyAfterBoardFailures consecutive failures
// have occurred, and as recovered by the first success after that.
type MarketDataChecker struct{ Health Health }

// Healthy reports whether the market data feed is up.
func (c MarketDataChecker) Healthy(context.Context) (bool, error) {
	return c.Health.BoardFailures().ConsecutiveFailures() < UnhealthyAfterBoardFailures, nil
}
