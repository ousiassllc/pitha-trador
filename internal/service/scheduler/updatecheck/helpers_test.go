package updatecheck_test

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
)

// newTestDB is this package's own copy of the helper (sibling test packages
// do not import each other); the Scheduler update-checker tests were moved
// here to keep internal/service/scheduler under the linterly line budget (#400).
func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "pitha_test.db"))
	if err != nil {
		t.Fatalf("sqlitedb.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}
