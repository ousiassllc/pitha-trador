// Package maintenanceflow_test holds the Scheduler maintenance-job tests
// (database backup, expired-data purge, log rotation). They only use
// scheduler's exported API and live in their own directory to keep
// internal/service/scheduler under the linterly line budget (#248). The
// helper below is this package's own (sibling test packages do not import
// each other).
package maintenanceflow_test

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
)

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "pitha_test.db"))
	if err != nil {
		t.Fatalf("sqlitedb.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}
