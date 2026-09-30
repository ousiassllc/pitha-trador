package repoportfolio_test

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
)

// newTestDB opens a fresh, fully migrated SQLite database in t's
// temporary directory, closed when t completes.
func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "repoportfolio_test.db"))
	if err != nil {
		t.Fatalf("sqlitedb.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}
