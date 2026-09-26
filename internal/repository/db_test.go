package repository_test

import (
	"path/filepath"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/repository"
)

func TestOpen_AppliesMigrationsAndEnablesRequiredPragmas(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "pitha.db")

	conn, err := repository.Open(dbPath)
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
	// §instruments, §market_snapshots, §jobs).
	for _, table := range []string{"instruments", "market_snapshots", "jobs"} {
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

	conn, err := repository.Open(dbPath)
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

	first, err := repository.Open(dbPath)
	if err != nil {
		t.Fatalf("first Open(%q) returned error: %v", dbPath, err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close first connection: %v", err)
	}

	// Re-opening an already-migrated database file must not fail (no
	// duplicate-table errors from re-running the up migration).
	second, err := repository.Open(dbPath)
	if err != nil {
		t.Fatalf("second Open(%q) returned error: %v", dbPath, err)
	}
	if err := second.Close(); err != nil {
		t.Fatalf("close second connection: %v", err)
	}
}
