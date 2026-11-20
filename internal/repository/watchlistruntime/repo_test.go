package watchlistruntime_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/repository/watchlistruntime"
)

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	conn, err := sqlitedb.Open(filepath.Join(t.TempDir(), "pitha.db"))
	if err != nil {
		t.Fatalf("sqlitedb.Open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func TestRepository_RecordRuntimeSizeUpdatesTheDayInPlace(t *testing.T) {
	ctx := context.Background()
	conn := newTestDB(t)
	repo := watchlistruntime.New(conn)
	base := time.Date(2026, 10, 21, 1, 0, 0, 0, time.UTC)
	for i, size := range []int{0, 12, 45, 30} {
		if err := repo.RecordRuntimeSize(ctx, "2026-10-21", "ranking", size, base.Add(time.Duration(i)*time.Minute)); err != nil {
			t.Fatalf("RecordRuntimeSize %d: %v", size, err)
		}
	}
	if err := repo.RecordRuntimeSize(ctx, "2026-10-22", "ranking", 7, base.AddDate(0, 0, 1)); err != nil {
		t.Fatal(err)
	}

	var n int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM watch_list_runtime_days`).Scan(&n); err != nil || n != 2 {
		t.Fatalf("rows = %d (%v), want one per day", n, err)
	}
	var entries, peak, samples int
	if err := conn.QueryRow(`SELECT entries, peak_entries, samples FROM watch_list_runtime_days WHERE session_date = '2026-10-21'`).
		Scan(&entries, &peak, &samples); err != nil {
		t.Fatal(err)
	}
	if entries != 30 || peak != 45 || samples != 4 {
		t.Errorf("entries/peak/samples = %d/%d/%d, want 30/45/4 (the latest, the largest, the count)", entries, peak, samples)
	}
	// The 立花 saved lists are a different table and stay untouched.
	if err := conn.QueryRow(`SELECT COUNT(*) FROM watch_lists`).Scan(&n); err != nil || n != 0 {
		t.Errorf("watch_lists rows = %d (%v), want 0", n, err)
	}
}
