package db_test

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	migratesqlite "github.com/golang-migrate/migrate/v4/database/sqlite"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	_ "modernc.org/sqlite"

	"github.com/ousiassllc/pitha-trador/db"
)

// insertProposalSQL is a valid open_long proposal row;
// the tests change one column at a time with UPDATE or a different INSERT.
const insertProposalSQL = `INSERT INTO order_proposals
	(idempotency_key, request_hash, received_at, symbol, bucket, action, quantity, order_type, stop_loss_price,
	 exit_plan, max_loss_jpy, rationale, sources_json, expires_at, updated_at)
	VALUES ('key-00000001', 'h', 't', '7203', 'stock', 'open_long', 100, 'MARKET', 900, 'plan', 10000, 'why', '[]', 't', 't')`

// Migration 000042 (issue #827) creates the order proposal tables. Paper is
// the only mode a row can hold, the event table is append-only, and the
// mcp_settings_audit rebuild keeps its rows while admitting the two order
// proposal switch actions; the down migration drops the tables and those rows.
func TestMigration000042_OrderProposalTablesAndAuditActions(t *testing.T) {
	conn, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "m.db")+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	src, err := iofs.New(db.MigrationsFS, "migrations")
	if err != nil {
		t.Fatalf("iofs: %v", err)
	}
	drv, err := migratesqlite.WithInstance(conn, &migratesqlite.Config{})
	if err != nil {
		t.Fatalf("driver: %v", err)
	}
	m, err := migrate.NewWithInstance("iofs", src, "sqlite", drv)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}

	if err := m.Migrate(41); err != nil {
		t.Fatalf("migrate to 41: %v", err)
	}
	mustExec(t, conn, `INSERT INTO mcp_settings_audit (at, actor, action, setting_key, after_value) VALUES ('t', 'mcp', 'set', 'risk.paper.max_trade_loss_pct', '2')`)
	if _, err := conn.Exec(`INSERT INTO mcp_settings_audit (at, actor, action, setting_key) VALUES ('t', 'gui', 'order_proposals_enable', 'mcp.order_proposals_enabled')`); err == nil {
		t.Fatal("order_proposals_enable accepted before migration 42")
	}

	if err := m.Migrate(42); err != nil {
		t.Fatalf("migrate to 42: %v", err)
	}
	var key string
	if err := conn.QueryRow(`SELECT setting_key FROM mcp_settings_audit WHERE id = 1`).Scan(&key); err != nil || key != "risk.paper.max_trade_loss_pct" {
		t.Fatalf("audit row after the rebuild: %q, %v", key, err)
	}
	mustExec(t, conn, `INSERT INTO mcp_settings_audit (at, actor, action, setting_key) VALUES ('t', 'gui', 'order_proposals_enable', 'mcp.order_proposals_enabled')`)
	mustExec(t, conn, `INSERT INTO mcp_settings_audit (at, actor, action, setting_key) VALUES ('t', 'mcp', 'order_proposals_disable', 'mcp.order_proposals_enabled')`)
	if _, err := conn.Exec(`INSERT INTO mcp_settings_audit (at, actor, action, setting_key) VALUES ('t', 'mcp', 'order_proposals_toggle', 'k')`); err == nil {
		t.Error("an unknown audit action was accepted")
	}

	// A proposal row: Paper is the only mode, and the defaults are received / empty.
	mustExec(t, conn, insertProposalSQL)
	var mode, status, reason string
	if err := conn.QueryRow(`SELECT mode, status, status_reason FROM order_proposals`).Scan(&mode, &status, &reason); err != nil || mode != "paper" || status != "received" || reason != "" {
		t.Fatalf("defaults: mode=%q status=%q reason=%q err=%v", mode, status, reason, err)
	}
	for _, mode := range []string{"live", "small_live", "PAPER", ""} {
		if _, err := conn.Exec(`UPDATE order_proposals SET mode = '` + mode + `'`); err == nil {
			t.Errorf("order_proposals.mode accepted %q, want only 'paper'", mode)
		}
		if _, err := conn.Exec(`INSERT INTO order_proposals
			(idempotency_key, request_hash, received_at, mode, symbol, bucket, action, order_type, rationale, sources_json, expires_at, updated_at)
			VALUES ('key-` + mode + `', 'h', 't', '` + mode + `', '7203', 'stock', 'close', 'MARKET', 'why', '[]', 't', 't')`); err == nil {
			t.Errorf("an order_proposals row with mode %q was inserted", mode)
		}
	}
	for _, bad := range []string{
		`UPDATE order_proposals SET bucket = 'crypto'`, `UPDATE order_proposals SET action = 'open_short'`,
		`UPDATE order_proposals SET order_type = 'STOP'`, `UPDATE order_proposals SET status = 'done'`,
		`UPDATE order_proposals SET quantity = 0`, `UPDATE order_proposals SET max_loss_jpy = 0`,
	} {
		if _, err := conn.Exec(bad); err == nil {
			t.Errorf("%q succeeded, want the CHECK to reject it", bad)
		}
	}
	if _, err := conn.Exec(insertProposalSQL); err == nil {
		t.Error("a second row with the same idempotency_key was inserted")
	}

	// Events are append-only.
	mustExec(t, conn, `INSERT INTO order_proposal_events (proposal_id, at, from_status, to_status, actor) VALUES (1, 't', NULL, 'received', 'mcp')`)
	for _, bad := range []string{
		`UPDATE order_proposal_events SET to_status = 'rejected'`, `DELETE FROM order_proposal_events`,
		`INSERT INTO order_proposal_events (proposal_id, at, to_status, actor) VALUES (1, 't', 'received', 'bot')`,
		`INSERT INTO order_proposal_events (proposal_id, at, to_status, actor) VALUES (999, 't', 'received', 'mcp')`,
	} {
		if _, err := conn.Exec(bad); err == nil {
			t.Errorf("%q succeeded", bad)
		}
	}

	if err := m.Migrate(41); err != nil {
		t.Fatalf("migrate down to 41: %v", err)
	}
	var rows int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM mcp_settings_audit`).Scan(&rows); err != nil || rows != 1 {
		t.Fatalf("audit rows after the down migration = %d (err %v), want only the original 'set' row", rows, err)
	}
	if _, err := conn.Exec(`SELECT 1 FROM order_proposals`); err == nil {
		t.Error("order_proposals still exists after the down migration")
	}
}
