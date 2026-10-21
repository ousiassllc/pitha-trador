// Package proposalexec is the application's side of the Paper results MCP's
// order proposals (issue #828, parent #826): it takes the proposals the MCP
// recorded (status received), puts each through the same Risk check the Paper
// pipeline uses, and places the Paper order or records why it did not. It
// also cancels what the MCP asked to cancel, lets limit orders that did not
// fill expire, and picks up proposals a restart left in processing.
//
// The MCP never places or cancels anything itself: it only records a proposal
// and, later, a request to cancel it. Everything that touches trade_signals,
// paper_orders and positions happens here.
//
// It is composition-root glue between internal/service/risk and execution, so
// it lives under internal/bootstrap (like paperexec).
package proposalexec

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/domain/orderproposal"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/repository/proposalstore"
	"github.com/ousiassllc/pitha-trador/internal/repository/trading"
	"github.com/ousiassllc/pitha-trador/internal/service/execution"
	"github.com/ousiassllc/pitha-trador/internal/service/marketcalendar"
	"github.com/ousiassllc/pitha-trador/internal/service/risk"
)

// DefaultInterval is how often Run looks for work: a proposal is picked up,
// and a waiting one retried, within this long.
const DefaultInterval = 5 * time.Second

// Fixed reason codes of order_proposals.status_reason and of the events. The
// Risk Engine's own codes (kill_switch_active, system_paused, bucket_disabled,
// bucket_budget_exceeded, ...) are recorded as they are.
const (
	ReasonExpired            = "expired"
	ReasonCancelRequested    = "cancel_requested"
	ReasonUnknownSymbol      = "unknown_symbol"
	ReasonInactiveSymbol     = "inactive_symbol"
	ReasonBucketMismatch     = "bucket_mismatch"
	ReasonOutsideSession     = "outside_trading_session"
	ReasonStaleQuote         = "stale_quote"
	ReasonSizeZero           = "size_zero"
	ReasonMaxTradeLoss       = "max_trade_loss"
	ReasonQuantityReduced    = "quantity_reduced"
	ReasonPositionOpen       = "position_already_open"
	ReasonPendingOrder       = "pending_order_exists"
	ReasonSymbolInCooldown   = "symbol_in_cooldown"
	ReasonNoOpenPosition     = "no_open_position"
	ReasonOrderRejected      = "order_rejected"
	ReasonOrderCancelled     = "order_cancelled"
	ReasonProcessingError    = "processing_error"
	ReasonRecoveredAfterStop = "recovered_after_restart"
)

// Risk is what the processor needs of risk.Engine. CheckThenEnter judges the
// entry and runs enter under the lock every Paper entry shares (issue #839).
type Risk interface {
	CheckThenEnter(ctx context.Context, instrumentID int64, direction string, enter func(size risk.EntrySize) error) (passed bool, reason string, err error)
	EffectiveLimits(ctx context.Context) (config.RiskLimits, error)
}

// Execution is what the processor needs of execution.Engine.
type Execution interface {
	Enter(ctx context.Context, req execution.EntryRequest) (execution.EntryResult, error)
	CloseAtMarket(ctx context.Context, positionID int64, reason string, now time.Time) (domain.Position, error)
	CancelPendingEntry(ctx context.Context, orderID int64) error
	EntrySessionOpenNow() bool
	Config() execution.Config
}

// Processor advances order proposals. The zero value is not usable; set every
// field.
type Processor struct {
	Proposals   *proposalstore.Repository
	Instruments *market.InstrumentRepository
	Snapshots   *market.SnapshotRepository
	Signals     *trading.SignalRepository
	Orders      *trading.OrderRepository
	Positions   *trading.PositionRepository
	Risk        Risk
	Execution   Execution
	// SnapshotMaxAge is how old the symbol's latest bar may be (against the
	// wall clock, in a session) before the quote counts as stale: the same
	// limit Policy and Jev use.
	SnapshotMaxAge time.Duration
}

// now is Execution's clock, so a session decision and the order it leads to
// see the same time.
func (p Processor) now() time.Time { return p.Execution.Config().Now() }

