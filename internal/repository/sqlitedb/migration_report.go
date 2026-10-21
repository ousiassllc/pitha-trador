package sqlitedb

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
)

// dataMigration is a migration that rewrites rows and keeps their old values in
// a backup table, whose row count is how many rows it changed. SQL cannot log,
// so Open reports the count right after it applied such a migration.
type dataMigration struct {
	version uint   // the migration's number
	rows    string // what it rewrote, for the log line
	table   string // the backup table, one row per rewritten row
}

var dataMigrations = []dataMigration{
	// Issue #860: a proposal's signal that passed Risk but never became an order.
	{version: 45, rows: "trade_signals", table: "trade_signal_rewrites_000045"},
}

// reportDataMigrations logs, for every dataMigration this Up applied (the
// schema went from before to after, so it runs once: on the start that applied
// it, never on a later one), one line with the number of rows it rewrote.
func reportDataMigrations(conn *sql.DB, before, after uint) {
	for _, m := range dataMigrations {
		if before >= m.version || after < m.version {
			continue
		}
		var n int
		if err := conn.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM `+m.table).Scan(&n); err != nil {
			slog.Warn(fmt.Sprintf("migration %06d: could not count the rows it rewrote", m.version), "table", m.table, "error", err)
			continue
		}
		slog.Info(fmt.Sprintf("migration %06d: rewrote %d %s (退避テーブル %s)", m.version, n, m.rows, m.table))
	}
}
