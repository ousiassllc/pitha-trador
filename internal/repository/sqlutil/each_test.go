package sqlutil_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

func scanInt(row sqlutil.RowScanner) (int, error) {
	var n int
	err := row.Scan(&n)
	return n, err
}

func openDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "pitha.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestEach_StreamsInOrderAndStopsOnCallbackError(t *testing.T) {
	db := openDB(t)
	const q = `WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < 5) SELECT i FROM n WHERE i >= ?`

	var got []int
	if err := sqlutil.Each(context.Background(), db, scanInt, func(n int) error { got = append(got, n); return nil }, q, 2); err != nil {
		t.Fatalf("Each: %v", err)
	}
	if len(got) != 4 || got[0] != 2 || got[3] != 5 {
		t.Fatalf("rows = %v, want 2..5 in order", got)
	}

	stop := errors.New("stop")
	calls := 0
	err := sqlutil.Each(context.Background(), db, scanInt, func(int) error { calls++; return stop }, q, 1)
	if !errors.Is(err, stop) || calls != 1 {
		t.Fatalf("err = %v after %d calls, want stop after the first row", err, calls)
	}

	// The rows were closed on the early return: the single-connection-safe
	// DB can run another query.
	if err := sqlutil.Each(context.Background(), db, scanInt, func(int) error { return nil }, q, 1); err != nil {
		t.Fatalf("Each after early stop: %v", err)
	}
}

func TestEach_ScanErrorIsReturned(t *testing.T) {
	db := openDB(t)
	scanFail := func(sqlutil.RowScanner) (int, error) { return 0, errors.New("bad row") }
	if err := sqlutil.Each(context.Background(), db, scanFail, func(int) error { return nil }, `SELECT 1`); err == nil {
		t.Fatal("want the scan error")
	}
}
