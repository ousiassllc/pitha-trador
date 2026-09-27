package featureengine_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository"
	"github.com/ousiassllc/pitha-trador/internal/service/featureengine"
	"github.com/ousiassllc/pitha-trador/internal/service/rag"
)

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := repository.Open(filepath.Join(t.TempDir(), "pitha_test.db"))
	if err != nil {
		t.Fatalf("repository.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func mustCreateInstrument(t *testing.T, repo *repository.InstrumentRepository, symbol string) domain.Instrument {
	t.Helper()
	inst, err := repo.Create(context.Background(), domain.Instrument{
		Symbol: symbol, Name: symbol + " Inc.", Market: "TSE Prime", IsActive: true,
	})
	if err != nil {
		t.Fatalf("Create instrument %q: %v", symbol, err)
	}
	return inst
}

func TestEngine_RunCycle_PersistsComputedSnapshots(t *testing.T) {
	db := newTestDB(t)
	instruments := repository.NewInstrumentRepository(db)
	snapshots := repository.NewSnapshotRepository(db)
	engine := featureengine.NewEngine(snapshots, rag.NewService(db, repository.NewDecisionRepository(db), snapshots))

	inst := mustCreateInstrument(t, instruments, "7203")
	now := time.Date(2026, 9, 27, 9, 31, 0, 0, time.UTC)

	saved, err := engine.RunCycle(context.Background(), []featureengine.CycleInput{
		{
			InstrumentID: inst.ID,
			Symbol:       inst.Symbol,
			Input: featureengine.Input{
				Timestamp: now,
				Current:   featureengine.Reading{Price: 2100, VWAP: 2090, Volume: 1000, Turnover: 2_000_000},
			},
			RawDataJSON: `{"Symbol":"7203"}`,
		},
	})
	if err != nil {
		t.Fatalf("RunCycle: %v", err)
	}
	if len(saved) != 1 {
		t.Fatalf("RunCycle returned %d snapshots, want 1", len(saved))
	}
	if saved[0].ID == 0 {
		t.Error("saved snapshot has no assigned ID")
	}
	if saved[0].Feature.VWAP != 2090 {
		t.Errorf("saved VWAP = %v, want 2090", saved[0].Feature.VWAP)
	}

	fetched, err := snapshots.Get(context.Background(), saved[0].ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if fetched.InstrumentID != inst.ID || fetched.Symbol != "7203" {
		t.Errorf("fetched snapshot = %+v", fetched)
	}
	if fetched.RawDataJSON != `{"Symbol":"7203"}` {
		t.Errorf("RawDataJSON = %q", fetched.RawDataJSON)
	}
}

func TestEngine_RunCycle_ComputesSpreadBpsFromBoard(t *testing.T) {
	db := newTestDB(t)
	instruments := repository.NewInstrumentRepository(db)
	snapshots := repository.NewSnapshotRepository(db)
	engine := featureengine.NewEngine(snapshots, rag.NewService(db, repository.NewDecisionRepository(db), snapshots))

	inst := mustCreateInstrument(t, instruments, "9433")
	now := time.Date(2026, 9, 27, 9, 31, 0, 0, time.UTC)
	bid, ask := 2408.5, 2409.5

	saved, err := engine.RunCycle(context.Background(), []featureengine.CycleInput{
		{
			InstrumentID: inst.ID,
			Symbol:       inst.Symbol,
			Input: featureengine.Input{
				Timestamp: now,
				Current:   featureengine.Reading{Price: 2409, VWAP: 2400, Bid: &bid, Ask: &ask},
			},
		},
	})
	if err != nil {
		t.Fatalf("RunCycle: %v", err)
	}
	if saved[0].SpreadBps == nil {
		t.Fatal("SpreadBps = nil, want a value from bid/ask")
	}
	wantMid := (bid + ask) / 2
	want := (ask - bid) / wantMid * 10000
	if *saved[0].SpreadBps != want {
		t.Errorf("SpreadBps = %v, want %v", *saved[0].SpreadBps, want)
	}
}

func TestEngine_RunCycle_NoBoardDataLeavesSpreadBpsNil(t *testing.T) {
	db := newTestDB(t)
	instruments := repository.NewInstrumentRepository(db)
	snapshots := repository.NewSnapshotRepository(db)
	engine := featureengine.NewEngine(snapshots, rag.NewService(db, repository.NewDecisionRepository(db), snapshots))

	inst := mustCreateInstrument(t, instruments, "1301")
	now := time.Date(2026, 9, 27, 9, 31, 0, 0, time.UTC)

	saved, err := engine.RunCycle(context.Background(), []featureengine.CycleInput{
		{
			InstrumentID: inst.ID,
			Symbol:       inst.Symbol,
			Input: featureengine.Input{
				Timestamp: now,
				Current:   featureengine.Reading{Price: 3000, VWAP: 3000},
			},
		},
	})
	if err != nil {
		t.Fatalf("RunCycle: %v", err)
	}
	if saved[0].Bid != nil || saved[0].Ask != nil || saved[0].SpreadBps != nil {
		t.Errorf("bid/ask/spread = (%v, %v, %v), want all nil (FR-FE-2)", saved[0].Bid, saved[0].Ask, saved[0].SpreadBps)
	}
}

func TestEngine_RunCycle_MultipleInstrumentsInOneTransaction(t *testing.T) {
	db := newTestDB(t)
	instruments := repository.NewInstrumentRepository(db)
	snapshots := repository.NewSnapshotRepository(db)
	engine := featureengine.NewEngine(snapshots, rag.NewService(db, repository.NewDecisionRepository(db), snapshots))

	a := mustCreateInstrument(t, instruments, "7203")
	b := mustCreateInstrument(t, instruments, "9433")
	now := time.Date(2026, 9, 27, 9, 31, 0, 0, time.UTC)

	saved, err := engine.RunCycle(context.Background(), []featureengine.CycleInput{
		{InstrumentID: a.ID, Symbol: a.Symbol, Input: featureengine.Input{Timestamp: now, Current: featureengine.Reading{Price: 2100, VWAP: 2100}}},
		{InstrumentID: b.ID, Symbol: b.Symbol, Input: featureengine.Input{Timestamp: now, Current: featureengine.Reading{Price: 2400, VWAP: 2400}}},
	})
	if err != nil {
		t.Fatalf("RunCycle: %v", err)
	}
	if len(saved) != 2 {
		t.Fatalf("RunCycle returned %d snapshots, want 2", len(saved))
	}
}

func TestEngine_RunCycle_DuplicateBarRollsBackWholeCycle(t *testing.T) {
	db := newTestDB(t)
	instruments := repository.NewInstrumentRepository(db)
	snapshots := repository.NewSnapshotRepository(db)
	engine := featureengine.NewEngine(snapshots, rag.NewService(db, repository.NewDecisionRepository(db), snapshots))

	a := mustCreateInstrument(t, instruments, "7203")
	b := mustCreateInstrument(t, instruments, "9433")
	now := time.Date(2026, 9, 27, 9, 31, 0, 0, time.UTC)

	inputs := []featureengine.CycleInput{
		{InstrumentID: a.ID, Symbol: a.Symbol, Input: featureengine.Input{Timestamp: now, Current: featureengine.Reading{Price: 2100, VWAP: 2100}}},
		{InstrumentID: b.ID, Symbol: b.Symbol, Input: featureengine.Input{Timestamp: now, Current: featureengine.Reading{Price: 2400, VWAP: 2400}}},
	}

	if _, err := engine.RunCycle(context.Background(), inputs); err != nil {
		t.Fatalf("first RunCycle: %v", err)
	}

	// Re-running the same cycle violates UNIQUE (instrument_id,
	// timestamp) for both instruments; the transaction must roll back
	// entirely rather than leaving a partial write.
	if _, err := engine.RunCycle(context.Background(), inputs); err == nil {
		t.Fatal("second RunCycle with duplicate bars: want error, got nil")
	}

	list, err := snapshots.ListByInstrument(context.Background(), a.ID, 10)
	if err != nil {
		t.Fatalf("ListByInstrument: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("ListByInstrument(a) returned %d rows, want 1 (no partial duplicate write)", len(list))
	}
}
