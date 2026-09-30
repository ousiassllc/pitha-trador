package system_test

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	migratesqlite "github.com/golang-migrate/migrate/v4/database/sqlite"
	"github.com/golang-migrate/migrate/v4/source/iofs"

	"github.com/ousiassllc/pitha-trador/db"
)

// Migration 000014 must carry already-resolved rows over to
// kill_switch_resolutions so history survives the cutover, and its down
// migration must restore them.
func TestMigration000014_PreservesResolvedKillSwitchEvents(t *testing.T) {
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
	if err := m.Migrate(13); err != nil {
		t.Fatalf("migrate to 13: %v", err)
	}

	for _, stmt := range []string{
		`INSERT INTO kill_switch_events (triggered_at, reason, detail_json, resolved_at, resolved_by)
		 VALUES ('2026-09-27T10:00:00Z', 'daily_loss_limit', '{}', '2026-09-27T11:00:00Z', 'manual')`,
		`INSERT INTO kill_switch_events (triggered_at, reason, detail_json)
		 VALUES ('2026-09-27T12:00:00Z', 'jev_api_down', '{}')`,
	} {
		if _, err := conn.Exec(stmt); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	if err := m.Migrate(14); err != nil {
		t.Fatalf("migrate to 14: %v", err)
	}
	var n int
	var by, at string
	if err := conn.QueryRow(`SELECT COUNT(*) FROM kill_switch_resolutions`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("resolutions count = %d, err = %v, want 1", n, err)
	}
	if err := conn.QueryRow(`SELECT resolved_at, resolved_by FROM kill_switch_resolutions WHERE kill_switch_event_id = 1`).Scan(&at, &by); err != nil {
		t.Fatalf("read migrated resolution: %v", err)
	}
	if at != "2026-09-27T11:00:00Z" || by != "manual" {
		t.Fatalf("migrated resolution = %q/%q", at, by)
	}

	if err := m.Migrate(13); err != nil {
		t.Fatalf("migrate down to 13: %v", err)
	}
	if err := conn.QueryRow(`SELECT resolved_at, resolved_by FROM kill_switch_events WHERE id = 1`).Scan(&at, &by); err != nil {
		t.Fatalf("read restored columns: %v", err)
	}
	if at != "2026-09-27T11:00:00Z" || by != "manual" {
		t.Fatalf("restored resolution = %q/%q", at, by)
	}
	var open sql.NullString
	if err := conn.QueryRow(`SELECT resolved_at FROM kill_switch_events WHERE id = 2`).Scan(&open); err != nil || open.Valid {
		t.Fatalf("unresolved event resolved_at = %v, err = %v, want NULL", open, err)
	}
}
