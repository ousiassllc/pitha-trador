package backup

import (
	"context"
	"database/sql"
	"log/slog"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
)

// Prune removes more rows from a database copy while it is being scrubbed;
// it runs on the copy's connection after the secrets table was emptied and
// before the file is VACUUMed.
type Prune func(ctx context.Context, copy *sql.DB) error

// CopyScrubbed writes a consistent single-snapshot copy of db to dst
// (sqlitedb.BackupTo: WAL checkpoint + VACUUM INTO, safe while the
// application keeps writing), empties its secrets table (plus whatever prune
// removes, may be nil), rewrites the file so the deleted values do not linger
// in free pages and verifies it with PRAGMA integrity_check. dst must be an
// empty or missing file in an existing directory; on failure the caller
// removes it. The daily backup and the PC migration bundle both use it, so
// "a copy that leaves the application" is scrubbed in exactly one place.
func CopyScrubbed(ctx context.Context, db *sql.DB, dst string, prune Prune) error {
	busy, err := sqlitedb.BackupTo(ctx, db, dst)
	if busy {
		slog.Warn("backup: wal checkpoint could not fully truncate the WAL; the copy is still a consistent snapshot")
	}
	if err == nil {
		err = scrubSecrets(ctx, dst, prune)
	}
	if err == nil {
		err = VerifyCopy(ctx, dst)
	}
	return err
}
