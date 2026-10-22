package db_test

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	migratesqlite "github.com/golang-migrate/migrate/v4/database/sqlite"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	_ "modernc.org/sqlite"

	"github.com/ousiassllc/pitha-trador/db"
)

// Migration 000046 (issue #871) adds tuning_experiments: one row per tuning
// experiment, with ONE changed variable. The CHECKs hold the shape: a known
// kind / status / actor, a non-empty hypothesis, two different values, and an
// end time exactly when the experiment is no longer running. The down
// migration drops the table.
func TestMigration000046_TuningExperiments(t *testing.T) {
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
	// 000044 and 000045 belong to other branches: go up to 43, then to 46
	// whether or not they are there yet.
	if err := m.Migrate(43); err != nil {
		t.Fatalf("migrate to 43: %v", err)
	}
	if _, err := conn.Exec(`SELECT 1 FROM tuning_experiments`); err == nil {
		t.Fatal("tuning_experiments exists before migration 000046")
	}
	if err := m.Migrate(46); err != nil {
		t.Fatalf("migrate to 46: %v", err)
	}

	const insert = `INSERT INTO tuning_experiments (created_at, actor, client, hypothesis, variable_kind, variable_name,
		before_value, after_value, audit_id, started_at, ended_at, status) VALUES (?, ?, '', ?, ?, ?, ?, ?, NULL, ?, ?, ?)`
	ok := func(actor, hypothesis, kind, name, before, after string, ended any, status string) error {
		_, err := conn.Exec(insert, "2026-10-22T00:00:00Z", actor, hypothesis, kind, name, before, after, "2026-10-22T00:00:00Z", ended, status)
		return err
	}
	if err := ok("mcp", "損失上限を下げるとドローダウンが減る", "setting", "risk.paper.max_trade_loss_pct", "1", "0.5", nil, "running"); err != nil {
		t.Fatalf("a valid running experiment was refused: %v", err)
	}
	if err := ok("gui", "別のモデルへ", "model", "jev-model", "a", "b", "2026-10-23T00:00:00Z", "closed"); err != nil {
		t.Fatalf("a valid closed experiment was refused: %v", err)
	}
	for name, err := range map[string]error{
		"unknown actor":             ok("bot", "h", "setting", "k", "1", "2", nil, "running"),
		"unknown kind":              ok("mcp", "h", "weather", "k", "1", "2", nil, "running"),
		"unknown status":            ok("mcp", "h", "setting", "k", "1", "2", nil, "paused"),
		"empty hypothesis":          ok("mcp", "  ", "setting", "k", "1", "2", nil, "running"),
		"empty name":                ok("mcp", "h", "setting", "", "1", "2", nil, "running"),
		"same before and after":     ok("mcp", "h", "setting", "k", "1", "1", nil, "running"),
		"running with an end":       ok("mcp", "h", "setting", "k", "1", "2", "2026-10-23T00:00:00Z", "running"),
		"closed without an end":     ok("mcp", "h", "setting", "k", "1", "2", nil, "closed"),
		"aborted without an end":    ok("mcp", "h", "setting", "k", "1", "2", nil, "aborted"),
		"hypothesis over 500 chars": ok("mcp", strings.Repeat("あ", 501), "setting", "k", "1", "2", nil, "running"),
	} {
		if err == nil {
			t.Errorf("%s: the CHECK accepted it", name)
		}
	}

	if err := m.Steps(-1); err != nil {
		t.Fatalf("migrate down one step: %v", err)
	}
	if _, err := conn.Exec(`SELECT 1 FROM tuning_experiments`); err == nil {
		t.Error("tuning_experiments still exists after the down migration")
	}
}
