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

// breakdownColumns are the five columns migration 000044 adds to
// metalabel_retrain_skips; all of them are NULL-able, NULL meaning "not recorded".
var breakdownColumns = []string{"dropped_incomplete", "label_v1_rows", "label_v2_rows", "oos_rows", "required_oos_rows"}

// readBreakdown returns the five breakdown columns of the skip at skippedAt,
// in breakdownColumns order.
func readBreakdown(t *testing.T, conn *sql.DB, skippedAt string) []sql.NullInt64 {
	t.Helper()
	out := make([]sql.NullInt64, len(breakdownColumns))
	dest := make([]any, len(out))
	for i := range out {
		dest[i] = &out[i]
	}
	q := "SELECT " + strings.Join(breakdownColumns, ", ") + " FROM metalabel_retrain_skips WHERE skipped_at = ?"
	if err := conn.QueryRow(q, skippedAt).Scan(dest...); err != nil {
		t.Fatalf("read the breakdown of %s: %v", skippedAt, err)
	}
	return out
}

// Migration 000044 (issue #862) adds the breakdown of a retrain skip (rows
// dropped as incomplete, labeled decisions per label_version, OOS-side rows).
// All five columns are NULL-able and NULL means "not recorded": a skip recorded
// before keeps its row and all five stay NULL (never rewritten to 0); a row
// written after the migration keeps the value it was given, 0 included; the
// down migration takes the columns away and keeps the rows.
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
	const old = "2026-10-18T09:08:00Z"
	mustExec(t, conn, `INSERT INTO metalabel_retrain_skips (skipped_at, reason_code, usable_rows, required_rows, created_at)
		VALUES ('`+old+`', 'too_few_rows_before_oos', 0, 130, '2026-10-18T09:08:01Z')`)

	if err := m.Migrate(44); err != nil {
		t.Fatalf("migrate to 44: %v", err)
	}
	// The existing row is not rewritten: all five columns are NULL, its own counts are kept.
	for i, v := range readBreakdown(t, conn, old) {
		if v.Valid {
			t.Errorf("old skip: %s = %d after up, want NULL (not recorded)", breakdownColumns[i], v.Int64)
		}
	}
	var usable, required int
	if err := conn.QueryRow(`SELECT usable_rows, required_rows FROM metalabel_retrain_skips WHERE skipped_at = ?`, old).Scan(&usable, &required); err != nil || usable != 0 || required != 130 {
		t.Errorf("old skip own counts = %d/%d, %v, want 0/130 kept", usable, required, err)
	}

	// New rows keep what they were given: positive values and 0 alike.
	insert := `INSERT INTO metalabel_retrain_skips (skipped_at, reason_code, usable_rows, required_rows, oos_rows, required_oos_rows,
		dropped_incomplete, label_v1_rows, label_v2_rows, created_at) VALUES (?, 'too_few_rows_before_oos', 38, 130, ?, ?, ?, ?, ?, 't')`
	if _, err := conn.Exec(insert, "2026-10-26T09:08:00Z", 720, 740, 2, 31, 760); err != nil {
		t.Fatalf("insert a skip with counts: %v", err)
	}
	if _, err := conn.Exec(insert, "2026-10-27T09:08:00Z", 0, 0, 0, 0, 0); err != nil {
		t.Fatalf("insert a skip with zero counts: %v", err)
	}
	// breakdownColumns order: dropped, v1, v2, oos, required_oos.
	for at, want := range map[string][]int64{
		"2026-10-26T09:08:00Z": {2, 31, 760, 720, 740},
		"2026-10-27T09:08:00Z": {0, 0, 0, 0, 0},
	} {
		for i, v := range readBreakdown(t, conn, at) {
			if !v.Valid || v.Int64 != want[i] {
				t.Errorf("new skip %s: %s = %+v, want %d kept as given", at, breakdownColumns[i], v, want[i])
			}
		}
	}
	// A negative count is refused, NULL is allowed.
	for _, col := range breakdownColumns {
		if _, err := conn.Exec(`INSERT INTO metalabel_retrain_skips (skipped_at, reason_code, usable_rows, required_rows, ` + col + `, created_at)
			VALUES ('t', 'x', 0, 0, -1, 't')`); err == nil {
			t.Errorf("a negative %s was accepted, want the CHECK to refuse it", col)
		}
	}

	if err := m.Migrate(43); err != nil {
		t.Fatalf("migrate down to 43: %v", err)
	}
	var rows int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM metalabel_retrain_skips`).Scan(&rows); err != nil || rows != 3 {
		t.Errorf("rows after down = %d, %v, want the 3 rows kept", rows, err)
	}
	for _, col := range breakdownColumns {
		var gone sql.NullInt64
		if err := conn.QueryRow(`SELECT ` + col + ` FROM metalabel_retrain_skips`).Scan(&gone); err == nil {
			t.Errorf("%s still exists after down", col)
		}
	}
}
