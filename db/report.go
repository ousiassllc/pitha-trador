package db

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
	// leftover counts the rows the migration found but could not safely
	// rewrite (empty: none); leftoverText says what they are.
	leftover, leftoverText string
}

var dataMigrations = []dataMigration{
	// Issue #860: a proposal's signal that passed Risk but never became an order.
	// The leftovers are mcp-proposal-v1 signals still risk_passed = 1 with no
	// order and no proposal pointing at them (see the migration's SQL).
	{
		version: 45, rows: "trade_signals", table: "trade_signal_rewrites_000045",
		leftover: `SELECT COUNT(*) FROM trade_signals ts WHERE ts.policy_version = 'mcp-proposal-v1' AND ts.risk_passed = 1
			AND NOT EXISTS (SELECT 1 FROM paper_orders o WHERE o.trade_signal_id = ts.id)
			AND NOT EXISTS (SELECT 1 FROM order_proposals op WHERE op.trade_signal_id = ts.id)`,
		leftoverText: "案に辿れず書き換えなかった孤立 signal",
	},
}

// ReportDataMigrations logs, for every dataMigration this Up applied (the
// schema went from before to after, so it runs once: on the start that applied
// it, never on a later one), one line with the number of rows it rewrote and,
// when it has any, the rows it left alone.
func ReportDataMigrations(conn *sql.DB, before, after uint) {
	ctx := context.Background()
	for _, m := range dataMigrations {
		if before >= m.version || after < m.version {
			continue
		}
		var n, left int
		err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+m.table).Scan(&n)
		if err == nil && m.leftover != "" {
			err = conn.QueryRowContext(ctx, m.leftover).Scan(&left)
		}
		if err != nil {
			slog.Warn(fmt.Sprintf("migration %06d: could not count the rows it rewrote", m.version), "table", m.table, "error", err)
			continue
		}
		line := fmt.Sprintf("migration %06d: rewrote %d %s (退避テーブル %s)", m.version, n, m.rows, m.table)
		if m.leftover != "" {
			line += fmt.Sprintf("、%s %d 件", m.leftoverText, left)
		}
		slog.Info(line)
	}
}
