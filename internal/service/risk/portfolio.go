package risk

import (
	"context"
	"time"
)

// PortfolioProvider supplies the position/order-derived state Engine's
// FR-RISK-1 exposure/loss-count limits and FR-RISK-2 daily-loss/
// consecutive-loss Kill Switch triggers need. Paper/Live Position/Order
// tracking is a later sub-scope (Paper Trading Execution, doc.go);
// PortfolioProvider is the extension point a real implementation
// substitutes without changing Engine's own limit-checking logic (mirrors
// internal/service/policy.RiskChecker's role for Policy Engine before
// this package existed).
type PortfolioProvider interface {
	// OpenPositionCount returns how many positions are currently open,
	// for FR-RISK-1's max_open_positions.
	OpenPositionCount(ctx context.Context) (int, error)

	// TotalExposurePct returns every open position's combined notional as
	// a percentage of account equity, for FR-RISK-1's
	// max_total_exposure_pct.
	TotalExposurePct(ctx context.Context) (float64, error)

	// SymbolExposurePct returns instrumentID's own open-position notional
	// as a percentage of account equity (0 if it has no open position),
	// for FR-RISK-1's max_position_per_symbol_pct.
	SymbolExposurePct(ctx context.Context, instrumentID int64) (float64, error)

	// DailyLossPct returns today's realized+unrealized loss as a
	// percentage of account equity (0 or negative when there is no net
	// loss), for FR-RISK-1's max_daily_loss_pct and FR-RISK-2's
	// daily_loss_limit Kill Switch trigger.
	DailyLossPct(ctx context.Context) (float64, error)

	// ConsecutiveLosses returns how many losing trades were closed most
	// recently in a row (reset to 0 by the next winning trade), for
	// FR-RISK-1's max_consecutive_losses and FR-RISK-2's
	// consecutive_losses Kill Switch trigger.
	ConsecutiveLosses(ctx context.Context) (int, error)

	// LastLossAt returns the closed_at timestamp of the most recent
	// losing trade, or the zero time if there has never been one, for the
	// cooldown_after_loss_minutes gate (functional.md §4.7's table; see
	// engine.go's Check doc comment for why this is not a
	// kill_switch_events row).
	LastLossAt(ctx context.Context) (time.Time, error)
}

// ZeroPortfolioProvider is the placeholder PortfolioProvider used until a
// later sub-scope wires real Position/Order repositories in: every query
// reports "no open positions, no losses" - which is factually true today
// (Paper Trading Execution has not started opening positions yet), so
// FR-RISK-1's exposure/loss-count limits simply never reject and
// FR-RISK-2's daily-loss/consecutive-loss triggers simply never fire
// until real portfolio data exists.
type ZeroPortfolioProvider struct{}

func (ZeroPortfolioProvider) OpenPositionCount(context.Context) (int, error) { return 0, nil }

func (ZeroPortfolioProvider) TotalExposurePct(context.Context) (float64, error) { return 0, nil }

func (ZeroPortfolioProvider) SymbolExposurePct(context.Context, int64) (float64, error) {
	return 0, nil
}

func (ZeroPortfolioProvider) DailyLossPct(context.Context) (float64, error) { return 0, nil }

func (ZeroPortfolioProvider) ConsecutiveLosses(context.Context) (int, error) { return 0, nil }

func (ZeroPortfolioProvider) LastLossAt(context.Context) (time.Time, error) {
	return time.Time{}, nil
}
