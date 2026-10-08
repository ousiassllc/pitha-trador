// Package heldposition is FR-SCHED-4's 保有ポジション監視・Exit評価 loop
// (issue #156): every 5〜15 seconds it re-prices each open Paper position
// from the latest broker quote and runs Execution's exit evaluation,
// so Stop Loss / Trailing Stop / Take Profit / 最大保有時間 / 引け前強制決済
// are judged on that cadence instead of only on the 60-second full scan's
// 1-minute bar.
//
// The evaluation is pure code (no Jev call, FR-EXIT-3) and runs only inside
// a trading session: off-hours no quote is fetched (non-functional.md §3).
// It is deliberately not gated by the Kill Switch: exits must keep working
// while new entries are halted.
//
// It is composition-root glue (it joins the broker quote feed and execution), so it
// lives under internal/bootstrap rather than internal/service.
package heldposition

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/safego"
	"github.com/ousiassllc/pitha-trador/internal/service/broker"
	"github.com/ousiassllc/pitha-trador/internal/service/execution"
	"github.com/ousiassllc/pitha-trador/internal/service/featureengine"
)

// DefaultInterval is the tick used when config/strategy.yaml's
// scan.held_position_interval_seconds_min/max are unset: the upper end of
// FR-SCHED-4's 5〜15秒.
const DefaultInterval = 15 * time.Second

// Positions lists the currently open positions (trading.PositionRepository).
type Positions interface {
	ListOpen(ctx context.Context) ([]domain.Position, error)
}

// Quotes returns the latest quote of a symbol (broker.StreamFeed): the
// streamed quote when it is fresh, a REST poll only as the thin supplement
// when it is stale or missing (issue #709). Held symbols are always in the
// stream's watch list, so the 5〜15秒 loop does not add a REST poll per
// position per tick.
type Quotes interface {
	Latest(ctx context.Context, symbol string) (broker.Quote, error)
}

// Exits evaluates exit conditions for one price update (execution.Engine).
// Engine.OnSnapshot serialises calls, so this monitor and the per-bar
// market-data job may both price the same instrument: the position is
// closed exactly once.
type Exits interface {
	OnSnapshot(ctx context.Context, snap domain.Snapshot) (execution.SnapshotResult, error)
}

// Monitor evaluates every open position once per Cycle.
type Monitor struct {
	Positions Positions
	Quotes    Quotes
	Exits     Exits
	// Open reports whether t is inside a trading session.
	Open func(t time.Time) bool
	// Now defaults to time.Now.
	Now func() time.Time
}

func (m Monitor) now() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now()
}

// Cycle evaluates every open position once and returns how many were
// evaluated. A failure or panic on one position (board fetch, DB) is
// logged and does not stop the others; ListOpen's own failure is returned.
func (m Monitor) Cycle(ctx context.Context) (int, error) {
	now := m.now()
	if !m.Open(now) {
		return 0, nil
	}
	positions, err := m.Positions.ListOpen(ctx)
	if err != nil {
		return 0, fmt.Errorf("heldposition: list open positions: %w", err)
	}
	evaluated := 0
	for _, p := range positions {
		// A panic on one position is logged and skipped so the positions
		// after it are still evaluated (FR-SCHED-6): otherwise a
		// deterministic panic would starve them of exit judgement every cycle.
		var ok bool
		if safego.Run("held position monitor: "+p.Symbol, func() { ok = m.evaluate(ctx, p, now) }) {
			continue
		}
		if ok {
			evaluated++
		}
	}
	return evaluated, nil
}

// evaluate prices one position from its quote and runs the exit
// evaluation, reporting whether the position was evaluated.
func (m Monitor) evaluate(ctx context.Context, p domain.Position, now time.Time) bool {
	q, err := m.Quotes.Latest(ctx, p.Symbol)
	if err != nil {
		slog.Warn("heldposition: board fetch failed", "symbol", p.Symbol, "error", err)
		return false
	}
	if !q.HasPrice() {
		// 寄り付き前・未約定: price 0 would stop out / close at a bogus -100%.
		slog.Warn("heldposition: board has no current price, skipping", "symbol", p.Symbol, "current_price", q.Price)
		return false
	}
	// A transient snapshot (never persisted: market_snapshots stays
	// the 1-minute bar series): the price, session VWAP and the quote
	// the exit conditions and the fill model read. The quote comes from the
	// same broker.Quote as the market-data job's persisted bar so an exit
	// fills at the same price whichever path fires first.
	snap := domain.Snapshot{
		InstrumentID: p.InstrumentID, Symbol: p.Symbol, Timestamp: now.UTC(),
		Price: q.Price, Bid: q.Bid, Ask: q.Ask, SpreadBps: featureengine.SpreadBps(q.Bid, q.Ask),
		Feature: domain.Feature{VWAP: q.VWAP},
	}
	if _, err := m.Exits.OnSnapshot(ctx, snap); err != nil {
		slog.Error("heldposition: exit evaluation failed", "symbol", p.Symbol, "position_id", p.ID, "error", err)
		return false
	}
	return true
}

// Run calls Cycle every wait() (a random duration in [min, max), or min
// when max <= min; DefaultInterval when min <= 0) until ctx is done.
func (m Monitor) Run(ctx context.Context, min, max time.Duration) {
	if min <= 0 {
		min, max = DefaultInterval, DefaultInterval
	}
	wait := func() time.Duration {
		if max > min {
			return min + time.Duration(rand.Int64N(int64(max-min))) //nolint:gosec // G404: poll-interval jitter, not security-sensitive
		}
		return min
	}
	safego.Loop(ctx, "held position monitor", wait, func(ctx context.Context) error {
		_, err := m.Cycle(ctx)
		return err
	})
}
