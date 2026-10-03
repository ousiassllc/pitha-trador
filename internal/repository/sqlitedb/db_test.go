package sqlitedb_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
)

func TestOpen_AppliesMigrationsAndEnablesRequiredPragmas(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "pitha.db")

	conn, err := sqlitedb.Open(dbPath)
	if err != nil {
		t.Fatalf("Open(%q) returned error: %v", dbPath, err)
	}
	t.Cleanup(func() {
		if err := conn.Close(); err != nil {
			t.Errorf("close conn: %v", err)
		}
	})

	var foreignKeys int
	if err := conn.QueryRow("PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
		t.Fatalf("query PRAGMA foreign_keys: %v", err)
	}
	if foreignKeys != 1 {
		t.Fatalf("expected PRAGMA foreign_keys=1, got %d", foreignKeys)
	}

	var journalMode string
	if err := conn.QueryRow("PRAGMA journal_mode").Scan(&journalMode); err != nil {
		t.Fatalf("query PRAGMA journal_mode: %v", err)
	}
	if journalMode != "wal" {
		t.Fatalf("expected PRAGMA journal_mode=wal, got %q", journalMode)
	}

	// The embedded db/migrations/*.up.sql migrations must have created
	// every table introduced so far (docs/architecture/er.md
	// §instruments, §market_snapshots, §jobs, §paper_orders, §positions,
	// §kill_switch_events, §runtime_settings, §policy_proposals).
	for _, table := range []string{
		"instruments", "market_snapshots", "jobs",
		"paper_orders", "positions", "kill_switch_events", "kill_switch_resolutions", "runtime_settings", "policy_proposals",
	} {
		var tableName string
		err = conn.QueryRow(
			"SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?", table,
		).Scan(&tableName)
		if err != nil {
			t.Fatalf("expected %s table to exist after migration: %v", table, err)
		}
	}
}

func TestOpen_JobsTableRejectsInvalidStatus(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "pitha.db")

	conn, err := sqlitedb.Open(dbPath)
	if err != nil {
		t.Fatalf("Open(%q) returned error: %v", dbPath, err)
	}
	t.Cleanup(func() {
		if err := conn.Close(); err != nil {
			t.Errorf("close conn: %v", err)
		}
	})

	// docs/architecture/er.md §jobs: status has a CHECK constraint
	// limiting it to pending|running|succeeded|failed.
	_, err = conn.Exec(
		`INSERT INTO jobs (queue, payload_json, status, scheduled_at) VALUES (?, ?, ?, ?)`,
		"market-data", "{}", "bogus-status", "2026-09-26T00:00:00Z",
	)
	if err == nil {
		t.Fatalf("insert with invalid jobs.status succeeded, want CHECK constraint violation")
	}
}

func TestOpen_IsIdempotentAcrossReopens(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "pitha.db")

	first, err := sqlitedb.Open(dbPath)
	if err != nil {
		t.Fatalf("first Open(%q) returned error: %v", dbPath, err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close first connection: %v", err)
	}

	// Re-opening an already-migrated database file must not fail (no
	// duplicate-table errors from re-running the up migration).
	second, err := sqlitedb.Open(dbPath)
	if err != nil {
		t.Fatalf("second Open(%q) returned error: %v", dbPath, err)
	}
	if err := second.Close(); err != nil {
		t.Fatalf("close second connection: %v", err)
	}
}

func TestOpen_PositionsTableAllowsOnlyOneOpenPositionPerInstrument(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "pitha.db")
	conn, err := sqlitedb.Open(dbPath)
	if err != nil {
		t.Fatalf("Open(%q) returned error: %v", dbPath, err)
	}
	t.Cleanup(func() {
		if err := conn.Close(); err != nil {
			t.Errorf("close conn: %v", err)
		}
	})

	if _, err := conn.Exec(`INSERT INTO instruments (symbol, name, market) VALUES ('AAPL', 'Apple Inc.', 'NASDAQ')`); err != nil {
		t.Fatalf("insert instrument: %v", err)
	}
	insertOrder := func() int64 {
		res, err := conn.Exec(`INSERT INTO paper_orders (instrument_id, symbol, side, order_type, quantity, status, submitted_at) VALUES (1, 'AAPL', 'BUY', 'MARKET', 10, 'FILLED', '2026-09-26T00:00:00Z')`)
		if err != nil {
			t.Fatalf("insert paper_orders: %v", err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			t.Fatalf("read paper_orders id: %v", err)
		}
		return id
	}
	insertPosition := func(entryOrderID int64, openedAt string) error {
		_, err := conn.Exec(`INSERT INTO positions (instrument_id, entry_order_id, symbol, side, quantity, entry_price, current_price, opened_at) VALUES (1, ?, 'AAPL', 'LONG', 10, 100.0, 100.0, ?)`, entryOrderID, openedAt)
		return err
	}

	firstOrderID := insertOrder()
	if err := insertPosition(firstOrderID, "2026-09-26T00:00:00Z"); err != nil {
		t.Fatalf("insert first open position: %v", err)
	}

	// docs/architecture/er.md §positions: `UNIQUE (instrument_id) WHERE
	// closed_at IS NULL` limits an instrument to one concurrently open
	// position.
	secondOrderID := insertOrder()
	if err := insertPosition(secondOrderID, "2026-09-26T01:00:00Z"); err == nil {
		t.Fatalf("insert of second concurrently open position succeeded, want partial UNIQUE index violation")
	}

	if _, err := conn.Exec(`UPDATE positions SET closed_at = '2026-09-26T02:00:00Z' WHERE entry_order_id = ?`, firstOrderID); err != nil {
		t.Fatalf("close first position: %v", err)
	}
	if err := insertPosition(secondOrderID, "2026-09-26T03:00:00Z"); err != nil {
		t.Fatalf("insert new open position after prior close: %v", err)
	}
}

func TestOpen_CreatesOwnerOnlyDatabaseFiles(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits are not enforced on Windows")
	}
	dir := filepath.Join(t.TempDir(), "nested", "pitha-trador")
	dbPath := filepath.Join(dir, "pitha.db")

	conn, err := sqlitedb.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	// Force a write so the -wal/-shm side files exist while the DB is open.
	if _, err := conn.Exec("CREATE TABLE perm_probe (id INTEGER)"); err != nil {
		t.Fatalf("write: %v", err)
	}

	assertMode(t, dir, 0o700)
	for _, suffix := range []string{"", "-wal", "-shm"} {
		assertMode(t, dbPath+suffix, 0o600)
	}
}

func TestOpen_TightensExistingWorldReadableDatabase(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits are not enforced on Windows")
	}
	dbPath := filepath.Join(t.TempDir(), "pitha.db")
	if err := os.WriteFile(dbPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dbPath, 0o644); err != nil {
		t.Fatal(err)
	}

	conn, err := sqlitedb.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	assertMode(t, dbPath, 0o600)
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %q: %v", path, err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Errorf("%q mode = %o, want %o", path, got, want)
	}
}