// Run calls Cycle every interval until ctx is done.
func (p Processor) Run(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = DefaultInterval
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		if _, err := p.Cycle(ctx); err != nil && ctx.Err() == nil {
			slog.ErrorContext(ctx, "proposalexec: cycle failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Cycle advances every proposal that has work (oldest first) once and returns
// how many it looked at. A failure on one proposal is logged and does not stop
// the others.
func (p Processor) Cycle(ctx context.Context) (int, error) {
	active, err := p.Proposals.Active(ctx)
	if err != nil {
		return 0, err
	}
	for _, pr := range active {
		if ctx.Err() != nil {
			break
		}
		p.handle(ctx, pr)
	}
	return len(active), nil
}

// handle advances one proposal; neither an error nor a panic leaves it.
func (p Processor) handle(ctx context.Context, pr orderproposal.Proposal) {
	defer func() {
		if r := recover(); r != nil {
			slog.ErrorContext(ctx, "proposalexec: panic while processing a proposal", "proposal_id", pr.ID, "panic", r)
		}
	}()
	if err := p.advance(ctx, pr); err != nil {
		slog.ErrorContext(ctx, "proposalexec: proposal not advanced", "proposal_id", pr.ID, "status", pr.Status, "error", err)
	}
}

// advance is the first two steps of the flow (expiry, then a cancellation
// request), then claims the proposal (processing) and runs it.
func (p Processor) advance(ctx context.Context, pr orderproposal.Proposal) error {
	now := p.now()
	if pr.Status == orderproposal.StatusSubmitted {
		return p.syncSubmitted(ctx, pr, now)
	}
	switch {
	case !now.Before(pr.ExpiresAt):
		return p.settle(ctx, pr, orderproposal.StatusExpired, ReasonExpired, proposalstore.Links{})
	case pr.CancelRequested():
		return p.settle(ctx, pr, orderproposal.StatusCancelled, ReasonCancelRequested, proposalstore.Links{})
	case pr.Status == orderproposal.StatusWaitingSession && !p.tradable(pr.Action, now):
		return nil // still waiting: nothing to record
	}
	if err := p.Proposals.Transition(ctx, pr.ID, pr.Status, proposalstore.Change{ToStatus: orderproposal.StatusProcessing}); err != nil {
		if errors.Is(err, proposalstore.ErrStatusChanged) {
			return nil
		}
		return err
	}
	pr.Status = orderproposal.StatusProcessing
	return p.run(ctx, pr, now)
}

// tradable reports whether a proposal of action can be placed now: a new entry
// needs the entry session, a close only needs the market to be executing.
func (p Processor) tradable(action string, now time.Time) bool {
	if action == orderproposal.ActionClose {
		cal := p.Execution.Config().Calendar
		return cal == nil || cal.PhaseAt(now) != marketcalendar.PhaseClosed
	}
	return p.Execution.EntrySessionOpenNow()
}

// run is the rest of the flow for a claimed proposal (status processing).
func (p Processor) run(ctx context.Context, pr orderproposal.Proposal, now time.Time) error {
	inst, reason, err := p.instrument(ctx, pr)
	if err != nil {
		return p.fail(ctx, pr, proposalstore.Links{}, err)
	}
	if reason != "" {
		return p.settle(ctx, pr, orderproposal.StatusRejected, reason, instrumentLinks(inst))
	}
	if pr.Action == orderproposal.ActionClose {
		return p.closePosition(ctx, pr, inst, now)
	}
	return p.openLong(ctx, pr, inst, now)
}

// instrument is step 3: the symbol exists and its asset class is the
// proposal's bucket. A new entry also needs an active stock (an index is no
// symbol to buy); a close does not: a delisted or inactive symbol may still be
// held and must stay closable. A non-empty reason is the rejection.
func (p Processor) instrument(ctx context.Context, pr orderproposal.Proposal) (domain.Instrument, string, error) {
	inst, err := p.Instruments.GetBySymbol(ctx, pr.Symbol)
	if errors.Is(err, market.ErrInstrumentNotFound) {
		return domain.Instrument{}, ReasonUnknownSymbol, nil
	}
	if err != nil {
		return domain.Instrument{}, "", fmt.Errorf("proposalexec: look up symbol %q: %w", pr.Symbol, err)
	}
	if pr.Action == orderproposal.ActionOpenLong {
		switch {
		case inst.Kind != domain.InstrumentKindStock:
			return inst, ReasonUnknownSymbol, nil
		case !inst.IsActive:
			return inst, ReasonInactiveSymbol, nil
		}
	}
	if !bucketMatches(inst, pr.Bucket) {
		return inst, ReasonBucketMismatch, nil
	}
	return inst, "", nil
}

// settle moves the proposal from its current status to a new one with reason
// (and links). Losing the race (the status changed meanwhile) is not an error.
func (p Processor) settle(ctx context.Context, pr orderproposal.Proposal, to, reason string, links proposalstore.Links) error {
	err := p.Proposals.Transition(ctx, pr.ID, pr.Status, proposalstore.Change{ToStatus: to, Reason: reason, Links: links})
	if errors.Is(err, proposalstore.ErrStatusChanged) {
		return nil
	}
	return err
}

// fail records an unexpected error as status failed with a fixed code; the
// error itself goes to the log, never into the record.
func (p Processor) fail(ctx context.Context, pr orderproposal.Proposal, links proposalstore.Links, cause error) error {
	slog.ErrorContext(ctx, "proposalexec: proposal failed", "proposal_id", pr.ID, "symbol", pr.Symbol, "error", cause)
	return p.settle(ctx, pr, orderproposal.StatusFailed, ReasonProcessingError, links)
}
