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
// recorded before keeps its row with dropped_incomplete NULL ("not recorded",
// never 0) and the other new counts 0; a row written after the migration keeps
// the value it was given, 0 included; the down migration takes the columns away
// and keeps the rows.
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
	// The existing row is not rewritten: dropped_incomplete stays NULL.
	var usable, required, oos, requiredOOS, v1, v2 int
	var dropped sql.NullInt64
	read := `SELECT usable_rows, required_rows, oos_rows, required_oos_rows, dropped_incomplete, label_v1_rows, label_v2_rows
		FROM metalabel_retrain_skips WHERE skipped_at = ?`
	if err := conn.QueryRow(read, "2026-10-18T09:08:00Z").Scan(&usable, &required, &oos, &requiredOOS, &dropped, &v1, &v2); err != nil {
		t.Fatalf("read the old skip after up: %v", err)
	}
	if usable != 0 || required != 130 || dropped.Valid || oos+requiredOOS+v1+v2 != 0 {
		t.Errorf("old skip after up = %d/%d dropped %+v, new counts %d %d %d %d, want its own counts kept, dropped_incomplete NULL and the other new counts 0",
			usable, required, dropped, oos, requiredOOS, v1, v2)
	}

	// New rows keep what they were given: a real count, 0 included.
	mustExec(t, conn, `INSERT INTO metalabel_retrain_skips (skipped_at, reason_code, usable_rows, required_rows, oos_rows, required_oos_rows,
		dropped_incomplete, label_v1_rows, label_v2_rows, created_at) VALUES ('2026-10-26T09:08:00Z', 'too_few_rows_before_oos', 38, 130, 720, 740, 2, 31, 760, 't')`)
	mustExec(t, conn, `INSERT INTO metalabel_retrain_skips (skipped_at, reason_code, usable_rows, required_rows, oos_rows, required_oos_rows,
		dropped_incomplete, label_v1_rows, label_v2_rows, created_at) VALUES ('2026-10-27T09:08:00Z', 'too_few_rows_before_oos', 38, 130, 720, 740, 0, 31, 760, 't')`)
	for at, want := range map[string]int64{"2026-10-26T09:08:00Z": 2, "2026-10-27T09:08:00Z": 0} {
		if err := conn.QueryRow(read, at).Scan(&usable, &required, &oos, &requiredOOS, &dropped, &v1, &v2); err != nil {
			t.Fatalf("read the new skip %s: %v", at, err)
		}
		if !dropped.Valid || dropped.Int64 != want || oos != 720 || requiredOOS != 740 || v1 != 31 || v2 != 760 {
			t.Errorf("new skip %s = dropped %+v oos %d/%d v1 %d v2 %d, want dropped %d kept as given", at, dropped, oos, requiredOOS, v1, v2, want)
		}
	}
	if _, err := conn.Exec(`INSERT INTO metalabel_retrain_skips (skipped_at, reason_code, usable_rows, required_rows, dropped_incomplete, created_at)
		VALUES ('t', 'x', 0, 0, -1, 't')`); err == nil {
		t.Error("a negative dropped_incomplete was accepted, want the CHECK to refuse it")
	}
	if _, err := conn.Exec(`INSERT INTO metalabel_retrain_skips (skipped_at, reason_code, usable_rows, required_rows, oos_rows, created_at)
		VALUES ('t', 'x', 0, 0, -1, 't')`); err == nil {
		t.Error("a negative oos_rows was accepted, want the CHECK to refuse it")
	}

	if err := m.Migrate(43); err != nil {
		t.Fatalf("migrate down to 43: %v", err)
	}
	var rows int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM metalabel_retrain_skips`).Scan(&rows); err != nil || rows != 3 {
		t.Errorf("rows after down = %d, %v, want the 3 rows kept", rows, err)
	}
	var gone sql.NullInt64
	if err := conn.QueryRow(`SELECT dropped_incomplete FROM metalabel_retrain_skips`).Scan(&gone); err == nil {
		t.Error("dropped_incomplete still exists after down")
	}
	if err := conn.QueryRow(`SELECT oos_rows FROM metalabel_retrain_skips`).Scan(&gone); err == nil {
		t.Error("oos_rows still exists after down")
	}
}
