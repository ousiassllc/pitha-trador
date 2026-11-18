package migrationtests

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

// Migration 000063 creates the disclosure source tables (issue #1096): the EDINET
// document list, its fetch days, the 立花 news, the per-source status and a read
// view over both sources. It touches no existing table (disclosure_items keeps
// its rows). It migrates 62 -> 63 over a database that holds a disclosure item,
// and steps one back.
func TestMigration000063_CreatesTheDisclosureSourceTablesWithoutTouchingOthers(t *testing.T) {
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
	created := []string{"research_edinet_documents", "research_edinet_fetch_days", "research_tachibana_news", "research_disclosure_source_status", "research_disclosure_source_events_v"}

	if err := m.Migrate(62); err != nil {
		t.Fatalf("migrate to 62: %v", err)
	}
	for _, name := range created {
		if exists(name) {
			t.Fatalf("%s exists before migration 63", name)
		}
	}
	if err := exec(`INSERT INTO disclosure_items (symbol, source_id, published_at, title, first_seen_at) VALUES ('7203', 'a', 't', 'x', 't')`); err != nil {
		t.Fatalf("seed disclosure item: %v", err)
	}
	if err := m.Migrate(63); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	for _, name := range created {
		if !exists(name) {
			t.Fatalf("%s missing after migration 63", name)
		}
	}

	good := `INSERT INTO research_edinet_documents (doc_id, symbol, sec_code, submitted_at, submit_day, first_seen_at, updated_at) VALUES (?, ?, ?, 't', ?, 't', 't')`
	if err := exec(good, "S1", "7203", "72030", "2026-11-13"); err != nil {
		t.Fatalf("valid document: %v", err)
	}
	for name, err := range map[string]error{
		"a duplicate doc_id": exec(good, "S1", "6758", "67580", "2026-11-13"),
		"an empty symbol":    exec(good, "S2", "", "72030", "2026-11-13"),
		"an empty doc_id":    exec(good, "", "7203", "72030", "2026-11-13"),
		"a malformed day":    exec(good, "S3", "7203", "72030", "2026-11"),
		"an unknown source":  exec(`INSERT INTO research_disclosure_source_status (source) VALUES ('jquants')`),
		"a negative count":   exec(`INSERT INTO research_disclosure_source_status (source, failure_count) VALUES ('edinet', -1)`),
		"an empty news id":   exec(`INSERT INTO research_tachibana_news (news_id, headline, published_at, first_seen_at) VALUES ('', 'h', 't', 't')`),
		"a negative day sum": exec(`INSERT INTO research_edinet_fetch_days (day, fetched_at, total_count, recorded_count, skipped_count) VALUES ('2026-11-13', 't', -1, 0, 0)`),
	} {
		if err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	news := `INSERT INTO research_tachibana_news (news_id, symbol, headline, published_at, first_seen_at) VALUES ('n1', ?, 'h', 't2', 't')`
	if err := exec(news, "7203"); err != nil {
		t.Fatalf("valid news: %v", err)
	}
	if err := exec(news, ""); err != nil {
		t.Fatalf("news without a symbol: %v", err)
	}
	if err := exec(news, "7203"); err == nil {
		t.Error("a duplicate (news_id, symbol) was accepted")
	}
	// The view shows the document and the news with a symbol, not the symbol-less news.
	var n int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM research_disclosure_source_events_v`).Scan(&n); err != nil || n != 2 {
		t.Errorf("view rows = %d (%v), want 2", n, err)
	}

	if err := m.Steps(-1); err != nil {
		t.Fatalf("down one step: %v", err)
	}
	for _, name := range created {
		if exists(name) {
			t.Errorf("%s still exists after the down migration", name)
		}
	}
	if err := conn.QueryRow(`SELECT COUNT(*) FROM disclosure_items`).Scan(&n); err != nil || n != 1 {
		t.Errorf("disclosure_items has %d rows after the down migration (%v), want 1", n, err)
	}
}
