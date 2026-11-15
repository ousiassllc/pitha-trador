package migrationtests

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	migratesqlite "github.com/golang-migrate/migrate/v4/database/sqlite"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	_ "modernc.org/sqlite"
	_ "modernc.org/sqlite/vec"

	"github.com/ousiassllc/pitha-trador/db"
)

// Migration 000057 creates research_notes (the head of each note) and
// research_note_versions (append-only: one row per version, UPDATE and DELETE
// refused by trigger). It touches no existing table. It migrates 56 -> 57 over
// a database that already holds a tuning experiment, keeps the text, count and
// enum limits as CHECKs, and steps one back.
func TestMigration000057_CreatesTheResearchNoteTablesWithoutTouchingOthers(t *testing.T) {
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

	if err := m.Migrate(56); err != nil {
		t.Fatalf("migrate to 56: %v", err)
	}
	if exists("research_notes") || exists("research_note_versions") {
		t.Fatal("the research note tables exist before migration 57")
	}
	if err := exec(`INSERT INTO tuning_experiments (created_at, actor, hypothesis, variable_kind, variable_name, before_value, after_value, started_at, status)
		VALUES ('t', 'gui', 'h', 'prompt', 'q', 'a', 'b', 't', 'running')`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	existing := []string{"tuning_experiments", "runtime_settings", "mcp_settings_audit", "positions"}
	before := map[string]int{}
	for _, tbl := range existing {
		before[tbl] = count(tbl)
	}

	if err := m.Migrate(57); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	for _, name := range []string{"research_notes", "research_note_versions", "research_notes_updated_idx", "research_note_versions_no_update", "research_note_versions_no_delete"} {
		if !exists(name) {
			t.Fatalf("%s missing after migration 57", name)
		}
	}
	for _, tbl := range existing {
		if got := count(tbl); got != before[tbl] {
			t.Errorf("%s rows %d -> %d: the migration must not touch existing tables", tbl, before[tbl], got)
		}
	}

	const cols = `(question, hypothesis, actions, result_summary, conclusion, evidence_links, tags, current_version, created_at, updated_at)`
	insertNote := func(question, conclusion, evidence, tags string) error {
		return exec(`INSERT INTO research_notes `+cols+` VALUES (?, '', '', '', ?, ?, ?, 1, 't', 't')`, question, conclusion, evidence, tags)
	}
	if err := insertNote("q", "pending", `[{"kind":"url","ref":"https://example.com"}]`, `["a","b"]`); err != nil {
		t.Fatalf("a valid note: %v", err)
	}
	if err := exec(`INSERT INTO research_notes (question, created_at, updated_at) VALUES ('defaults', 't', 't')`); err != nil {
		t.Fatalf("defaults: %v", err)
	}
	var conclusion, evidence string
	var version int
	if err := conn.QueryRow(`SELECT conclusion, evidence_links, current_version FROM research_notes WHERE question = 'defaults'`).Scan(&conclusion, &evidence, &version); err != nil ||
		conclusion != "pending" || evidence != "[]" || version != 1 {
		t.Fatalf("defaults = %q %q %d (%v), want pending [] 1", conclusion, evidence, version, err)
	}
	twentyOne := "[" + strings.Repeat(`{"kind":"other","ref":"x"},`, 20) + `{"kind":"other","ref":"x"}]`
	elevenTags := "[" + strings.Repeat(`"t",`, 10) + `"t"]`
	for name, err := range map[string]error{
		"an empty question":          insertNote("  ", "pending", "[]", "[]"),
		"a question over 500 chars":  insertNote(strings.Repeat("あ", 501), "pending", "[]", "[]"),
		"an unknown conclusion":      insertNote("q", "maybe", "[]", "[]"),
		"evidence that is not JSON":  insertNote("q", "pending", "nope", "[]"),
		"evidence that is an object": insertNote("q", "pending", `{"kind":"url"}`, "[]"),
		"21 evidence links":          insertNote("q", "pending", twentyOne, "[]"),
		"11 tags":                    insertNote("q", "pending", "[]", elevenTags),
		"a result over 3000 chars": exec(`INSERT INTO research_notes (question, result_summary, created_at, updated_at) VALUES ('q', ?, 't', 't')`,
			strings.Repeat("x", 3001)),
		"a current version of 0": exec(`INSERT INTO research_notes (question, current_version, created_at, updated_at) VALUES ('q', 0, 't', 't')`),
	} {
		if err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	// 500 multi-byte characters fit: the limit counts characters, not bytes.
	if err := insertNote(strings.Repeat("あ", 500), "supported", "[]", "[]"); err != nil {
		t.Errorf("a 500-character Japanese question: %v", err)
	}

	const versionCols = `(note_id, version, question, conclusion, actor, client, created_at)`
	insertVersion := func(noteID, version int, actor string) error {
		return exec(`INSERT INTO research_note_versions `+versionCols+` VALUES (?, ?, 'q', 'pending', ?, '100.64.0.9', 't')`, noteID, version, actor)
	}
	if err := insertVersion(1, 1, "mcp"); err != nil {
		t.Fatalf("version 1: %v", err)
	}
	if err := insertVersion(1, 2, "mcp"); err != nil {
		t.Fatalf("version 2: %v", err)
	}
	for name, err := range map[string]error{
		"a repeated (note, version)": insertVersion(1, 2, "mcp"),
		"a version of 0":             insertVersion(1, 0, "mcp"),
		"a note that does not exist": insertVersion(999, 1, "mcp"),
		"an unknown actor":           insertVersion(1, 3, "gui"),
	} {
		if err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	// Versions are append-only: the trigger refuses UPDATE and DELETE.
	if err := exec(`UPDATE research_note_versions SET question = 'edited' WHERE note_id = 1 AND version = 1`); err == nil || !strings.Contains(err.Error(), "append-only") {
		t.Errorf("UPDATE of a version row: %v, want the append-only refusal", err)
	}
	if err := exec(`DELETE FROM research_note_versions WHERE note_id = 1`); err == nil || !strings.Contains(err.Error(), "append-only") {
		t.Errorf("DELETE of a version row: %v, want the append-only refusal", err)
	}
	// A note with versions cannot be deleted either (foreign key).
	if err := exec(`DELETE FROM research_notes WHERE id = 1`); err == nil {
		t.Error("a note that has versions was deleted")
	}
	if got := count("research_note_versions"); got != 2 {
		t.Fatalf("version rows = %d, want 2", got)
	}

	if err := m.Migrate(56); err != nil {
		t.Fatalf("migrate down: %v", err)
	}
	if exists("research_notes") || exists("research_note_versions") {
		t.Fatal("the research note tables remain after the down step")
	}
	if got := count("tuning_experiments"); got != before["tuning_experiments"] {
		t.Fatalf("tuning_experiments rows after the down step = %d", got)
	}
}
