package rag_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/judgement"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/service/rag"
)

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "pitha.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func mustCreateInstrument(t *testing.T, instruments *market.InstrumentRepository, symbol string) domain.Instrument {
	t.Helper()
	inst, err := instruments.Create(context.Background(), domain.Instrument{
		Symbol: symbol, Name: "Test " + symbol, Market: "TSE Prime", IsActive: true,
	})
	if err != nil {
		t.Fatalf("Create instrument %q: %v", symbol, err)
	}
	return inst
}

func TestService_Context_ColdStartReturnsEmptyNotError(t *testing.T) {
	db := newTestDB(t)
	svc := rag.NewService(db, judgement.NewDecisionRepository(db), market.NewSnapshotRepository(db))

	got, err := svc.Context(context.Background(), rag.FeatureInput{Return1m: ptr(0.01)}, rag.Subject{}, rag.DefaultK)
	if err != nil {
		t.Fatalf("Context: %v (a cold-start empty index must not be an error, functional.md FR-RAG-4)", err)
	}
	if len(got.Cases) != 0 {
		t.Errorf("Context().Cases = %+v, want empty on a fresh, unindexed database", got.Cases)
	}
}

func TestService_IndexSnapshot_ThenContextFindsItAsMarketSnapshotCase(t *testing.T) {
	db := newTestDB(t)
	instruments := market.NewInstrumentRepository(db)
	snapshots := market.NewSnapshotRepository(db)
	svc := rag.NewService(db, judgement.NewDecisionRepository(db), snapshots)

	inst := mustCreateInstrument(t, instruments, "7203")
	ts := time.Date(2026, 9, 27, 9, 31, 0, 0, time.UTC)
	snap, err := snapshots.Insert(context.Background(), domain.Snapshot{
		InstrumentID: inst.ID, Symbol: inst.Symbol, Timestamp: ts,
		Price: 2100, Volume: 1000, Turnover: 2_000_000,
		Feature:     domain.Feature{VWAP: 2090, PriceVsVWAPBps: 47.8, Return1m: ptr(0.01)},
		RawDataJSON: `{}`,
	})
	if err != nil {
		t.Fatalf("insert snapshot fixture: %v", err)
	}

	in := rag.FeatureInputFromFeature(snap.Feature, snap.SpreadBps)
	if err := svc.IndexSnapshot(context.Background(), snap.ID, in); err != nil {
		t.Fatalf("IndexSnapshot: %v", err)
	}

	got, err := svc.Context(context.Background(), in, rag.Subject{}, rag.DefaultK)
	if err != nil {
		t.Fatalf("Context: %v", err)
	}
	if len(got.Cases) != 1 {
		t.Fatalf("Context().Cases = %+v, want exactly the one indexed snapshot", got.Cases)
	}
	c := got.Cases[0]
	if c.Source != "market_snapshot" || c.Symbol != "7203" || !c.Timestamp.Equal(ts) {
		t.Errorf("Context().Cases[0] = %+v, want source=market_snapshot symbol=7203 timestamp=%v", c, ts)
	}
	if c.Distance != 0 {
		t.Errorf("Context().Cases[0].Distance = %v, want 0 for an identical query vector", c.Distance)
	}
}

