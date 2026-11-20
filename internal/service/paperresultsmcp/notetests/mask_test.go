package notetests

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/researchnote"
)

// The strings below are made up for the test; none is a real credential.
var secrets = []string{
	"sk-proj-abcdefghijklmnopqrstuv0123456789",
	"ghp_abcdefghijklmnopqrstuvwxyz0123456789",
	"AKIAIOSFODNN7EXAMPLE",
	"eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dBjftJeZ4CVPmB92K27uhbUJU1p1r_wW1gFWFOEjXk",
	"0123456789abcdef0123456789abcdef01234567",
	"hunter2-passphrase",
	"Zm9vYmFyYmF6cXV4",
}

// secretText is free text holding every secret in a different shape.
func secretText() string {
	return "鍵は " + secrets[0] + " と " + secrets[1] + "、AWS は " + secrets[2] + "、JWT は " + secrets[3] +
		"、ハッシュ風 " + secrets[4] + "、curl -H 'Authorization: Bearer " + secrets[6] + "'、 api_key=" + secrets[5] + " でした"
}

func assertNoSecret(t *testing.T, where, text string) {
	t.Helper()
	for _, s := range secrets {
		if strings.Contains(text, s) {
			t.Errorf("%s holds the secret %q: %.300s", where, s, text)
		}
	}
}

func dumpTable(t *testing.T, w *sql.DB, query string) string {
	t.Helper()
	rows, err := w.QueryContext(t.Context(), query)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	cols, _ := rows.Columns()
	var b strings.Builder
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			t.Fatal(err)
		}
		for _, v := range vals {
			switch x := v.(type) {
			case []byte:
				b.Write(x)
			case string:
				b.WriteString(x)
			case nil:
				b.WriteString("NULL")
			default:
				_, _ = fmt.Fprint(&b, x)
			}
			b.WriteByte('|')
		}
		b.WriteByte('\n')
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// 受け入れ: キーの形の文字列は、保存の前に伏せられる。表の行・ツールの応答・
// 版・一覧・監査の記録のどこにも元の文字列は残らない（作成でも更新でも）。
func TestKeyShapedStringsAreMaskedBeforeStoring(t *testing.T) {
	srv, w := newServer(t)
	text := secretText()
	args := map[string]any{
		"question": "調べもの " + secrets[0], "hypothesis": text, "actions": text, "result_summary": text,
		"tags":           []string{"ok", secrets[1], "api_key=" + secrets[5]},
		"evidence_links": []map[string]any{{"kind": "url", "ref": "https://u:" + secrets[5] + "@example.com/x?token=" + secrets[4]}, {"kind": "other", "ref": secrets[2]}},
	}
	create := srv.call(t, "research_note_create", args)
	assertNoSecret(t, "create response", string(create.body))
	var n researchnote.Note
	create.data(t, "research_note_create", &n)
	for field, got := range map[string]string{"question": n.Question, "hypothesis": n.Hypothesis, "actions": n.Actions, "result_summary": n.ResultSummary} {
		if !strings.Contains(got, "[masked]") {
			t.Errorf("%s was not masked: %q", field, got)
		}
	}
	if !strings.Contains(n.Hypothesis, "鍵は [masked] と [masked]") || !strings.Contains(n.Hypothesis, "でした") {
		t.Errorf("the text around the secrets was not kept: %q", n.Hypothesis)
	}
	if n.Tags[0] != "ok" || n.Tags[1] != "[masked]" || len(n.Tags) != 3 {
		t.Errorf("tags = %q, want the key-shaped ones masked", n.Tags)
	}
	if len(n.EvidenceLinks) != 2 || n.EvidenceLinks[1].Ref != "[masked]" || !strings.HasPrefix(n.EvidenceLinks[0].Ref, "https://u:[masked]@example.com") {
		t.Errorf("evidence = %+v, want the key-shaped refs masked", n.EvidenceLinks)
	}

	// An update masks too, and the masked text is what the version holds.
	var up researchnote.Note
	update(t, srv, n.ID, 1, map[string]any{"result_summary": "new " + text, "actions": "password=" + secrets[5]}).data(t, "research_note_update", &up)
	if up.ResultSummary != "new "+n.ResultSummary || up.Actions != "password=[masked]" {
		t.Errorf("updated note = %q / %q", up.ResultSummary, up.Actions)
	}

	// Nowhere is the original left: every response and every stored row.
	for tool, body := range map[string]string{
		"list": string(srv.call(t, "research_notes_list", map[string]any{}).body),
		"get":  string(srv.call(t, "research_note_get", map[string]any{"note_id": n.ID}).body),
	} {
		assertNoSecret(t, tool+" response", body)
	}
	for _, table := range []string{"research_notes", "research_note_versions", "mcp_settings_audit", "runtime_settings"} {
		assertNoSecret(t, "table "+table, dumpTable(t, w, `SELECT * FROM `+table))
	}
	// The audit trail of a note is its version rows (actor and caller's host);
	// like the experiments it writes no mcp_settings_audit row.
	if c := count(t, w, `SELECT COUNT(*) FROM research_note_versions WHERE actor = 'mcp' AND client <> '' AND created_at <> ''`); c != 2 {
		t.Errorf("%d version rows with an actor, a client and a time, want 2", c)
	}
	if c := count(t, w, `SELECT COUNT(*) FROM mcp_settings_audit`); c != 0 {
		t.Errorf("a note wrote %d settings audit rows, want none", c)
	}
	// A search for the secret itself finds nothing: it was never stored.
	if got := list(t, srv, map[string]any{"query": secrets[0]}); len(got) != 0 {
		t.Errorf("a search for the secret found %d notes", len(got))
	}
}

// A refusal never echoes the caller's text, so a secret in a refused request
// does not come back in the error.
func TestRefusalsDoNotEchoTheCallersText(t *testing.T) {
	srv, _ := newServer(t)
	r := srv.call(t, "research_note_create", map[string]any{"question": secrets[0], "conclusion": secrets[1]})
	if r.status == 200 {
		t.Fatalf("accepted: %s", r.body)
	}
	assertNoSecret(t, "error response", string(r.body))
	long := srv.call(t, "research_note_create", map[string]any{"question": "q", "hypothesis": strings.Repeat("あ", 1001) + secrets[0]})
	assertNoSecret(t, "error response", string(long.body))
}
