package migratebundle

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"strconv"
	"strings"
	"sync"

	"github.com/ousiassllc/pitha-trador/db"
)

var (
	latestOnce    sync.Once
	latestVersion uint
	latestErr     error
)

// LatestSchemaVersion is the newest golang-migrate version embedded in this
// build (the highest NNNNNN_*.up.sql under db/migrations): what an opened
// database is migrated to, and the newest schema an import accepts.
func LatestSchemaVersion() (uint, error) {
	latestOnce.Do(func() {
		entries, err := fs.ReadDir(db.MigrationsFS, "migrations")
		if err != nil {
			latestErr = fmt.Errorf("migratebundle: read embedded migrations: %w", err)
			return
		}
		for _, e := range entries {
			num, _, ok := strings.Cut(e.Name(), "_")
			if !ok || !strings.HasSuffix(e.Name(), ".up.sql") {
				continue
			}
			if v, err := strconv.ParseUint(num, 10, 32); err == nil && uint(v) > latestVersion {
				latestVersion = uint(v)
			}
		}
		if latestVersion == 0 {
			latestErr = errors.New("migratebundle: no embedded migrations")
		}
	})
	return latestVersion, latestErr
}

// SchemaVersion reads the golang-migrate version recorded in conn's database
// (0 for one that was never migrated). dirty is true when a migration was
// interrupted: such a database is not a trustworthy snapshot.
func SchemaVersion(ctx context.Context, conn *sql.DB) (version uint, dirty bool, err error) {
	err = conn.QueryRowContext(ctx, `SELECT version, dirty FROM schema_migrations LIMIT 1`).Scan(&version, &dirty)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return 0, false, nil
	case err != nil && strings.Contains(err.Error(), "no such table"):
		return 0, false, nil
	case err != nil:
		return 0, false, fmt.Errorf("migratebundle: read schema version: %w", err)
	}
	return version, dirty, nil
}
