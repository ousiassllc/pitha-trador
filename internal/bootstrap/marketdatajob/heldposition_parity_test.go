package marketdatajob

import (
	"context"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap/heldposition"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/execution"
	"github.com/ousiassllc/pitha-trador/internal/service/marketdata"
)

type parityPositions struct{ p domain.Position }

func (f parityPositions) ListOpen(context.Context) ([]domain.Position, error) {
	return []domain.Position{f.p}, nil
}

type parityBoards struct{ board marketdata.Board }

func (f parityBoards) GetBoard(context.Context, string, int) (marketdata.Board, error) {
	return f.board, nil
}

type parityExits struct{ snaps []domain.Snapshot }

func (f *parityExits) OnSnapshot(_ context.Context, snap domain.Snapshot) (execution.SnapshotResult, error) {
	f.snaps = append(f.snaps, snap)
	return execution.SnapshotResult{}, nil
}

// Regression test for issue #545: the held-position monitor's transient
// snapshot must carry the same Bid/Ask/SpreadBps as the bar the 60-second
// market-data job persists for the same board (a crossed book included),
// otherwise an exit fills at a different price depending on which path
// evaluates it first.
func TestHeldPositionSnapshotQuoteMatchesPersistedBar(t *testing.T) {
	boards := map[string]marketdata.Board{
		"normal": {Symbol: "7203", CurrentPrice: 2408, VWAP: 2400, TradingVolume: 1000000, TradingValue: 2.4e9,
			BidPrice: fptr(2408.5), BidQty: fptr(100), AskPrice: fptr(2407.5), AskQty: fptr(200)},
		"crossed": {Symbol: "7203", CurrentPrice: 2408, VWAP: 2400, TradingVolume: 1000000, TradingValue: 2.4e9,
			BidPrice: fptr(2407), BidQty: fptr(100), AskPrice: fptr(2409), AskQty: fptr(200)},
		"no quote": {Symbol: "7203", CurrentPrice: 2408, VWAP: 2400, TradingVolume: 1000000, TradingValue: 2.4e9},
	}
	for name, board := range boards {
		t.Run(name, func(t *testing.T) {
			env := newTestEnv(t)
			env.Fake.board = board
			inst := mustCreateInstrument(t, env, "7203")
			if err := env.HandleMarketData(context.Background(), marketDataJob(t, inst)); err != nil {
				t.Fatalf("HandleMarketData: %v", err)
			}
			persisted, err := env.Snapshots.ListByInstrument(context.Background(), inst.ID, 10)
			if err != nil || len(persisted) != 1 {
				t.Fatalf("ListByInstrument = %d snaps, err %v; want 1", len(persisted), err)
			}

			exits := &parityExits{}
			held := heldposition.Monitor{
				Positions: parityPositions{domain.Position{ID: 1, InstrumentID: inst.ID, Symbol: "7203"}},
				Boards:    parityBoards{board}, Exits: exits, Exchange: 1,
				Open: func(time.Time) bool { return true },
			}
			if n, err := held.Cycle(context.Background()); err != nil || n != 1 || len(exits.snaps) != 1 {
				t.Fatalf("Cycle = (%d, %v), snaps %d; want one evaluated position", n, err, len(exits.snaps))
			}

			got, want := exits.snaps[0], persisted[0]
			if !equalPtr(got.Bid, want.Bid) || !equalPtr(got.Ask, want.Ask) || !equalPtr(got.SpreadBps, want.SpreadBps) {
				t.Errorf("held-position Bid/Ask/SpreadBps = %v/%v/%v, persisted bar has %v/%v/%v",
					deref(got.Bid), deref(got.Ask), deref(got.SpreadBps), deref(want.Bid), deref(want.Ask), deref(want.SpreadBps))
			}
		})
	}
}

func equalPtr(a, b *float64) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func deref(p *float64) any {
	if p == nil {
		return nil
	}
	return *p
}
