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

// Migration 000058 creates research_quote_samples (issue #1083): the 立花 時価
// recorded once a minute next to kabu's market_snapshots, one row per (source,
// symbol, minute), with no foreign key (retention deletes rows, the archive keeps
// them) and CHECKs that keep a zero price, an unknown source, via or session phase
// out. It touches no existing table. It migrates 57 -> 58 over a database that
// holds snapshots, and steps one back.
func TestMigration000058_CreatesTheResearchQuoteSamplesWithoutTouchingOthers(t *testing.T) {
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
	count := func(table string) int {
		t.Helper()
		var n int
		if err := conn.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		return n
	}
	exists := func(name string) bool {
		t.Helper()
		var n int
		if err := conn.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name = ?`, name).Scan(&n); err != nil {
			t.Fatalf("sqlite_master: %v", err)
		}
		return n == 1
	}

	if err := m.Migrate(57); err != nil {
		t.Fatalf("migrate to 57: %v", err)
	}
	if exists("research_quote_samples") {
		t.Fatal("research_quote_samples exists before migration 58")
	}
	for _, stmt := range []string{
		`INSERT INTO instruments (id, symbol, name, market, is_active, created_at, updated_at) VALUES (1, '7203', 'x', 'TSE Prime', 1, 't', 't')`,
		`INSERT INTO market_snapshots (id, instrument_id, symbol, timestamp, price, volume, turnover, vwap, price_vs_vwap_bps, raw_data_json)
			VALUES (11, 1, '7203', '2026-10-14T01:00:00.000000000Z', 1000, 1, 1, 1, 1, '{}')`,
	} {
		if err := exec(stmt); err != nil {
			t.Fatalf("seed %q: %v", stmt, err)
		}
	}
	existing := []string{"instruments", "market_snapshots", "research_tachibana_status", "research_daily_bar_runs"}
	before := map[string]int{}
	for _, tbl := range existing {
		before[tbl] = count(tbl)
	}

	if err := m.Migrate(58); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	for _, name := range []string{"research_quote_samples", "research_quote_samples_key_idx", "research_quote_samples_timestamp_idx"} {
		if !exists(name) {
			t.Fatalf("%s missing after migration 58", name)
		}
	}
	for _, tbl := range existing {
		if got := count(tbl); got != before[tbl] {
			t.Errorf("%s rows %d -> %d: the migration must not touch existing tables", tbl, before[tbl], got)
		}
	}

	insert := func(ts, source, symbol string, price any, special any, via, phase string) error {
		return exec(`INSERT INTO research_quote_samples (timestamp, source, symbol, price, special_quote, via, session_phase)
			VALUES (?, ?, ?, ?, ?, ?, ?)`, ts, source, symbol, price, special, via, phase)
	}
	const ts = "2026-10-14T01:00:00.000000000Z"
	if err := insert(ts, "tachibana", "7203", 1000.5, 0, "event", "morning"); err != nil {
		t.Fatalf("a valid sample: %v", err)
	}
	// Optional columns are nullable; created_at defaults; ids are assigned.
	var id int64
	var created string
	if err := conn.QueryRow(`SELECT id, created_at FROM research_quote_samples`).Scan(&id, &created); err != nil || id == 0 || created == "" {
		t.Fatalf("id %d created_at %q (%v), want assigned values", id, created, err)
	}
	for name, err := range map[string]error{
		"a second sample of the same source, symbol and minute": insert(ts, "tachibana", "7203", 1001, 0, "rest", "morning"),
		"a zero price":                insert("2026-10-14T01:01:00.000000000Z", "tachibana", "7203", 0, 0, "event", "morning"),
		"a negative price":            insert("2026-10-14T01:01:00.000000000Z", "tachibana", "7203", -1, 0, "event", "morning"),
		"an unknown source":           insert("2026-10-14T01:01:00.000000000Z", "other", "7203", 1000, 0, "event", "morning"),
		"an unknown via":              insert("2026-10-14T01:01:00.000000000Z", "tachibana", "7203", 1000, 0, "push", "morning"),
		"an unknown phase":            insert("2026-10-14T01:01:00.000000000Z", "tachibana", "7203", 1000, 0, "event", "evening"),
		"a special_quote outside 0/1": insert("2026-10-14T01:01:00.000000000Z", "tachibana", "7203", 1000, 2, "event", "morning"),
		"a NULL price":                insert("2026-10-14T01:01:00.000000000Z", "tachibana", "7203", nil, 0, "event", "morning"),
	} {
		if err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	// The same minute of another symbol or source is a different sample; no foreign key to the instruments.
	if err := insert(ts, "tachibana", "NOSUCH", 10, 1, "event", "open_auction"); err != nil {
		t.Errorf("another symbol, same minute: %v", err)
	}
	if err := insert(ts, "kabu", "7203", 1000, 0, "rest", "close"); err != nil {
		t.Errorf("another source, same minute: %v", err)
	}
	if got := count("research_quote_samples"); got != 3 {
		t.Fatalf("rows = %d, want 3", got)
	}

	if err := m.Migrate(57); err != nil {
		t.Fatalf("migrate down: %v", err)
	}
	if exists("research_quote_samples") {
		t.Fatal("research_quote_samples remains after the down step")
	}
	for _, tbl := range existing {
		if got := count(tbl); got != before[tbl] {
			t.Errorf("%s rows after the down step = %d, want %d", tbl, got, before[tbl])
		}
	}
}
