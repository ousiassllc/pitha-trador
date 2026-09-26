package policy

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ousiassllc/pitha-trador/internal/repository"
	"github.com/ousiassllc/pitha-trador/internal/service/jev"
)

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
}

// NewHandler returns a Handler that evaluates jev-trader queue jobs via
// trader and engine, reading each instrument's latest market_snapshots
// row via snapshots.
func NewHandler(trader *jev.Trader, snapshots *repository.SnapshotRepository, engine *Engine) *Handler {
	return &Handler{trader: trader, snapshots: snapshots, engine: engine}
}

// HandleJob processes one jev-trader queue job (jev.ScoutJobPayload: it
// identifies which instrument to evaluate, mirroring
// internal/service/jev.Scout's own convention of enqueuing that same
// payload shape onto this queue). Its signature matches
// internal/service/scheduler.Handler, so it can be registered directly
// once the Scheduler wiring itself is built (a later sub-scope):
// scheduler.RegisterHandler(repository.JobQueueJevTrader, handler.HandleJob).
//
// Calibrated defaults to true: Calibration (functional.md §4.9) is a
// separate later sub-scope, so no real signal yet says an
// instrument/setup is excluded from it (FR-POLICY-3 "キャリブレーション
// 対象外"). Turnover5mJPY is left nil for the same reason
// internal/service/screener.Input's own doc comment gives: Feature
// Engine does not compute a trailing 5-minute turnover value yet, so
// FR-POLICY-3's "板が薄い" check does not yet apply via this real path
// (Engine.Decide's unit tests exercise it directly).
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

	if _, err := h.engine.Evaluate(ctx, Input{
		InstrumentID:        payload.InstrumentID,
		Symbol:              payload.Symbol,
		Timestamp:           decision.Timestamp,
		Decision:            &decision,
		EntryPriceReference: &snap.Price,
		SpreadBps:           snap.SpreadBps,
		Calibrated:          true,
	}); err != nil {
		return fmt.Errorf("policy: evaluate trade signal for %q: %w", payload.Symbol, err)
	}
	return nil
}
