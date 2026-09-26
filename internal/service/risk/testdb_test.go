package risk_test

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/repository"
)

// newTestDB opens a fresh, fully migrated SQLite database in t's
// temporary directory and registers it to close when t completes
// (mirrors internal/repository's own repository_test.newTestDB, which is
// unexported and so not reusable from this package).
func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := repository.Open(filepath.Join(t.TempDir(), "risk_test.db"))
	if err != nil {
		t.Fatalf("repository.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}
