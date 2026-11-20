package notetests

import (
	"database/sql"
	"regexp"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

// 受け入れ: 取引・Risk・発注・Jev・設定に差分がない。A note is a record only:
// creating and updating notes (and reading them) leaves every table but the two
// research-note tables byte-for-byte as it was, which covers runtime_settings,
// the settings audit, positions, signals, orders, risk and the Jev tables.
func TestNotesChangeNoSettingAndNoTradingState(t *testing.T) {
	srv, w := newServer(t)
	before := snapshotOtherTables(t, w)
	if len(before) < 20 {
		t.Fatalf("only %d other tables to compare; the schema was not migrated?", len(before))
	}
	for _, must := range []string{"runtime_settings", "mcp_settings_audit", "positions", "trade_signals", "paper_orders", "order_proposals", "jev_decisions", "tuning_experiments"} {
		if _, ok := before[must]; !ok {
			t.Fatalf("table %s is not part of the comparison", must)
		}
	}

	n := create(t, srv, noteArgs("q", nil))
	update(t, srv, n.ID, 1, map[string]any{"conclusion": "supported", "result_summary": "x"}).data(t, "research_note_update", &struct{}{})
	create(t, srv, noteArgs("second", nil))
	list(t, srv, map[string]any{"query": "q"})
	get(t, srv, n.ID)

	after := snapshotOtherTables(t, w)
	for table, was := range before {
		if after[table] != was {
			t.Errorf("table %s changed when notes were written and read", table)
		}
	}
	if c := count(t, w, `SELECT COUNT(*) FROM research_notes`); c != 2 {
		t.Fatalf("%d notes, want 2", c)
	}
}

// snapshotOtherTables dumps every table that is not a research-note table (and
// not SQLite's own), keyed by name.
func snapshotOtherTables(t *testing.T, w *sql.DB) map[string]string {
	t.Helper()
	rows, err := w.QueryContext(t.Context(), `SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' AND name NOT LIKE 'research_note%' AND name NOT LIKE 'schema_migrations'`)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	_ = rows.Close()
	out := map[string]string{}
	for _, name := range names {
		out[name] = dumpTable(t, w, `SELECT * FROM "`+name+`"`)
	}
	return out
}

var allowedWrites = map[string]*regexp.Regexp{
	"research_notes":         regexp.MustCompile(`(?is)^\s*(INSERT INTO research_notes\b|UPDATE research_notes\s+SET\b.*\sWHERE id = \? AND current_version = \?)`),
	"research_note_versions": regexp.MustCompile(`(?is)^\s*INSERT INTO research_note_versions\b`),
}

// The only statements a note ever writes with are the INSERT of a note, the
// INSERT of a version and the UPDATE of a note's head row (guarded by the
// version it was read at). No statement UPDATEs or DELETEs a version row, none
// writes any other table, and the read tools never take the write path.
func TestNoteWritePathIsNarrow(t *testing.T) {
	driverName, counter := testkit.NewCountingDriver(t)
	srv, w := newServerOn(t, driverName)

	list(t, srv, nil)
	n := create(t, srv, noteArgs("q", nil))
	update(t, srv, n.ID, 1, map[string]any{"result_summary": "x"}).data(t, "research_note_update", &struct{}{})
	update(t, srv, n.ID, 1, map[string]any{"result_summary": "stale"}).wantError(t, 409, "research_note_version_conflict")
	srv.call(t, "research_note_create", noteArgs("", nil)) // refused
	list(t, srv, map[string]any{"query": "q", "tag": "volume"})
	get(t, srv, n.ID)

	written := map[string]int{}
	for _, stmt := range counter.StatementLog() {
		s := strings.ToUpper(strings.TrimSpace(stmt))
		changes := strings.HasPrefix(s, "INSERT") || strings.HasPrefix(s, "UPDATE") || strings.HasPrefix(s, "DELETE") ||
			strings.HasPrefix(s, "CREATE") || strings.HasPrefix(s, "DROP") || strings.HasPrefix(s, "ALTER")
		if !changes {
			continue
		}
		matched := false
		for table, re := range allowedWrites {
			if re.MatchString(stmt) {
				written[table]++
				matched = true
			}
		}
		if !matched {
			t.Errorf("a write statement outside the allowed set: %s", testkit.FirstLine(stmt))
		}
	}
	// One create (note + version 1) and one accepted update (version 2 + head UPDATE).
	if written["research_notes"] != 2 || written["research_note_versions"] != 2 {
		t.Errorf("writes = %v, want research_notes 2 (INSERT, UPDATE) and research_note_versions 2 (INSERTs)", written)
	}
	if c := count(t, w, `SELECT COUNT(*) FROM research_note_versions`); c != 2 {
		t.Errorf("%d version rows, want 2", c)
	}
	// The triggers refuse a version rewrite or removal even for a direct statement.
	if _, err := w.ExecContext(t.Context(), `UPDATE research_note_versions SET question = 'x' WHERE note_id = ?`, n.ID); err == nil {
		t.Error("a version row was updated")
	}
	if _, err := w.ExecContext(t.Context(), `DELETE FROM research_note_versions WHERE note_id = ?`, n.ID); err == nil {
		t.Error("a version row was deleted")
	}
	// The read connection still refuses writes.
	db, err := srv.DB()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO research_notes (question, created_at, updated_at) VALUES ('x', 't', 't')`); err == nil {
		t.Error("the read connection accepted a write")
	}
}
