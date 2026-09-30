package risk_test

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
)

// newTestDB opens a fresh, fully migrated SQLite database in t's
// temporary directory and registers it to close when t completes
// (mirrors internal/repository/*'s own test-package newTestDB, which is
// unexported and so not reusable from this package).
func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "risk_test.db"))
	if err != nil {
		t.Fatalf("sqlitedb.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}
