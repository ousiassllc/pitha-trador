package snapshotarchive

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
)

// fixedNow is the archiver's clock in tests.
var fixedNow = time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)

func newArchiver(t *testing.T) (*Archiver, *sql.DB, int64) {
	t.Helper()
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "pitha.db"))
	if err != nil {
		t.Fatalf("sqlitedb.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	inst, err := market.NewInstrumentRepository(db).Create(context.Background(), domain.Instrument{
		Symbol: "7203", Name: "Toyota", Market: "TSE Prime", IsActive: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	a := New(db, filepath.Join(t.TempDir(), "snapshot_archive"))
	a.now = func() time.Time { return fixedNow }
	return a, db, inst.ID
}

// insertSnapshots inserts n one-minute bars from start and returns their ids.
func insertSnapshots(t *testing.T, db *sql.DB, instrumentID int64, start time.Time, n int) []int64 {
	t.Helper()
	var ids []int64
	for i := range n {
		lendable := i%2 == 0
		bid := 99.5 + float64(i)
		s, err := market.NewSnapshotRepository(db).Insert(context.Background(), domain.Snapshot{
			InstrumentID: instrumentID, Symbol: "7203", Timestamp: start.Add(time.Duration(i) * time.Minute),
			Price: 100 + float64(i)/10, Bid: &bid, Volume: int64(i + 1), Turnover: 100,
			Lendable: &lendable, RawDataJSON: `{"note":"a,b \"quoted\"\nline"}`,
			Feature: domain.Feature{VWAP: 100},
		})
		if err != nil {
			t.Fatalf("insert snapshot: %v", err)
		}
		ids = append(ids, s.ID)
	}
	return ids
}

func countRows(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM market_snapshots`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func utc(y int, m time.Month, d, h, min int) time.Time {
	return time.Date(y, m, d, h, min, 0, 0, time.UTC)
}
