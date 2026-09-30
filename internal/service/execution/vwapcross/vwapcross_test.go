package vwapcross_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/judgement"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/repository/trading"
	"github.com/ousiassllc/pitha-trador/internal/service/execution"
	"github.com/ousiassllc/pitha-trador/internal/service/execution/vwapcross"
)

func TestCrossed(t *testing.T) {
	long, short := domain.PositionSideLong, domain.PositionSideShort
	tests := []struct {
		name                             string
		side                             string
		prevPrice, prevVWAP, price, vwap float64
		want                             bool
	}{
		{"long crosses below VWAP", long, 101, 100, 99, 100, true},
		{"long stays below VWAP", long, 99, 100, 98, 100, false},
		{"long crosses above VWAP (favorable)", long, 99, 100, 101, 100, false},
		{"long touches VWAP is not adverse", long, 101, 100, 100, 100, false},
		{"long previously exactly on VWAP then below", long, 100, 100, 99, 100, true},
		{"short crosses above VWAP", short, 99, 100, 101, 100, true},
		{"short stays above VWAP", short, 101, 100, 102, 100, false},
		{"short crosses below VWAP (favorable)", short, 101, 100, 99, 100, false},
		{"VWAP moving up past a flat price is a cross for a long", long, 100, 99, 100, 101, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := vwapcross.Crossed(tt.side, tt.prevPrice, tt.prevVWAP, tt.price, tt.vwap); got != tt.want {
				t.Errorf("Crossed() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTracker_PreviousIsScopedToThePosition(t *testing.T) {
	var tr vwapcross.Tracker
	if _, ok := tr.Previous(1, 10); ok {
		t.Fatal("Previous on an empty tracker = ok, want none")
	}
	tr.Record(1, vwapcross.Observation{PositionID: 10, Price: 100, VWAP: 101})
	if obs, ok := tr.Previous(1, 10); !ok || obs.Price != 100 || obs.VWAP != 101 {
		t.Errorf("Previous(1, 10) = %+v, %v, want the recorded observation", obs, ok)
	}
	if _, ok := tr.Previous(1, 11); ok {
		t.Error("Previous for a newer position on the same instrument reused the closed position's observation")
	}
}

// newLongEngine returns an Engine with a LONG position entered at 2000
// (issue #192's end-to-end path through OnSnapshot).
func newLongEngine(t *testing.T) (*execution.Engine, int64, time.Time) {
	t.Helper()
	ctx := context.Background()
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "pitha.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	inst, err := market.NewInstrumentRepository(db).Create(ctx, domain.Instrument{Symbol: "7203", Name: "トヨタ自動車", Market: "TSE Prime", IsActive: true})
	if err != nil {
		t.Fatal(err)
	}
	engine := execution.NewEngine(execution.Deps{
		Orders: trading.NewOrderRepository(db), Positions: trading.NewPositionRepository(db),
		Snapshots: market.NewSnapshotRepository(db), Decisions: judgement.NewDecisionRepository(db),
		Signals: trading.NewSignalRepository(db), Instruments: market.NewInstrumentRepository(db),
	}, execution.DefaultConfig())
	opened := time.Date(2026, 9, 27, 9, 31, 0, 0, time.UTC)
	if _, err := engine.Enter(ctx, execution.EntryRequest{
		Signal:   domain.TradeSignal{InstrumentID: inst.ID, Symbol: "7203", Direction: domain.JevDirectionLong, RiskPassed: true, PolicyVersion: "v1"},
		Quantity: 100, Price: 2000, Now: opened,
	}); err != nil {
		t.Fatalf("Enter: %v", err)
	}
	return engine, inst.ID, opened
}

// run feeds prices (all within SL/TP) against a fixed vwap and returns
// each OnSnapshot's Exited flag.
func run(t *testing.T, engine *execution.Engine, id int64, opened time.Time, vwap float64, prices ...float64) []bool {
	t.Helper()
	exited := make([]bool, len(prices))
	for i, price := range prices {
		snap := domain.Snapshot{InstrumentID: id, Symbol: "7203", Timestamp: opened.Add(time.Duration(i+1) * 15 * time.Second), Price: price}
		snap.Feature.VWAP = vwap
		result, err := engine.OnSnapshot(context.Background(), snap)
		if err != nil {
			t.Fatalf("OnSnapshot #%d: %v", i, err)
		}
		exited[i] = result.Exited
	}
	return exited
}

// A LONG entered below VWAP (adverse side) must survive evaluations that
// keep it there: VWAP逆クロス is a cross, not a stay on the adverse side.
func TestOnSnapshot_EntryOnAdverseVWAPSideIsNotClosedWhileStaying(t *testing.T) {
	engine, id, opened := newLongEngine(t)
	for i, exited := range run(t, engine, id, opened, 2010, 2001, 2002, 2001) {
		if exited {
			t.Fatalf("OnSnapshot #%d closed a position that merely stayed below VWAP", i)
		}
	}
}

func TestOnSnapshot_ClosesOnVWAPCrossAgainstThePosition(t *testing.T) {
	engine, id, opened := newLongEngine(t)
	// Below VWAP (entered adverse) -> above -> back below: only the last
	// step is a cross against the LONG.
	got := run(t, engine, id, opened, 2005, 2001, 2006, 2003)
	if want := []bool{false, false, true}; got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("Exited per snapshot = %v, want %v", got, want)
	}
}
