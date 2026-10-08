package heldposition_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap/heldposition"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/broker"
	"github.com/ousiassllc/pitha-trador/internal/service/execution"
)

type fakePositions struct {
	open []domain.Position
	err  error
}

func (f fakePositions) ListOpen(context.Context) ([]domain.Position, error) { return f.open, f.err }

type fakeQuotes struct {
	boards map[string]broker.Quote
	calls  []string
}

func (f *fakeQuotes) Latest(_ context.Context, symbol string) (broker.Quote, error) {
	f.calls = append(f.calls, symbol)
	if b, ok := f.boards[symbol]; ok {
		return b, nil
	}
	return broker.Quote{}, errors.New("board unavailable")
}

type fakeExits struct{ snaps []domain.Snapshot }

func (f *fakeExits) OnSnapshot(_ context.Context, snap domain.Snapshot) (execution.SnapshotResult, error) {
	f.snaps = append(f.snaps, snap)
	return execution.SnapshotResult{}, nil
}

func monitor(open bool, positions fakePositions, boards *fakeQuotes, exits *fakeExits) heldposition.Monitor {
	return heldposition.Monitor{
		Positions: positions, Quotes: boards, Exits: exits,
		Open: func(time.Time) bool { return open },
		Now:  func() time.Time { return time.Date(2026, 9, 29, 1, 0, 0, 0, time.UTC) },
	}
}

func held(id int64, symbol string) domain.Position {
	return domain.Position{ID: id, InstrumentID: id, Symbol: symbol}
}

func TestMonitor_CyclePricesEachHeldPositionFromItsBoard(t *testing.T) {
	boards := &fakeQuotes{boards: map[string]broker.Quote{
		"7203": {Price: 2400, VWAP: 2450},
		"6758": {Price: 900, VWAP: 910},
	}}
	exits := &fakeExits{}
	n, err := monitor(true, fakePositions{open: []domain.Position{held(1, "7203"), held(2, "6758")}}, boards, exits).Cycle(context.Background())
	if err != nil || n != 2 {
		t.Fatalf("Cycle = (%d, %v), want (2, nil)", n, err)
	}
	if len(exits.snaps) != 2 || exits.snaps[0].Price != 2400 || exits.snaps[0].Feature.VWAP != 2450 || exits.snaps[1].InstrumentID != 2 {
		t.Fatalf("snapshots = %+v", exits.snaps)
	}
}

func TestMonitor_CycleSkipsEverythingOutsideSession(t *testing.T) {
	boards, exits := &fakeQuotes{}, &fakeExits{}
	n, err := monitor(false, fakePositions{open: []domain.Position{held(1, "7203")}}, boards, exits).Cycle(context.Background())
	if err != nil || n != 0 || len(boards.calls) != 0 || len(exits.snaps) != 0 {
		t.Fatalf("off-hours Cycle = (%d, %v), boards %v, snaps %v; want nothing fetched", n, err, boards.calls, exits.snaps)
	}
}

func TestMonitor_CycleContinuesPastBoardFailure(t *testing.T) {
	boards := &fakeQuotes{boards: map[string]broker.Quote{"6758": {Price: 900}}}
	exits := &fakeExits{}
	n, err := monitor(true, fakePositions{open: []domain.Position{held(1, "7203"), held(2, "6758")}}, boards, exits).Cycle(context.Background())
	if err != nil || n != 1 || len(exits.snaps) != 1 || exits.snaps[0].Symbol != "6758" {
		t.Fatalf("Cycle = (%d, %v), snaps %+v; want only 6758 evaluated", n, err, exits.snaps)
	}
}

func TestMonitor_CycleReturnsListError(t *testing.T) {
	boom := errors.New("db down")
	_, err := monitor(true, fakePositions{err: boom}, &fakeQuotes{}, &fakeExits{}).Cycle(context.Background())
	if !errors.Is(err, boom) {
		t.Fatalf("Cycle err = %v, want wrapping %v", err, boom)
	}
}

func TestMonitor_RunCyclesOnTheConfiguredInterval(t *testing.T) {
	boards := &fakeQuotes{boards: map[string]broker.Quote{"7203": {Price: 2500}}}
	exits := &fakeExits{}
	m := monitor(true, fakePositions{open: []domain.Position{held(1, "7203")}}, boards, exits)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { m.Run(ctx, 5*time.Millisecond, 10*time.Millisecond); close(done) }()
	time.Sleep(200 * time.Millisecond)
	cancel()
	<-done
	if len(boards.calls) < 3 {
		t.Fatalf("Run evaluated %d times in 200ms at a 5-10ms interval, want >= 3", len(boards.calls))
	}
}

