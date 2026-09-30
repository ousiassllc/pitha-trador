package risk

import (
	"context"
	"time"
)

// PortfolioProvider supplies the position/order-derived state Engine's
// FR-RISK-1 exposure/loss-count limits and FR-RISK-2 daily-loss/
// consecutive-loss Kill Switch triggers need. repoportfolio.Provider
// (internal/service/risk/repoportfolio) is the production implementation backed by the positions
// table; the interface keeps Engine's own limit-checking logic
// independent of how that state is stored.
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
	// daily_loss_limit Kill Switch trigger. Realized P&L counts only
	// positions closed after since (the manual-resume baseline,
	// baseline.go); a zero since counts all of today.
	DailyLossPct(ctx context.Context, since time.Time) (float64, error)

	// ConsecutiveLosses returns how many losing trades were closed most
	// recently in a row (reset to 0 by the next winning trade), for
	// FR-RISK-1's max_consecutive_losses and FR-RISK-2's
	// consecutive_losses Kill Switch trigger. Only positions closed after
	// since (the manual-resume baseline, baseline.go) are counted; a zero
	// since counts the whole history.
	ConsecutiveLosses(ctx context.Context, since time.Time) (int, error)

	// LastLossAt returns the closed_at timestamp of the most recent
	// losing trade closed after since (the manual-resume baseline), or
	// the zero time if there has been none, for the
	// cooldown_after_loss_minutes gate (functional.md §4.7's table; see
	// engine.go's Check doc comment for why this is not a
	// kill_switch_events row).
	LastLossAt(ctx context.Context, since time.Time) (time.Time, error)
}

// ZeroPortfolioProvider is NewEngine's default PortfolioProvider when
// Config.Portfolio is nil (tests, or a caller with no positions table):
// every query reports "no open positions, no losses", so FR-RISK-1's
// exposure/loss-count limits never reject and FR-RISK-2's daily-loss/
// consecutive-loss triggers never fire.
type ZeroPortfolioProvider struct{}

func (ZeroPortfolioProvider) OpenPositionCount(context.Context) (int, error) { return 0, nil }

func (ZeroPortfolioProvider) TotalExposurePct(context.Context) (float64, error) { return 0, nil }

func (ZeroPortfolioProvider) SymbolExposurePct(context.Context, int64) (float64, error) {
	return 0, nil
}

func (ZeroPortfolioProvider) DailyLossPct(context.Context, time.Time) (float64, error) { return 0, nil }

func (ZeroPortfolioProvider) ConsecutiveLosses(context.Context, time.Time) (int, error) {
	return 0, nil
}

func (ZeroPortfolioProvider) LastLossAt(context.Context, time.Time) (time.Time, error) {
	return time.Time{}, nil
}
