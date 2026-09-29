package policy

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository"
	"github.com/ousiassllc/pitha-trador/internal/service/jev"
)

// SignalExecutor acts on a persisted trade signal Policy Engine approved
// (RiskPassed LONG/SHORT): internal/bootstrap passes a Paper Trading
// adapter over internal/service/execution.Engine (issue #49). snap is
// the market_snapshots row the signal was evaluated against, supplying
// the entry reference price.
type SignalExecutor interface {
	ExecuteSignal(ctx context.Context, signal domain.TradeSignal, snap domain.Snapshot) error
}

// Handler connects Jev Trader and Engine to the jev-trader queue
// (repository.JobQueueJevTrader): HandleJob loads the instrument's latest
// market state, calls Jev Trader, then Engine, persisting a trade_signals
// row for every outcome (FR-POLICY-5) - including a NONE row when the Jev
// Trader call itself fails (FR-POLICY-3 "API異常"). This differs from
// jev.Trader.Evaluate's own "persist nothing on failure" contract for
// jev_decisions (overview.md §6 "継続失敗でnew entry停止"): a
// trade_signals row is a decision log, not a Jev API I/O log, so Policy
// Engine still records that no signal was generated and why.
type Handler struct {
	trader    *jev.Trader
	snapshots *repository.SnapshotRepository
	engine    *Engine
	executor  SignalExecutor
	calib     CalibrationSource // optional, see WithCalibration
}

// NewHandler returns a Handler that evaluates jev-trader queue jobs via
// trader and engine, reading each instrument's latest market_snapshots
// row via snapshots, and hands every approved signal to executor. A nil
// executor only records signals (no order is ever placed).
func NewHandler(trader *jev.Trader, snapshots *repository.SnapshotRepository, engine *Engine, executor SignalExecutor, opts ...HandlerOption) *Handler {
	h := &Handler{trader: trader, snapshots: snapshots, engine: engine, executor: executor}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// HandleJob processes one jev-trader queue job (jev.ScoutJobPayload: it
// identifies which instrument to evaluate, mirroring
// internal/service/jev.Scout's own convention of enqueuing that same
// payload shape onto this queue). Its signature matches
// internal/service/scheduler.Handler, so it can be registered directly
// once the Scheduler wiring itself is built (a later sub-scope):
// scheduler.RegisterHandler(repository.JobQueueJevTrader, handler.HandleJob).
//
// Turnover5mJPY is the snapshot's trailing 5-minute turnover
// (Feature.Turnover5m: a difference of cumulative session turnover, see
// featureengine.TurnoverOverWindow), so FR-POLICY-3's "板が薄い" check
// applies on this path; nil (insufficient history) skips it. Calibrated
// comes from the CalibrationSource set via WithCalibration (see
// calibrated); without one every decision counts as calibrated.
func (h *Handler) HandleJob(ctx context.Context, job repository.Job) error {
	var payload jev.ScoutJobPayload
	if err := json.Unmarshal([]byte(job.PayloadJSON), &payload); err != nil {
		return fmt.Errorf("policy: decode jev-trader job payload: %w", err)
	}

	snapshots, err := h.snapshots.ListByInstrument(ctx, payload.InstrumentID, 1)
	if err != nil {
		return fmt.Errorf("policy: load latest snapshot for %q: %w", payload.Symbol, err)
	}
	if len(snapshots) == 0 {
		return fmt.Errorf("policy: no market snapshot recorded yet for %q", payload.Symbol)
	}
	snap := snapshots[0]

	decision, err := h.trader.Evaluate(ctx, payload.InstrumentID, jev.StateFromSnapshot(snap))
	if err != nil {
		if _, sigErr := h.engine.Evaluate(ctx, Input{
			InstrumentID: payload.InstrumentID,
			Symbol:       payload.Symbol,
			Timestamp:    snap.Timestamp,
			APIErr:       err,
		}); sigErr != nil {
			return fmt.Errorf("policy: record api-error trade signal for %q: %w (trader error: %v)", payload.Symbol, sigErr, err)
		}
		return err
	}

	calibrated, err := h.calibrated(ctx, decision)
	if err != nil {
		return fmt.Errorf("policy: calibration check for %q: %w", payload.Symbol, err)
	}

	signal, err := h.engine.Evaluate(ctx, Input{
		InstrumentID:        payload.InstrumentID,
		Symbol:              payload.Symbol,
		Timestamp:           decision.Timestamp,
		Decision:            &decision,
		EntryPriceReference: &snap.Price,
		SpreadBps:           snap.SpreadBps,
		Turnover5mJPY:       snap.Feature.Turnover5m,
		Calibrated:          calibrated,
	})
	if err != nil {
		return fmt.Errorf("policy: evaluate trade signal for %q: %w", payload.Symbol, err)
	}
	if h.executor == nil || !signal.RiskPassed || signal.Direction == domain.JevDirectionNone {
		return nil
	}
	if err := h.executor.ExecuteSignal(ctx, signal, snap); err != nil {
		return fmt.Errorf("policy: execute trade signal for %q: %w", payload.Symbol, err)
	}
	return nil
}
