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

	// The embedded db/migrations/000001_create_instruments_table.up.sql
	// migration must have been applied against the empty database file.
	var tableName string
	err = conn.QueryRow(
		"SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'instruments'",
	).Scan(&tableName)
	if err != nil {
		t.Fatalf("expected instruments table to exist after migration: %v", err)
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
