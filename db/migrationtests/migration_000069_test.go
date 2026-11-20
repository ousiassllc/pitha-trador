package migrationtests

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

// Migration 000069 creates research_index_minutes and research_index_days (issue
// #1084): one value per (index code, session minute), and a per-trading-day marker
// whose 'unavailable' status (the 立花 quote recording had no value that day) must
// carry minute_count 0. It touches no existing table (the 立花 quote samples and the
// FX record stay as they are) and steps back cleanly.
func TestMigration000069_CreatesTheResearchIndexTablesWithoutTouchingOthers(t *testing.T) {
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
	exec := func(stmt string, args ...any) error {
		_, err := conn.Exec(stmt, args...)
		return err
	}
	exists := func(name string) bool {
		t.Helper()
		var n int
		if err := conn.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name = ?`, name).Scan(&n); err != nil {
			t.Fatalf("sqlite_master: %v", err)
		}
		return n == 1
	}
	count := func(table string) int {
		t.Helper()
		var n int
		if err := conn.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		return n
	}

	if err := m.Migrate(68); err != nil {
		t.Fatalf("migrate to 68: %v", err)
	}
	if exists("research_index_minutes") || exists("research_index_days") {
		t.Fatal("the index tables exist before migration 69")
	}
	if err := exec(`INSERT INTO instruments (id, symbol, name, market, is_active, created_at, updated_at) VALUES (1, '7203', 'x', 'TSE Prime', 1, 't', 't')`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	existing := []string{"instruments", "market_snapshots", "jev_decisions", "research_horizon_labels", "research_label_days", "research_fx_candles", "research_fx_days", "research_quote_samples"}
	before := map[string]int{}
	for _, tbl := range existing {
		before[tbl] = count(tbl)
	}

	if err := m.Migrate(69); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	for _, name := range []string{"research_index_minutes", "research_index_days", "research_index_minutes_day_idx"} {
		if !exists(name) {
			t.Fatalf("%s missing after migration 69", name)
		}
	}
	for _, tbl := range existing {
		if got := count(tbl); got != before[tbl] {
			t.Errorf("%s rows %d -> %d: the migration must not touch existing tables", tbl, before[tbl], got)
		}
	}

	const at = "2026-10-14T21:00:00.000000000Z"
	insert := func(symbol, minuteTime string, price any, via string) error {
		return exec(`INSERT INTO research_index_minutes (symbol, minute_time, trading_day, price, via, recorded_at) VALUES (?, ?, '2026-10-15', ?, ?, 't')`, symbol, minuteTime, price, via)
	}
	if err := insert("101", at, 38000.5, "event"); err != nil {
		t.Fatalf("a minute value: %v", err)
	}
	if err := insert("101", "2026-10-14T21:01:00.000000000Z", 38001, "rest"); err != nil {
		t.Fatalf("a REST value: %v", err)
	}
	// Another code may take the same minute.
	if err := insert("151", at, 2700, "event"); err != nil {
		t.Fatalf("another code at the same minute: %v", err)
	}
	for name, err := range map[string]error{
		"a duplicate (code, minute)": insert("101", at, 1.0, "event"),
		"a non-positive price":       insert("101", "2026-10-14T21:04:00.000000000Z", 0, "event"),
		"a missing price":            insert("101", "2026-10-14T21:05:00.000000000Z", nil, "event"),
		"an unknown via":             insert("101", "2026-10-14T21:06:00.000000000Z", 1.0, "push"),
		"a code with a space":        insert("1 01", "2026-10-14T21:07:00.000000000Z", 1.0, "event"),
		"a code with a symbol":       insert("N-225", "2026-10-14T21:08:00.000000000Z", 1.0, "event"),
		"a code that is too long":    insert("123456789", "2026-10-14T21:09:00.000000000Z", 1.0, "event"),
		"an empty code":              insert("", "2026-10-14T21:10:00.000000000Z", 1.0, "event"),
	} {
		if err == nil {
			t.Errorf("%s was accepted", name)
		}
	}

	const day = `INSERT INTO research_index_days (symbol, trading_day, status, minute_count, checked_at) VALUES ('101', ?, ?, ?, 't')`
	if err := exec(day, "2026-10-15", "recorded", 3); err != nil {
		t.Fatalf("a recorded day: %v", err)
	}
	if err := exec(day, "2026-10-16", "unavailable", 0); err != nil {
		t.Fatalf("an unavailable day: %v", err)
	}
	for name, err := range map[string]error{
		"a second marker for one day":      exec(day, "2026-10-15", "recorded", 3),
		"an unknown status":                exec(day, "2026-10-19", "missing", 0),
		"an unavailable day with minutes":  exec(day, "2026-10-20", "unavailable", 2),
		"a recorded day without minutes":   exec(day, "2026-10-21", "recorded", 0),
		"a negative count":                 exec(day, "2026-10-22", "recorded", -1),
		"a trading day that is not a date": exec(day, "20261023", "recorded", 1),
	} {
		if err == nil {
			t.Errorf("%s was accepted", name)
		}
	}

	if err := m.Migrate(68); err != nil {
		t.Fatalf("migrate down: %v", err)
	}
	if exists("research_index_minutes") || exists("research_index_days") || exists("research_index_minutes_day_idx") {
		t.Fatal("the index tables remain after the down step")
	}
	for _, tbl := range existing {
		if got := count(tbl); got != before[tbl] {
			t.Errorf("%s rows after the down step = %d, want %d", tbl, got, before[tbl])
		}
	}
}
