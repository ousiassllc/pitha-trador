package execution_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/execution"
)

func TestEngine_State_DefaultsWhenNoDataYet(t *testing.T) {
	te := newTestEngine(t, execution.Config{})

	state, err := te.engine.State(context.Background(), "7203")
	if err != nil {
		t.Fatalf("State: %v", err)
	}
	if state.Symbol != "7203" {
		t.Fatalf("State().Symbol = %q, want %q", state.Symbol, "7203")
	}
	if state.LastSignal != domain.JevDirectionNone {
		t.Fatalf("State().LastSignal = %q, want %q (no signal yet)", state.LastSignal, domain.JevDirectionNone)
	}
	if state.LastPrice != 0 || state.LastScanAt != nil {
		t.Fatalf("State() = %+v, want zero LastPrice/nil LastScanAt (no snapshot yet)", state)
	}
	if state.Position != nil {
		t.Fatalf("State().Position = %+v, want nil (no open position)", state.Position)
	}
	if state.CooldownUntil != nil {
		t.Fatalf("State().CooldownUntil = %v, want nil (not in cooldown)", state.CooldownUntil)
	}
}

func TestEngine_State_UnknownSymbol(t *testing.T) {
	te := newTestEngine(t, execution.Config{})

	_, err := te.engine.State(context.Background(), "9999")
	if !errors.Is(err, execution.ErrInstrumentUnknown) {
		t.Fatalf("State(unknown symbol) error = %v, want ErrInstrumentUnknown", err)
	}
}

func TestEngine_State_ReflectsLatestSnapshotDecisionsSignalAndPosition(t *testing.T) {
	te := newTestEngine(t, execution.Config{})
	ctx := context.Background()
	now := time.Date(2026, 9, 27, 9, 31, 0, 0, time.UTC)

	if _, err := te.snapshots.Insert(ctx, domain.Snapshot{
		InstrumentID: te.instrument.ID, Symbol: "7203", Timestamp: now, Price: 2105.0,
		Volume: 1000, Turnover: 2105000, Feature: domain.Feature{VWAP: 2100.0},
	}); err != nil {
		t.Fatalf("seed snapshot: %v", err)
	}

	decisions := te.decisions
	scoutAt := now.Add(-2 * time.Minute)
	traderAt := now.Add(-1 * time.Minute)
	if _, err := decisions.Insert(ctx, domain.JevDecision{
		InstrumentID: te.instrument.ID, Symbol: "7203", Timestamp: traderAt,
		DecisionType: domain.JevDecisionTypeTrader, StateHash: "h1", StateJSON: "{}",
		QuestionVersion: "v1", ResponseJSON: "{}", ModelID: "m1",
	}); err != nil {
		t.Fatalf("seed trader decision: %v", err)
	}
	if _, err := decisions.Insert(ctx, domain.JevDecision{
		InstrumentID: te.instrument.ID, Symbol: "7203", Timestamp: scoutAt,
		DecisionType: domain.JevDecisionTypeScout, StateHash: "h2", StateJSON: "{}",
		QuestionVersion: "v1", ResponseJSON: "{}", ModelID: "m1",
	}); err != nil {
		t.Fatalf("seed scout decision: %v", err)
	}

	signals := te.signals
	if _, err := signals.Insert(ctx, domain.TradeSignal{
		InstrumentID: te.instrument.ID, Symbol: "7203", Timestamp: now, Direction: domain.JevDirectionLong,
		Score: floatPtr(0.74), PolicyVersion: "v1", RiskPassed: true,
	}); err != nil {
		t.Fatalf("seed signal: %v", err)
	}

	entry, err := te.engine.Enter(ctx, execution.EntryRequest{
		Signal: longSignal(te.instrument.ID), Quantity: 100, OrderType: domain.OrderTypeMarket, Price: 2105.0, Now: now,
	})
	if err != nil {
		t.Fatalf("Enter: %v", err)
	}

	state, err := te.engine.State(ctx, "7203")
	if err != nil {
		t.Fatalf("State: %v", err)
	}
	if state.LastPrice != 2105.0 || state.LastScanAt == nil || !state.LastScanAt.Equal(now) {
		t.Fatalf("State() snapshot fields = LastPrice=%v LastScanAt=%v, want 2105.0/%v", state.LastPrice, state.LastScanAt, now)
	}
	if state.LastJevScoutAt == nil || !state.LastJevScoutAt.Equal(scoutAt) {
		t.Fatalf("State().LastJevScoutAt = %v, want %v", state.LastJevScoutAt, scoutAt)
	}
	if state.LastJevTraderAt == nil || !state.LastJevTraderAt.Equal(traderAt) {
		t.Fatalf("State().LastJevTraderAt = %v, want %v", state.LastJevTraderAt, traderAt)
	}
	if state.LastSignal != domain.JevDirectionLong || state.LastSignalConfidence != 0.74 {
		t.Fatalf("State() signal fields = LastSignal=%q LastSignalConfidence=%v, want LONG/0.74", state.LastSignal, state.LastSignalConfidence)
	}
	if state.Position == nil || state.Position.ID != entry.Position.ID {
		t.Fatalf("State().Position = %+v, want the just-opened position", state.Position)
	}
}

func TestEngine_State_ReportsCooldownAfterLosingClose(t *testing.T) {
	now := time.Date(2026, 9, 27, 9, 31, 0, 0, time.UTC)
	// State() reads Engine's cooldown window via cfg.Now() (state.go),
	// so this must be pinned like Enter/Close's explicit Now args below
	// - leaving it nil defaults to real time.Now() (config.go's
	// withDefaults) and the cooldown (closedAt + 5min) would already
	// be in the past for any run after 2026-09-27.
	closedAt := now.Add(time.Minute)
	te := newTestEngine(t, execution.Config{
		CooldownAfterLossMinutes: 5,
		Now:                      func() time.Time { return closedAt.Add(time.Minute) },
	})
	ctx := context.Background()

	entry, err := te.engine.Enter(ctx, execution.EntryRequest{
		Signal: longSignal(te.instrument.ID), Quantity: 100, OrderType: domain.OrderTypeMarket, Price: 2100.0, Now: now,
	})
	if err != nil {
		t.Fatalf("Enter: %v", err)
	}
	if _, err := te.engine.Close(ctx, entry.Position.ID, domain.ExitReasonStopLoss, 2088.0, closedAt); err != nil {
		t.Fatalf("Close: %v", err)
	}

	state, err := te.engine.State(ctx, "7203")
	if err != nil {
		t.Fatalf("State: %v", err)
	}
	if state.CooldownUntil == nil {
		t.Fatalf("State().CooldownUntil = nil, want a set cooldown after a losing close")
	}
	if state.Position != nil {
		t.Fatalf("State().Position = %+v, want nil (position closed)", state.Position)
	}
}
