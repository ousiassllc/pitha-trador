package retention

import (
	"database/sql"
	"log/slog"

	"github.com/ousiassllc/pitha-trador/internal/service/snapshotarchive"
)

// NewWithSnapshotArchive returns a Service that archives market_snapshots to
// snapshotarchive.DefaultDir of the database at dbPath (a folder on the same
// PC, never sent anywhere) before purging them.
func NewWithSnapshotArchive(db *sql.DB, policy Policy, dbPath string) *Service {
	dir := snapshotarchive.DefaultDir(dbPath)
	slog.Info("retention: market_snapshots are archived locally before deletion", "dir", dir)
	return New(db, policy, WithSnapshotArchiver(snapshotarchive.New(db, dir)))
}