func TestMonitor_CycleSkipsBoardWithoutCurrentPrice(t *testing.T) {
	boards := &fakeQuotes{boards: map[string]broker.Quote{"7203": {Price: 0}, "6758": {Price: 900}}}
	exits := &fakeExits{}
	n, err := monitor(true, fakePositions{open: []domain.Position{held(1, "7203"), held(2, "6758")}}, boards, exits).Cycle(context.Background())
	if err != nil || n != 1 || len(exits.snaps) != 1 || exits.snaps[0].Symbol != "6758" {
		t.Fatalf("Cycle = (%d, %v), snaps %+v; want only 6758 evaluated", n, err, exits.snaps)
	}
}

// panicOnceExits panics on its first OnSnapshot call, then records.
type panicOnceExits struct {
	fakeExits
	calls int
}

func (f *panicOnceExits) OnSnapshot(ctx context.Context, snap domain.Snapshot) (execution.SnapshotResult, error) {
	f.calls++
	if f.calls == 1 {
		panic("exit evaluation blew up")
	}
	return f.fakeExits.OnSnapshot(ctx, snap)
}

// FR-SCHED-6: a panic inside one cycle is logged, not fatal, and the
// monitor keeps evaluating on the following cycles.
func TestMonitor_RunSurvivesPanickingCycle(t *testing.T) {
	boards := &fakeQuotes{boards: map[string]broker.Quote{"7203": {Price: 2500}}}
	exits := &panicOnceExits{}
	m := monitor(true, fakePositions{open: []domain.Position{held(1, "7203")}}, boards, &fakeExits{})
	m.Exits = exits

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { m.Run(ctx, 5*time.Millisecond, 10*time.Millisecond); close(done) }()
	time.Sleep(200 * time.Millisecond)
	cancel()
	<-done
	if len(exits.snaps) < 2 {
		t.Fatalf("evaluations after the panicking cycle = %d, want >= 2", len(exits.snaps))
	}
}

// panicOnSymbolExits panics for one symbol and records the rest.
type panicOnSymbolExits struct {
	fakeExits
	symbol string
}

func (f *panicOnSymbolExits) OnSnapshot(ctx context.Context, snap domain.Snapshot) (execution.SnapshotResult, error) {
	if snap.Symbol == f.symbol {
		panic("exit evaluation blew up")
	}
	return f.fakeExits.OnSnapshot(ctx, snap)
}

// FR-SCHED-6: a position whose evaluation always panics must not starve
// the positions after it of exit judgement.
func TestMonitor_CycleContinuesPastPanickingPosition(t *testing.T) {
	boards := &fakeQuotes{boards: map[string]broker.Quote{
		"7203": {Price: 2400}, "6758": {Price: 900}, "9984": {Price: 8000},
	}}
	exits := &panicOnSymbolExits{symbol: "7203"}
	m := monitor(true, fakePositions{open: []domain.Position{held(1, "7203"), held(2, "6758"), held(3, "9984")}}, boards, &fakeExits{})
	m.Exits = exits
	n, err := m.Cycle(context.Background())
	if err != nil || n != 2 || len(exits.snaps) != 2 || exits.snaps[0].Symbol != "6758" || exits.snaps[1].Symbol != "9984" {
		t.Fatalf("Cycle = (%d, %v), snaps %+v; want 6758 and 9984 evaluated", n, err, exits.snaps)
	}
}

// Regression test for issue #545: the snapshot carries the quote's
// Bid=buy / Ask=sell prices as they are and their spread, so an exit fills
// across the spread.
func TestMonitor_CycleSnapshotCarriesQuoteAndSpread(t *testing.T) {
	sell, buy := 2408.5, 2407.5
	boards := &fakeQuotes{boards: map[string]broker.Quote{
		"7203": {Price: 2408, Bid: &buy, Ask: &sell},
		"6758": {Price: 900, Bid: &sell, Ask: &buy}, // crossed
		"9984": {Price: 100},                        // no book
	}}
	exits := &fakeExits{}
	open := []domain.Position{held(1, "7203"), held(2, "6758"), held(3, "9984")}
	if n, err := monitor(true, fakePositions{open: open}, boards, exits).Cycle(context.Background()); err != nil || n != 3 {
		t.Fatalf("Cycle = (%d, %v), want (3, nil)", n, err)
	}

	s := exits.snaps[0]
	if s.Bid == nil || *s.Bid != buy || s.Ask == nil || *s.Ask != sell {
		t.Fatalf("Bid/Ask = %v/%v, want %v/%v", s.Bid, s.Ask, buy, sell)
	}
	if s.SpreadBps == nil || *s.SpreadBps <= 0 {
		t.Errorf("SpreadBps = %v, want > 0", s.SpreadBps)
	}
	if c := exits.snaps[1]; c.SpreadBps != nil {
		t.Errorf("crossed book SpreadBps = %v, want nil", *c.SpreadBps)
	}
	if n := exits.snaps[2]; n.Bid != nil || n.Ask != nil || n.SpreadBps != nil {
		t.Errorf("board without a book: Bid/Ask/SpreadBps = %v/%v/%v, want all nil (fill falls back to the last price)", n.Bid, n.Ask, n.SpreadBps)
	}
}
