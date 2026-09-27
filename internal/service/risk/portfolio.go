package risk

import (
	"context"
	"slices"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository"
)

// PortfolioProvider supplies the position/order-derived state Engine's
// FR-RISK-1 exposure/loss-count limits and FR-RISK-2 daily-loss/
// consecutive-loss Kill Switch triggers need. RepositoryPortfolioProvider
// (below) is the production implementation backed by the positions
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

func (ZeroPortfolioProvider) DailyLossPct(context.Context) (float64, error) { return 0, nil }

func (ZeroPortfolioProvider) ConsecutiveLosses(context.Context) (int, error) { return 0, nil }

func (ZeroPortfolioProvider) LastLossAt(context.Context) (time.Time, error) {
	return time.Time{}, nil
}

// recentClosedPositionsLimit bounds how many of the most-recently-opened
// positions.List rows RepositoryPortfolioProvider.recentClosedPositions
// scans to find the most-recently-closed ones (List has no closed_at-
// ordered query of its own - ListClosedBetween needs a [start,end) range,
// not "most recent N" - so this reads a generously large recent window
// and filters/sorts client-side). 200 comfortably covers FR-RISK-1's
// max_consecutive_losses (Paper/Live: 4/3) and cooldown checks even on an
// unusually active trading day.
const recentClosedPositionsLimit = 200

// RepositoryPortfolioProvider is the real PortfolioProvider backed by
// internal/repository.PositionRepository (issue #48; positions/orders
// themselves start flowing once internal/service/execution.Engine,
// issue #49, begins calling PositionRepository.Open/Close - this type has
// no dependency on execution.Engine itself, only on the same
// PositionRepository it also writes through).
//
// TotalExposurePct/SymbolExposurePct/DailyLossPct all require an
// account-equity baseline ("percentage of account equity" - this
// interface's own doc comments) that no table, config file, or domain
// type in this codebase defines yet (docs/architecture/er.md has no
// accounts/equity concept; config/risk.yaml's own Paper/Live sections
// only carry percentage *limits*, never the denominator they are limits
// of). Introducing that denominator is a product decision (how much
// simulated capital does Paper Trading represent?) beyond this issue's
// "Position/Orderリポジトリとの接続" scope, so - exactly like
// ZeroPortfolioProvider above, whose own doc comment already anticipates
// exactly this staged rollout - these three methods keep returning 0
// (the safe "never reject on this specific check" default) until that
// decision is made and a real denominator exists to compute against.
type RepositoryPortfolioProvider struct {
	positions *repository.PositionRepository
}

// NewRepositoryPortfolioProvider returns a RepositoryPortfolioProvider
// backed by positions.
func NewRepositoryPortfolioProvider(positions *repository.PositionRepository) *RepositoryPortfolioProvider {
	return &RepositoryPortfolioProvider{positions: positions}
}

func (p *RepositoryPortfolioProvider) OpenPositionCount(ctx context.Context) (int, error) {
	open, err := p.positions.ListOpen(ctx)
	if err != nil {
		return 0, err
	}
	return len(open), nil
}

func (p *RepositoryPortfolioProvider) TotalExposurePct(context.Context) (float64, error) {
	return 0, nil // see type doc comment: no account-equity baseline exists yet
}

func (p *RepositoryPortfolioProvider) SymbolExposurePct(context.Context, int64) (float64, error) {
	return 0, nil // see type doc comment: no account-equity baseline exists yet
}

func (p *RepositoryPortfolioProvider) DailyLossPct(context.Context) (float64, error) {
	return 0, nil // see type doc comment: no account-equity baseline exists yet
}

// recentClosedPositions returns positions.List's recentClosedPositionsLimit
// most-recently-opened rows, filtered to closed ones only and sorted
// most-recently-closed first.
func (p *RepositoryPortfolioProvider) recentClosedPositions(ctx context.Context) ([]closedPosition, error) {
	rows, err := p.positions.List(ctx, recentClosedPositionsLimit)
	if err != nil {
		return nil, err
	}

	closed := make([]closedPosition, 0, len(rows))
	for _, row := range rows {
		if row.ClosedAt == nil || row.RealizedPnL == nil {
			continue
		}
		closed = append(closed, closedPosition{closedAt: *row.ClosedAt, realizedPnL: *row.RealizedPnL})
	}
	slices.SortFunc(closed, func(a, b closedPosition) int { return b.closedAt.Compare(a.closedAt) })
	return closed, nil
}

type closedPosition struct {
	closedAt    time.Time
	realizedPnL float64
}

func (p *RepositoryPortfolioProvider) ConsecutiveLosses(ctx context.Context) (int, error) {
	closed, err := p.recentClosedPositions(ctx)
	if err != nil {
		return 0, err
	}

	count := 0
	for _, pos := range closed {
		if pos.realizedPnL >= 0 {
			break
		}
		count++
	}
	return count, nil
}

func (p *RepositoryPortfolioProvider) LastLossAt(ctx context.Context) (time.Time, error) {
	closed, err := p.recentClosedPositions(ctx)
	if err != nil {
		return time.Time{}, err
	}

	for _, pos := range closed {
		if pos.realizedPnL < 0 {
			return pos.closedAt, nil
		}
	}
	return time.Time{}, nil
}