func TestService_Context_PrioritizesDecisionsOverSnapshotsAndBackfillsToK(t *testing.T) {
	db := newTestDB(t)
	instruments := market.NewInstrumentRepository(db)
	snapshots := market.NewSnapshotRepository(db)
	decisions := judgement.NewDecisionRepository(db)
	svc := rag.NewService(db, decisions, snapshots)

	inst := mustCreateInstrument(t, instruments, "9433")
	ts := time.Date(2026, 9, 27, 9, 31, 0, 0, time.UTC)
	in := rag.FeatureInput{Return1m: ptr(0.01), Return5m: ptr(0.02)}

	snap, err := snapshots.Insert(context.Background(), domain.Snapshot{
		InstrumentID: inst.ID, Symbol: inst.Symbol, Timestamp: ts,
		Price: 1000, Volume: 100, Turnover: 100_000,
		Feature:     domain.Feature{VWAP: 995},
		RawDataJSON: `{}`,
	})
	if err != nil {
		t.Fatalf("insert snapshot fixture: %v", err)
	}
	if err := svc.IndexSnapshot(context.Background(), snap.ID, in); err != nil {
		t.Fatalf("IndexSnapshot: %v", err)
	}

	direction := "LONG"
	decision, err := decisions.Insert(context.Background(), domain.JevDecision{
		InstrumentID: inst.ID, Symbol: inst.Symbol, Timestamp: ts,
		DecisionType: domain.JevDecisionTypeTrader, Direction: &direction,
		StateHash: "abc", StateJSON: `{}`, QuestionVersion: "trader-v1",
		ResponseJSON: `{}`, ModelID: "jev-trader-test",
	})
	if err != nil {
		t.Fatalf("insert decision fixture: %v", err)
	}
	if err := svc.IndexDecision(context.Background(), decision.ID, in); err != nil {
		t.Fatalf("IndexDecision: %v", err)
	}

	got, err := svc.Context(context.Background(), in, rag.Subject{}, 1)
	if err != nil {
		t.Fatalf("Context: %v", err)
	}
	if len(got.Cases) != 1 || got.Cases[0].Source != "jev_decision" {
		t.Fatalf("Context(k=1).Cases = %+v, want a single jev_decision case (jev_decisions is prioritized, FR-RAG-2)", got.Cases)
	}
	if got.Cases[0].Direction == nil || *got.Cases[0].Direction != "LONG" {
		t.Errorf("Context(k=1).Cases[0].Direction = %v, want \"LONG\"", got.Cases[0].Direction)
	}

	got, err = svc.Context(context.Background(), in, rag.Subject{}, 2)
	if err != nil {
		t.Fatalf("Context: %v", err)
	}
	if len(got.Cases) != 2 {
		t.Fatalf("Context(k=2).Cases = %+v, want 2: the jev_decision match backfilled with the market_snapshot match", got.Cases)
	}
	if got.Cases[0].Source != "jev_decision" || got.Cases[1].Source != "market_snapshot" {
		t.Errorf("Context(k=2).Cases sources = [%q, %q], want [jev_decision, market_snapshot]", got.Cases[0].Source, got.Cases[1].Source)
	}
}

// A vec0 hit whose market_snapshots row no longer resolves (vec0 has no FK,
// er/tables-system.md) is no data, not a failure: skipped, Context succeeds
// (issue #1214).
func TestService_Context_SkipsSimilarSnapshotWithoutRow(t *testing.T) {
	db := newTestDB(t)
	svc := rag.NewService(db, judgement.NewDecisionRepository(db), market.NewSnapshotRepository(db))

	in := rag.FeatureInput{Return1m: ptr(0.01)}
	const orphanSnapshotID = 424242
	if err := svc.IndexSnapshot(context.Background(), orphanSnapshotID, in); err != nil {
		t.Fatalf("IndexSnapshot: %v", err)
	}

	got, err := svc.Context(context.Background(), in, rag.Subject{}, rag.DefaultK)
	if err != nil {
		t.Fatalf("Context: %v (a vector without a snapshot row is ErrSnapshotNotFound and must be skipped)", err)
	}
	if len(got.Cases) != 0 {
		t.Errorf("Context().Cases = %+v, want empty: the only hit has no market_snapshots row", got.Cases)
	}
}

// Any snapshot load error other than "no row" (here: the repository's
// database is closed) is a real failure and must reach the caller instead of
// yielding a silently partial Context (issue #1214).
func TestService_Context_ReturnsErrorWhenSnapshotLoadFails(t *testing.T) {
	db := newTestDB(t)
	svc0 := rag.NewService(db, judgement.NewDecisionRepository(db), market.NewSnapshotRepository(db))
	in := rag.FeatureInput{Return1m: ptr(0.01)}
	if err := svc0.IndexSnapshot(context.Background(), 1, in); err != nil {
		t.Fatalf("IndexSnapshot: %v", err)
	}

	// Same vector index, but the snapshot repository's handle is dead.
	brokenDB, err := sqlitedb.Open(filepath.Join(t.TempDir(), "broken.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := brokenDB.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	svc := rag.NewService(db, judgement.NewDecisionRepository(db), market.NewSnapshotRepository(brokenDB))

	got, err := svc.Context(context.Background(), in, rag.Subject{}, rag.DefaultK)
	if err == nil {
		t.Fatalf("Context() = %+v, nil error; want the snapshot load failure returned", got)
	}
	if errors.Is(err, market.ErrSnapshotNotFound) {
		t.Errorf("Context error = %v, must not be ErrSnapshotNotFound", err)
	}
	if !strings.Contains(err.Error(), "rag: load similar snapshot") {
		t.Errorf("Context error = %q, want it wrapped as \"rag: load similar snapshot: ...\"", err)
	}
	if len(got.Cases) != 0 {
		t.Errorf("Context().Cases = %+v, want zero Context on error", got.Cases)
	}
}
