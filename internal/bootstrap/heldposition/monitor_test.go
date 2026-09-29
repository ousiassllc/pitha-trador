package heldposition_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap/heldposition"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/execution"
	"github.com/ousiassllc/pitha-trador/internal/service/marketdata"
)

type fakePositions struct {
	open []domain.Position
	err  error
}

func (f fakePositions) ListOpen(context.Context) ([]domain.Position, error) { return f.open, f.err }

type fakeBoards struct {
	boards map[string]marketdata.Board
	calls  []string
}

func (f *fakeBoards) GetBoard(_ context.Context, symbol string, exchange int) (marketdata.Board, error) {
	f.calls = append(f.calls, symbol)
	if b, ok := f.boards[symbol]; ok {
		return b, nil
	}
	return marketdata.Board{}, errors.New("board unavailable")
}

type fakeExits struct{ snaps []domain.Snapshot }

func (f *fakeExits) OnSnapshot(_ context.Context, snap domain.Snapshot) (execution.SnapshotResult, error) {
	f.snaps = append(f.snaps, snap)
	return execution.SnapshotResult{}, nil
}

func monitor(open bool, positions fakePositions, boards *fakeBoards, exits *fakeExits) heldposition.Monitor {
	return heldposition.Monitor{
		Positions: positions, Boards: boards, Exits: exits, Exchange: 1,
		Open: func(time.Time) bool { return open },
		Now:  func() time.Time { return time.Date(2026, 9, 29, 1, 0, 0, 0, time.UTC) },
	}
}

func held(id int64, symbol string) domain.Position {
	return domain.Position{ID: id, InstrumentID: id, Symbol: symbol}
}

func TestMonitor_CyclePricesEachHeldPositionFromItsBoard(t *testing.T) {
	boards := &fakeBoards{boards: map[string]marketdata.Board{
		"7203": {CurrentPrice: 2400, VWAP: 2450},
		"6758": {CurrentPrice: 900, VWAP: 910},
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
	boards, exits := &fakeBoards{}, &fakeExits{}
	n, err := monitor(false, fakePositions{open: []domain.Position{held(1, "7203")}}, boards, exits).Cycle(context.Background())
	if err != nil || n != 0 || len(boards.calls) != 0 || len(exits.snaps) != 0 {
		t.Fatalf("off-hours Cycle = (%d, %v), boards %v, snaps %v; want nothing fetched", n, err, boards.calls, exits.snaps)
	}
}

func TestMonitor_CycleContinuesPastBoardFailure(t *testing.T) {
	boards := &fakeBoards{boards: map[string]marketdata.Board{"6758": {CurrentPrice: 900}}}
	exits := &fakeExits{}
	n, err := monitor(true, fakePositions{open: []domain.Position{held(1, "7203"), held(2, "6758")}}, boards, exits).Cycle(context.Background())
	if err != nil || n != 1 || len(exits.snaps) != 1 || exits.snaps[0].Symbol != "6758" {
		t.Fatalf("Cycle = (%d, %v), snaps %+v; want only 6758 evaluated", n, err, exits.snaps)
	}
}

func TestMonitor_CycleReturnsListError(t *testing.T) {
	boom := errors.New("db down")
	_, err := monitor(true, fakePositions{err: boom}, &fakeBoards{}, &fakeExits{}).Cycle(context.Background())
	if !errors.Is(err, boom) {
		t.Fatalf("Cycle err = %v, want wrapping %v", err, boom)
	}
}

func TestMonitor_RunCyclesOnTheConfiguredInterval(t *testing.T) {
	boards := &fakeBoards{boards: map[string]marketdata.Board{"7203": {CurrentPrice: 2500}}}
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
