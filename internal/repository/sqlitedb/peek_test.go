package sqlitedb_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
)

func TestPeekRuntimeSetting_MissingDatabaseTableAndKeyAreNotFound(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	missing := filepath.Join(dir, "absent.db")
	if _, ok, err := sqlitedb.PeekRuntimeSetting(ctx, missing, "system.log_dir"); err != nil || ok {
		t.Fatalf("missing file: ok = %v, err = %v, want not found", ok, err)
	}
	if _, err := os.Stat(missing); err == nil {
		t.Fatal("PeekRuntimeSetting created the database file")
	}

	unmigrated := filepath.Join(dir, "empty.db")
	if err := os.WriteFile(unmigrated, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := sqlitedb.PeekRuntimeSetting(ctx, unmigrated, "system.log_dir"); err != nil || ok {
		t.Fatalf("unmigrated file: ok = %v, err = %v, want not found", ok, err)
	}

	migrated := filepath.Join(dir, "pitha.db")
	conn, err := sqlitedb.Open(migrated)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = conn.Close() }()
	if _, ok, err := sqlitedb.PeekRuntimeSetting(ctx, migrated, "system.log_dir"); err != nil || ok {
		t.Fatalf("missing key: ok = %v, err = %v, want not found", ok, err)
	}
}

func TestPeekRuntimeSetting_ReadsStoredValueWhileDatabaseIsOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dir with #?%", "pitha.db")
	conn, err := sqlitedb.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.Exec(`INSERT INTO runtime_settings (key, value, updated_at) VALUES ('system.log_dir', '"/var/log/pitha"', '2026-10-08T00:00:00Z')`); err != nil {
		t.Fatalf("insert: %v", err)
	}

	got, ok, err := sqlitedb.PeekRuntimeSetting(context.Background(), path, "system.log_dir")
	if err != nil || !ok || got != `"/var/log/pitha"` {
		t.Fatalf("PeekRuntimeSetting = (%q, %v, %v), want the stored JSON string", got, ok, err)
	}
}
