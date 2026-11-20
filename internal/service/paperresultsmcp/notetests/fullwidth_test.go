package notetests

import (
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/researchnote"
)

// fullwidth writes the printable ASCII of s as fullwidth forms and its spaces
// as U+3000, the way an IME types it.
func fullwidth(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= '!' && r <= '~':
			return r + 0xFEE0
		case r == ' ':
			return '　'
		}
		return r
	}, s)
}

// 受け入れ（#1135）: 全角のキー名・接頭辞・Bearer も、保存の前に伏せられる。
// 全角の秘密も半角の秘密も、表・応答・版・一覧のどこにも残らず、伏せた箇所の
// 外の文章（全角を含む）は書いたままである。
func TestFullwidthKeyShapedStringsAreMaskedBeforeStoring(t *testing.T) {
	srv, w := newServer(t)
	bearer := "tok_Zm9vYmFyYmF6cXV4"
	text := "鍵は " + fullwidth("sk-proj-"+"abcdefghijklmnopqrstuv0123456789") +
		" と " + fullwidth("API_KEY="+secrets[5]) +
		"、" + fullwidth("Bearer "+bearer) + " 以上。ＡＢＣ（１２３）は残る"
	n := create(t, srv, map[string]any{
		"question": "調べもの " + fullwidth("ghp_abcdefghijklmnopqrstuvwxyz0123456789"), "hypothesis": text,
		"tags": []string{"ok", fullwidth("sk-" + "abcdefgh12345678")},
	})
	if want := "鍵は [masked] と ＡＰＩ＿ＫＥＹ＝[masked]、Ｂｅａｒｅｒ　[masked] 以上。ＡＢＣ（１２３）は残る"; n.Hypothesis != want {
		t.Errorf("hypothesis = %q, want %q", n.Hypothesis, want)
	}
	if n.Question != "調べもの [masked]" || n.Tags[0] != "ok" || n.Tags[1] != "[masked]" {
		t.Errorf("question = %q, tags = %q", n.Question, n.Tags)
	}

	var up researchnote.Note
	update(t, srv, n.ID, 1, map[string]any{"actions": fullwidth("password=" + secrets[5])}).data(t, "research_note_update", &up)
	if up.Actions != "ｐａｓｓｗｏｒｄ＝[masked]" {
		t.Errorf("updated actions = %q", up.Actions)
	}

	forms := []string{
		"abcdefghijklmnopqrstuv0123456789", "ｓｋ－ｐｒｏｊ", "ｇｈｐ＿ａｂｃｄ", bearer, fullwidth(bearer), fullwidth(secrets[5]), fullwidth(secrets[1]),
	}
	forms = append(forms, secrets...)
	dumps := map[string]string{
		"list": string(srv.call(t, "research_notes_list", map[string]any{}).body),
		"get":  string(srv.call(t, "research_note_get", map[string]any{"note_id": n.ID}).body),
	}
	for _, table := range []string{"research_notes", "research_note_versions", "mcp_settings_audit"} {
		dumps["table "+table] = dumpTable(t, w, `SELECT * FROM `+table)
	}
	for where, body := range dumps {
		for _, s := range forms[3:] {
			if strings.Contains(body, s) {
				t.Errorf("%s holds the secret %q: %.300s", where, s, body)
			}
		}
		for _, key := range []string{"abcdefghijklmnopqrstuv0123456789", "ｇｈｐ＿ａｂｃｄ", "ｓｋ－ｐｒｏｊ"} {
			if strings.Contains(body, key) {
				t.Errorf("%s holds the secret prefix %q", where, key)
			}
		}
	}
}

// A refused request with a fullwidth secret does not echo it either.
func TestRefusalsDoNotEchoAFullwidthSecret(t *testing.T) {
	srv, _ := newServer(t)
	fwSecret := fullwidth(secrets[1])
	r := srv.call(t, "research_note_create", map[string]any{"question": fwSecret, "conclusion": fullwidth(secrets[0])})
	if r.status == 200 {
		t.Fatalf("accepted: %s", r.body)
	}
	long := srv.call(t, "research_note_create", map[string]any{"question": "q", "hypothesis": strings.Repeat("あ", 1001) + fwSecret})
	for _, body := range []string{string(r.body), string(long.body)} {
		assertNoSecret(t, "error response", body)
		for _, s := range []string{fwSecret, fullwidth(secrets[0])} {
			if strings.Contains(body, s) {
				t.Errorf("error response echoes %q: %s", s, body)
			}
		}
	}
}
