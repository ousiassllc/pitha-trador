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

// Migration 000044 (issue #862) adds the breakdown of a retrain skip (OOS-side
// rows, rows dropped as incomplete, labeled decisions per label_version). A skip
// recorded before keeps its row with the new counts at 0; the down migration
// takes the columns away and keeps the row.
func TestMigration000044_RetrainSkipBreakdownColumns(t *testing.T) {
	conn, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "m.db"))
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

	if err := m.Migrate(43); err != nil {
		t.Fatalf("migrate to 43: %v", err)
	}
	mustExec(t, conn, `INSERT INTO metalabel_retrain_skips (skipped_at, reason_code, usable_rows, required_rows, created_at)
		VALUES ('2026-10-18T09:08:00Z', 'too_few_rows_before_oos', 0, 130, '2026-10-18T09:08:01Z')`)

	if err := m.Migrate(44); err != nil {
		t.Fatalf("migrate to 44: %v", err)
	}
	var usable, required, oos, requiredOOS, dropped, v1, v2 int
	if err := conn.QueryRow(`SELECT usable_rows, required_rows, oos_rows, required_oos_rows, dropped_incomplete, label_v1_rows, label_v2_rows
		FROM metalabel_retrain_skips`).Scan(&usable, &required, &oos, &requiredOOS, &dropped, &v1, &v2); err != nil {
		t.Fatalf("read the old skip after up: %v", err)
	}
	if usable != 0 || required != 130 || oos+requiredOOS+dropped+v1+v2 != 0 {
		t.Errorf("old skip after up = %d/%d with new counts %d %d %d %d %d, want its own counts kept and the new ones 0",
			usable, required, oos, requiredOOS, dropped, v1, v2)
	}
	if _, err := conn.Exec(`INSERT INTO metalabel_retrain_skips (skipped_at, reason_code, usable_rows, required_rows, oos_rows, created_at)
		VALUES ('t', 'x', 0, 0, -1, 't')`); err == nil {
		t.Error("a negative oos_rows was accepted, want the CHECK to refuse it")
	}

	if err := m.Migrate(43); err != nil {
		t.Fatalf("migrate down to 43: %v", err)
	}
	var kept int
	if err := conn.QueryRow(`SELECT required_rows FROM metalabel_retrain_skips`).Scan(&kept); err != nil || kept != 130 {
		t.Errorf("old skip after down = %d, %v, want 130 kept", kept, err)
	}
	var gone int
	if err := conn.QueryRow(`SELECT oos_rows FROM metalabel_retrain_skips`).Scan(&gone); err == nil {
		t.Error("oos_rows still exists after down")
	}
}
