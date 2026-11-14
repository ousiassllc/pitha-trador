package migratebundle

import (
	"context"
	"database/sql"
	"fmt"
	"sort"

	"github.com/ousiassllc/pitha-trador/internal/logging"
)

// secretBearing reports whether a runtime_settings row must not leave the PC:
// a key the log/export masking rules treat as a secret, or a value they would
// rewrite (a webhook URL, a Bearer token, a PEM block ... pasted into any
// setting). Today's settings hold none, so this is a guard for the day one
// does; the dropped key is listed in the manifest, never silent.
func secretBearing(key, value string) bool {
	return logging.IsSensitiveKey(key) || logging.MaskJSONText(value) != value
}

// pruneSettings is the backup.Prune of the bundle's database copy: it deletes
// the excluded and secret-bearing runtime_settings rows and returns the keys
// it deleted, sorted.
func pruneSettings(ctx context.Context, conn *sql.DB) ([]string, error) {
	rows, err := conn.QueryContext(ctx, `SELECT key, value FROM runtime_settings`)
	if err != nil {
		return nil, fmt.Errorf("migratebundle: read runtime settings: %w", err)
	}
	var drop []string
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("migratebundle: read runtime settings: %w", err)
		}
		if isExcludedKey(key) || secretBearing(key, value) {
			drop = append(drop, key)
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, fmt.Errorf("migratebundle: read runtime settings: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("migratebundle: read runtime settings: %w", err)
	}
	for _, key := range drop {
		if _, err := conn.ExecContext(ctx, `DELETE FROM runtime_settings WHERE key = ?`, key); err != nil {
			return nil, fmt.Errorf("migratebundle: drop runtime setting %q: %w", key, err)
		}
	}
	sort.Strings(drop)
	return drop, nil
}

// checkSecretFree is the import-side counterpart of the export's scrub: the
// bundle's database must hold no secrets row and no excluded or
// secret-bearing runtime_settings row. A bundle that does (hand-edited, or
// made by something else) is refused with ErrNotSecretFree.
func checkSecretFree(ctx context.Context, conn *sql.DB) error {
	var n int
	if err := conn.QueryRowContext(ctx, `SELECT count(*) FROM secrets`).Scan(&n); err != nil {
		return fmt.Errorf("%w: read secrets: %v", ErrCorrupt, err)
	}
	if n != 0 {
		return fmt.Errorf("%w: the secrets table has %d rows", ErrNotSecretFree, n)
	}
	rows, err := conn.QueryContext(ctx, `SELECT key, value FROM runtime_settings`)
	if err != nil {
		return fmt.Errorf("%w: read runtime settings: %v", ErrCorrupt, err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return fmt.Errorf("%w: read runtime settings: %v", ErrCorrupt, err)
		}
		if isExcludedKey(key) || secretBearing(key, value) {
			return fmt.Errorf("%w: runtime setting %q", ErrNotSecretFree, key)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("%w: read runtime settings: %v", ErrCorrupt, err)
	}
	return nil
}
