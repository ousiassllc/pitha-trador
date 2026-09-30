package updater

import (
	"context"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// defaultMinIdleAfterOrder is how long after the most recently submitted
// paper_orders row (entry or exit) SafeGate.SafeToUpdate requires before
// allowing an update, when Config.MinIdleAfterOrder is zero (issue #65's
// "直近の発注...から一定時間(例: 5分)経過している" example).
const defaultMinIdleAfterOrder = 5 * time.Minute

// PositionCounter reports how many positions are currently open, for
// SafeToUpdate's "ポジション数0" gate. This mirrors
// internal/service/risk.PortfolioProvider.OpenPositionCount's exact
// signature (package doc.go's layer rule forbids importing that package
// directly); *repoportfolio.Provider - the same instance
// internal/bootstrap already wires into risk.Engine - implements it
// directly, so position aggregation is never re-implemented here.
type PositionCounter interface {
	OpenPositionCount(ctx context.Context) (int, error)
}

// SystemStateReader reports the current Kill-Switch/pause state, for
// SafeToUpdate's "Kill Switch非発動" gate. This mirrors
// internal/service/risk.Engine.State's exact signature (same layer-rule
// reason as PositionCounter above); *risk.Engine implements it directly.
type SystemStateReader interface {
	State(ctx context.Context) (domain.SystemState, []domain.KillSwitchEvent, error)
}

// OrderLister reports the most recently submitted paper_orders rows, for
// SafeToUpdate's "直近の発注から一定時間経過" gate. This mirrors
// internal/service/execution.Engine.ListOrders' exact signature (same
// layer-rule reason as PositionCounter above); *execution.Engine
// implements it directly. execution.Engine.Close (an Exit) submits a
// paper_orders row exactly like Enter does for an Entry, so an empty
// status filter's single most-recent row already covers both "Entry" and
// "Exit" from the issue's gate description.
type OrderLister interface {
	ListOrders(ctx context.Context, status string, limit int) ([]domain.PaperOrder, error)
}

// SafeGate bundles issue #65's three-part safety gate - ポジション数0/
// Kill Switch非発動/直近発注から一定時間経過 - each read from the same
// instances internal/bootstrap already wires into risk.Engine/
// execution.Engine.
type SafeGate struct {
	Positions PositionCounter
	State     SystemStateReader
	Orders    OrderLister

	// MinIdleAfterOrder overrides defaultMinIdleAfterOrder (5 minutes)
	// when non-zero.
	MinIdleAfterOrder time.Duration
	// Now overrides time.Now (tests only); nil uses time.Now.
	Now func() time.Time
}

func (g SafeGate) now() time.Time {
	if g.Now != nil {
		return g.Now()
	}
	return time.Now()
}

func (g SafeGate) minIdle() time.Duration {
	if g.MinIdleAfterOrder > 0 {
		return g.MinIdleAfterOrder
	}
	return defaultMinIdleAfterOrder
}

// BlockKind identifies which SafeGate condition held an installation back,
// so the UI can name it (issue #241). The zero value means "not blocked".
type BlockKind string

const (
	// BlockOpenPositions: at least one open position exists.
	BlockOpenPositions BlockKind = "open_positions"
	// BlockKillSwitch: the Kill Switch is active.
	BlockKillSwitch BlockKind = "kill_switch"
	// BlockRecentOrder: an order was submitted too recently.
	BlockRecentOrder BlockKind = "recent_order"
	// BlockCheckFailed: a gate's own lookup failed, so safety could not be
	// established (fail closed).
	BlockCheckFailed BlockKind = "check_failed"
)

// BlockReason is why SafeToUpdate rejected an installation: Kind for the
// UI, Detail (English, with counts/durations) for the log.
type BlockReason struct {
	Kind   BlockKind
	Detail string
}

// SafeToUpdate reports whether every gate passes, and - when it does not
// - the reason identifying the first failing gate (checked in the order:
// open positions, Kill Switch, then a too-recent order).
func (g SafeGate) SafeToUpdate(ctx context.Context) (bool, BlockReason) {
	count, err := g.Positions.OpenPositionCount(ctx)
	if err != nil {
		return false, BlockReason{BlockCheckFailed, fmt.Sprintf("open position count check failed: %v", err)}
	}
	if count > 0 {
		return false, BlockReason{BlockOpenPositions, fmt.Sprintf("open positions: count=%d", count)}
	}

	state, events, err := g.State.State(ctx)
	if err != nil {
		return false, BlockReason{BlockCheckFailed, fmt.Sprintf("system state check failed: %v", err)}
	}
	if state == domain.SystemStateKilled {
		return false, BlockReason{BlockKillSwitch, fmt.Sprintf("kill switch active: %d unresolved event(s)", len(events))}
	}

	orders, err := g.Orders.ListOrders(ctx, "", 1)
	if err != nil {
		return false, BlockReason{BlockCheckFailed, fmt.Sprintf("recent order check failed: %v", err)}
	}
	if len(orders) > 0 {
		if elapsed := g.now().Sub(orders[0].SubmittedAt); elapsed < g.minIdle() {
			return false, BlockReason{BlockRecentOrder, fmt.Sprintf("recent order: submitted %s ago, need %s", elapsed, g.minIdle())}
		}
	}

	return true, BlockReason{}
}
