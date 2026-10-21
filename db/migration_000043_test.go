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

// Migration 000043 (issue #828) adds what the application records while it
// processes a proposal: the cancellation request, the quantity actually
// ordered, the maximum loss it computed and the stop that estimate used, plus
// the events' detail. A proposal and its event that existed before keep their
// rows; the down migration takes the columns away again.
func TestMigration000043_OrderProposalProcessingColumns(t *testing.T) {
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

	if err := m.Migrate(42); err != nil {
		t.Fatalf("migrate to 42: %v", err)
	}
	mustExec(t, conn, insertProposalSQL)
	mustExec(t, conn, `INSERT INTO order_proposal_events (proposal_id, at, from_status, to_status, actor) VALUES (1, 't', NULL, 'received', 'mcp')`)

	if err := m.Migrate(43); err != nil {
		t.Fatalf("migrate to 43: %v", err)
	}
	var cancel sql.NullString
	var applied sql.NullInt64
	var detail string
	if err := conn.QueryRow(`SELECT cancel_requested_at, applied_quantity FROM order_proposals`).Scan(&cancel, &applied); err != nil || cancel.Valid || applied.Valid {
		t.Fatalf("new proposal columns = (%v, %v), err %v; want NULL for the existing row", cancel, applied, err)
	}
	if err := conn.QueryRow(`SELECT detail_json FROM order_proposal_events`).Scan(&detail); err != nil || detail != "" {
		t.Fatalf("detail_json of the existing event = %q, err %v; want empty", detail, err)
	}
	mustExec(t, conn, `UPDATE order_proposals SET cancel_requested_at = 't', applied_quantity = 100, computed_max_loss_jpy = 0, effective_stop_loss_price = 900`)
	for _, bad := range []string{
		`UPDATE order_proposals SET applied_quantity = 0`, `UPDATE order_proposals SET computed_max_loss_jpy = -1`,
		`UPDATE order_proposals SET effective_stop_loss_price = 0`,
	} {
		if _, err := conn.Exec(bad); err == nil {
			t.Errorf("%q succeeded, want the CHECK to reject it", bad)
		}
	}
	// The events table stays append-only after the new column.
	if _, err := conn.Exec(`UPDATE order_proposal_events SET detail_json = '{}'`); err == nil {
		t.Error("an order_proposal_events row was updated")
	}

	if err := m.Migrate(42); err != nil {
		t.Fatalf("migrate down to 42: %v", err)
	}
	if _, err := conn.Exec(`SELECT cancel_requested_at FROM order_proposals`); err == nil {
		t.Error("cancel_requested_at still exists after the down migration")
	}
	var n int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM order_proposals`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("proposals after the down migration = %d (%v), want the row kept", n, err)
	}
}
