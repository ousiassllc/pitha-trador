package db_test

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	migratesqlite "github.com/golang-migrate/migrate/v4/database/sqlite"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	_ "modernc.org/sqlite"
	_ "modernc.org/sqlite/vec"

	"github.com/ousiassllc/pitha-trador/db"
)

// Migration 000017 rebuilds kill_switch_events/kill_switch_resolutions to
// admit 'operator_manual': history and links must survive, the append-only
// triggers must be back, and the down migration must drop only the rows the
// old CHECK cannot hold.
func TestMigration000017_AddsOperatorManualReasonKeepingHistoryAndTriggers(t *testing.T) {
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
	exec := func(stmt string) {
		t.Helper()
		if _, err := conn.Exec(stmt); err != nil {
			t.Fatalf("exec %q: %v", stmt, err)
		}
	}
	if err := m.Migrate(16); err != nil {
		t.Fatalf("migrate to 16: %v", err)
	}
	exec(`INSERT INTO kill_switch_events (triggered_at, reason, detail_json) VALUES ('2026-09-27T10:00:00Z', 'daily_loss_limit', '{}')`)
	exec(`INSERT INTO kill_switch_resolutions (kill_switch_event_id, resolved_at, resolved_by) VALUES (1, '2026-09-27T11:00:00Z', 'manual')`)
	if _, err := conn.Exec(`INSERT INTO kill_switch_events (triggered_at, reason, detail_json) VALUES ('2026-09-27T12:00:00Z', 'operator_manual', '{}')`); err == nil {
		t.Fatalf("operator_manual accepted before migration 17")
	}

	if err := m.Migrate(17); err != nil {
		t.Fatalf("migrate to 17: %v", err)
	}
	exec(`INSERT INTO kill_switch_events (triggered_at, reason, detail_json) VALUES ('2026-09-27T12:00:00Z', 'operator_manual', '{}')`)
	exec(`INSERT INTO kill_switch_resolutions (kill_switch_event_id, resolved_at, resolved_by) VALUES (2, '2026-09-27T12:30:00Z', 'manual')`)

	var by string
	if err := conn.QueryRow(`SELECT resolved_by FROM kill_switch_resolutions WHERE kill_switch_event_id = 1`).Scan(&by); err != nil || by != "manual" {
		t.Fatalf("carried-over resolution = %q, err = %v", by, err)
	}
	for _, stmt := range []string{
		`UPDATE kill_switch_events SET reason = 'jev_api_down' WHERE id = 1`,
		`DELETE FROM kill_switch_events WHERE id = 1`,
		`UPDATE kill_switch_resolutions SET resolved_by = 'auto' WHERE id = 1`,
		`DELETE FROM kill_switch_resolutions WHERE id = 1`,
	} {
		if _, err := conn.Exec(stmt); err == nil {
			t.Fatalf("append-only trigger missing after migration 17: %s succeeded", stmt)
		}
	}
	if _, err := conn.Exec(`INSERT INTO kill_switch_resolutions (kill_switch_event_id, resolved_at, resolved_by) VALUES (99, '2026-09-27T12:30:00Z', 'manual')`); err == nil {
		t.Fatalf("resolution for unknown event accepted: foreign key lost")
	}

	if err := m.Migrate(16); err != nil {
		t.Fatalf("migrate down to 16: %v", err)
	}
	var n int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM kill_switch_events`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("events after down = %d, err = %v, want 1 (operator_manual dropped)", n, err)
	}
	if err := conn.QueryRow(`SELECT COUNT(*) FROM kill_switch_resolutions`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("resolutions after down = %d, err = %v, want 1", n, err)
	}
}
