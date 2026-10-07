package sqlitedb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
)

// PeekRuntimeSetting reads one runtime_settings value straight from the
// database file at path, before (and without) Open: the connection is
// read-only, runs no migration and does not create the file. It exists for
// values needed before the application can open the database normally -
// the log directory, which the file logger needs ahead of bootstrap.Run
// (issue #708). A missing file, a missing table (a database that has not
// been migrated yet) and a missing key all report ok=false with a nil
// error; only a failing query on an existing table is an error.
func PeekRuntimeSetting(ctx context.Context, path, key string) (value string, ok bool, err error) {
	if _, statErr := os.Stat(path); statErr != nil {
		if errors.Is(statErr, os.ErrNotExist) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("repository: peek runtime setting %q: %w", key, statErr)
	}
	conn, err := sql.Open("sqlite", FileURI(path, "mode=ro&_busy_timeout=5000"))
	if err != nil {
		return "", false, fmt.Errorf("repository: peek runtime setting %q: open %q: %w", key, path, err)
	}
	defer func() { _ = conn.Close() }()
	conn.SetMaxOpenConns(1)

	err = conn.QueryRowContext(ctx, `SELECT value FROM runtime_settings WHERE key = ?`, key).Scan(&value)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return "", false, nil
	case err != nil && isNoSuchTable(err):
		return "", false, nil
	case err != nil:
		return "", false, fmt.Errorf("repository: peek runtime setting %q: %w", key, err)
	}
	return value, true, nil
}

func isNoSuchTable(err error) bool {
	return strings.Contains(err.Error(), "no such table")
}
