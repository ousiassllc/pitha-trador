package migratebundle

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// CountTables returns the row count of every table of conn's database,
// enumerated from sqlite_master and sorted by name: the data tables, the
// secrets table (so a caller can show it is empty) and the virtual (vector)
// tables. Left out: SQLite's own tables, golang-migrate's schema_migrations
// (the schema version is compared separately) and the shadow tables a
// virtual table keeps for itself (their rows are an implementation detail of
// the virtual table, which is counted as one).
func CountTables(ctx context.Context, conn *sql.DB) ([]TableCount, error) {
	rows, err := conn.QueryContext(ctx, `SELECT name, sql FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite\_%' ESCAPE '\' ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("migratebundle: list tables: %w", err)
	}
	var names, virtual []string
	for rows.Next() {
		var name string
		var ddl sql.NullString
		if err := rows.Scan(&name, &ddl); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("migratebundle: list tables: %w", err)
		}
		names = append(names, name)
		if strings.HasPrefix(strings.ToUpper(ddl.String), "CREATE VIRTUAL TABLE") {
			virtual = append(virtual, name)
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, fmt.Errorf("migratebundle: list tables: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("migratebundle: list tables: %w", err)
	}

	var out []TableCount
	for _, name := range names {
		if name == "schema_migrations" || isShadowOf(name, virtual) {
			continue
		}
		var n int64
		if err := conn.QueryRowContext(ctx, `SELECT count(*) FROM "`+strings.ReplaceAll(name, `"`, `""`)+`"`).Scan(&n); err != nil {
			return nil, fmt.Errorf("migratebundle: count %s: %w", name, err)
		}
		out = append(out, TableCount{Name: name, Rows: n})
	}
	return out, nil
}

// isShadowOf reports whether name is "<virtual table>_<suffix>".
func isShadowOf(name string, virtual []string) bool {
	for _, v := range virtual {
		if strings.HasPrefix(name, v+"_") {
			return true
		}
	}
	return false
}
