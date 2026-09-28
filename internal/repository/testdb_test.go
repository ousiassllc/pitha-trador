package repository_test

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/repository"
)

// newTestDB opens a fresh, fully migrated SQLite database in a temporary
// directory for a single test, closing it on cleanup.
func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	conn, err := repository.Open(filepath.Join(t.TempDir(), "pitha.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if err := conn.Close(); err != nil {
			t.Errorf("close conn: %v", err)
		}
	})
	return conn
}

func sectorPtr(s string) *string { return &s }

func floatPtr(f float64) *float64 { return &f }
