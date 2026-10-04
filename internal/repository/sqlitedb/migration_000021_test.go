package sqlitedb_test

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	migratesqlite "github.com/golang-migrate/migrate/v4/database/sqlite"
	"github.com/golang-migrate/migrate/v4/source/iofs"

	"github.com/ousiassllc/pitha-trador/db"
)

// Migration 000021 must rewrite already-stored variable-width RFC3339Nano
// values to the fixed nine-digit fraction so lexicographic order equals
// chronological order for existing user databases (issue #430), without
// touching NULLs, already-normalized values or non-UTC spellings, and
// without losing the kill-switch tables' append-only triggers.
func TestMigration000021_NormalizesStoredTimesToFixedWidth(t *testing.T) {
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
	if err := m.Migrate(20); err != nil {
		t.Fatalf("migrate to 20: %v", err)
	}

	for _, stmt := range []string{
		`INSERT INTO jobs (queue, payload_json, status, scheduled_at, started_at, created_at)
		 VALUES ('q', '{}', 'running', '2026-10-05T01:00:05Z', '2026-10-05T01:00:05.5Z', '2026-10-05T01:00:05.51Z')`,
		`INSERT INTO jobs (queue, payload_json, status, scheduled_at, created_at)
		 VALUES ('q', '{}', 'pending', '2026-10-05T01:00:05.123456789Z', '2026-10-05T01:00:05.123Z')`,
		`INSERT INTO jobs (queue, payload_json, status, scheduled_at, created_at)
		 VALUES ('q', '{}', 'pending', '2026-10-05T10:00:05+09:00', '2026-10-05T01:00:05Z')`,
		`INSERT INTO kill_switch_events (triggered_at, reason, detail_json, created_at)
		 VALUES ('2026-10-05T01:00:05.25Z', 'daily_loss_limit', '{}', '2026-10-05T01:00:05.250Z')`,
		`INSERT INTO kill_switch_resolutions (kill_switch_event_id, resolved_at, resolved_by)
		 VALUES (1, '2026-10-05T02:00:00Z', 'manual')`,
	} {
		if _, err := conn.Exec(stmt); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	if err := m.Migrate(21); err != nil {
		t.Fatalf("migrate to 21: %v", err)
	}

	var sched, started, created string
	if err := conn.QueryRow(`SELECT scheduled_at, started_at, created_at FROM jobs WHERE id = 1`).Scan(&sched, &started, &created); err != nil {
		t.Fatalf("read job 1: %v", err)
	}
	if sched != "2026-10-05T01:00:05.000000000Z" || started != "2026-10-05T01:00:05.500000000Z" || created != "2026-10-05T01:00:05.510000000Z" {
		t.Fatalf("job 1 = %q/%q/%q, want fixed-width normalized values", sched, started, created)
	}
	// The normalized values must now order chronologically as text.
	var lt int
	if err := conn.QueryRow(`SELECT scheduled_at < started_at AND started_at < created_at FROM jobs WHERE id = 1`).Scan(&lt); err != nil || lt != 1 {
		t.Fatalf("string order of normalized values = %d, err = %v, want 1", lt, err)
	}

	var startedNull sql.NullString
	if err := conn.QueryRow(`SELECT scheduled_at, started_at, created_at FROM jobs WHERE id = 2`).Scan(&sched, &startedNull, &created); err != nil {
		t.Fatalf("read job 2: %v", err)
	}
	if sched != "2026-10-05T01:00:05.123456789Z" || startedNull.Valid || created != "2026-10-05T01:00:05.123000000Z" {
		t.Fatalf("job 2 = %q/%v/%q, want already-fixed value unchanged, NULL kept, ms value padded", sched, startedNull, created)
	}

	if err := conn.QueryRow(`SELECT scheduled_at, created_at FROM jobs WHERE id = 3`).Scan(&sched, &created); err != nil {
		t.Fatalf("read job 3: %v", err)
	}
	if sched != "2026-10-05T10:00:05+09:00" || created != "2026-10-05T01:00:05.000000000Z" {
		t.Fatalf("job 3 = %q/%q, want non-UTC spelling untouched", sched, created)
	}

	var trig, trigCreated, resolved string
	if err := conn.QueryRow(`SELECT triggered_at, created_at FROM kill_switch_events WHERE id = 1`).Scan(&trig, &trigCreated); err != nil {
		t.Fatalf("read kill_switch_events: %v", err)
	}
	if trig != "2026-10-05T01:00:05.250000000Z" || trigCreated != "2026-10-05T01:00:05.250000000Z" {
		t.Fatalf("kill_switch_events = %q/%q", trig, trigCreated)
	}
	if err := conn.QueryRow(`SELECT resolved_at FROM kill_switch_resolutions WHERE id = 1`).Scan(&resolved); err != nil || resolved != "2026-10-05T02:00:00.000000000Z" {
		t.Fatalf("kill_switch_resolutions.resolved_at = %q, err = %v", resolved, err)
	}

	// Append-only triggers must be back in place.
	for _, stmt := range []string{
		`UPDATE kill_switch_events SET reason = 'jev_api_down' WHERE id = 1`,
		`DELETE FROM kill_switch_events WHERE id = 1`,
		`UPDATE kill_switch_resolutions SET resolved_by = 'auto' WHERE id = 1`,
		`DELETE FROM kill_switch_resolutions WHERE id = 1`,
	} {
		if _, err := conn.Exec(stmt); err == nil || !strings.Contains(err.Error(), "append-only") {
			t.Fatalf("%q error = %v, want append-only trigger rejection", stmt, err)
		}
	}

	if err := m.Migrate(20); err != nil {
		t.Fatalf("migrate down to 20: %v", err)
	}
}
