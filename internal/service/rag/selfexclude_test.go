package rag_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/judgement"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/service/rag"
)

// The subject's own rows are dropped after the KNN search, so a burst of
// own rows larger than the over-fetch margin must widen the search instead
// of starving the result: the genuine older snapshots still come back.
func TestService_Context_SnapshotsSurviveMoreOwnRowsThanTheMargin(t *testing.T) {
	f := newSubjectFixture(t, "7203")
	now := time.Date(2026, 9, 28, 1, 30, 0, 0, time.UTC)
	in := rag.FeatureInput{Return1m: ptr(0.01)}

	for i := 0; i < 40; i++ { // all inside SnapshotRecencyGuard, identical vectors
		f.indexSnapshot(t, "7203", now.Add(-time.Duration(i)*time.Second), in)
	}
	older := map[time.Time]bool{}
	for j := 1; j <= 3; j++ {
		ts := now.Add(-rag.SnapshotRecencyGuard - time.Duration(j)*time.Minute)
		older[ts] = true
		f.indexSnapshot(t, "7203", ts, rag.FeatureInput{Return1m: ptr(0.02)}) // farther than the own rows
	}

	got, err := f.svc.Context(context.Background(), in, rag.Subject{Symbol: "7203", Timestamp: now}, 3)
	if err != nil {
		t.Fatalf("Context: %v", err)
	}
	if len(got.Cases) != 3 {
		t.Fatalf("Context().Cases = %+v, want the 3 snapshots older than the recency guard", got.Cases)
	}
	for _, c := range got.Cases {
		if !older[c.Timestamp] {
			t.Errorf("case %+v is within the recency guard, want it excluded", c)
		}
	}
}

func TestService_Context_DecisionsSurviveMoreOwnRowsThanTheMargin(t *testing.T) {
	f := newSubjectFixture(t, "7203")
	now := time.Date(2026, 9, 28, 1, 30, 0, 0, time.UTC)
	in := rag.FeatureInput{Return1m: ptr(0.01)}

	for i := 0; i < 45; i++ { // at/after the subject timestamp
		f.indexDecision(t, "7203", domain.JevDecisionTypeScout, now.Add(time.Duration(i)*time.Second), in)
	}
	earlier := f.indexDecision(t, "7203", domain.JevDecisionTypeScout, now.Add(-time.Hour), rag.FeatureInput{Return1m: ptr(0.02)})

	got, err := f.svc.Context(context.Background(), in, rag.Subject{Symbol: "7203", Timestamp: now}, 2)
	if err != nil {
		t.Fatalf("Context: %v", err)
	}
	if len(got.Cases) != 1 || !got.Cases[0].Timestamp.Equal(earlier.Timestamp) {
		t.Fatalf("Context().Cases = %+v, want only the earlier decision", got.Cases)
	}
}

// BenchmarkContext_LargeSnapshotHistory measures Context against a large
// vector history with the subject's recent same-symbol snapshots present
// (issue #528: the subject exclusion used to be an IN subquery that
// materialized nearly every snapshot row on every call).
func BenchmarkContext_LargeSnapshotHistory(b *testing.B) {
	const rows = 100_000
	db, err := sqlitedb.Open(filepath.Join(b.TempDir(), "pitha.db"))
	if err != nil {
		b.Fatalf("Open: %v", err)
	}
	b.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()

	inst, err := market.NewInstrumentRepository(db).Create(ctx, domain.Instrument{
		Symbol: "7203", Name: "Test", Market: "TSE Prime", IsActive: true,
	})
	if err != nil {
		b.Fatalf("Create instrument: %v", err)
	}
	snapshots := market.NewSnapshotRepository(db)
	svc := rag.NewService(db, judgement.NewDecisionRepository(db), snapshots)
	now := time.Date(2026, 9, 28, 1, 30, 0, 0, time.UTC)

	// Bulk history of another symbol: real market_snapshots rows (the old
	// subquery scanned them) plus their vectors, each in one transaction.
	peer, err := market.NewInstrumentRepository(db).Create(ctx, domain.Instrument{
		Symbol: "9433", Name: "Peer", Market: "TSE Prime", IsActive: true,
	})
	if err != nil {
		b.Fatalf("Create instrument: %v", err)
	}
	history := make([]domain.Snapshot, rows)
	for i := range history {
		history[i] = domain.Snapshot{
			InstrumentID: peer.ID, Symbol: "9433", Timestamp: now.Add(-time.Duration(i+60) * time.Minute),
			Price: 1000, Volume: 100, Turnover: 100_000, RawDataJSON: `{}`,
		}
	}
	saved, err := snapshots.InsertBatch(ctx, history)
	if err != nil {
		b.Fatalf("InsertBatch: %v", err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		b.Fatalf("begin: %v", err)
	}
	for i, snap := range saved {
		v := rag.Vector{float64(i%97) / 97, float64(i%89) / 89, float64(i%83) / 83}
		blob, _ := json.Marshal(v)
		if _, err := tx.ExecContext(ctx, `INSERT INTO market_snapshot_vectors (snapshot_id, embedding) VALUES (?, ?)`, snap.ID, string(blob)); err != nil {
			b.Fatalf("insert vector: %v", err)
		}
	}
	if err := tx.Commit(); err != nil {
		b.Fatalf("commit: %v", err)
	}
	in := rag.FeatureInput{Return1m: ptr(0.01)}
	for i := 0; i < 20; i++ { // the subject's own recent bars
		snap, err := snapshots.Insert(ctx, domain.Snapshot{
			InstrumentID: inst.ID, Symbol: "7203", Timestamp: now.Add(-time.Duration(i) * time.Minute),
			Price: 1000, Volume: 100, Turnover: 100_000, RawDataJSON: `{}`,
		})
		if err != nil {
			b.Fatalf("insert snapshot: %v", err)
		}
		if err := svc.IndexSnapshot(ctx, snap.ID, in); err != nil {
			b.Fatalf("IndexSnapshot: %v", err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := svc.Context(ctx, in, rag.Subject{Symbol: "7203", Timestamp: now}, rag.DefaultK); err != nil {
			b.Fatalf("Context: %v", err)
		}
	}
}
