package notetests

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp"
)

var (
	day1 = time.Date(2026, 10, 14, 1, 0, 0, 0, time.UTC)
	day2 = time.Date(2026, 10, 15, 1, 0, 0, 0, time.UTC)
	day3 = time.Date(2026, 10, 16, 1, 0, 0, 0, time.UTC)
)

// 受け入れ: MCP から書き込み・一覧・検索・更新ができ、更新前の版が残る。
func TestCreateListSearchUpdateKeepsEarlierVersions(t *testing.T) {
	srv, w := newServer(t)
	srv.tick(t, day1)
	first := create(t, srv, noteArgs("出来高と 30 分リターンの関係", nil))
	srv.tick(t, day2)
	second := create(t, srv, noteArgs("スプレッドとコストの関係", map[string]any{
		"conclusion": "supported", "tags": []string{"spread"}, "hypothesis": "", "actions": "", "result_summary": "広いと負ける",
	}))

	if first.ID == 0 || first.CurrentVersion != 1 || first.Conclusion != "unsupported" || first.UpdatedAt != "2026-10-14T01:00:00Z" {
		t.Fatalf("created note = %+v", first)
	}
	if len(first.Tags) != 2 || first.Tags[0] != "volume" || len(first.EvidenceLinks) != 2 {
		t.Errorf("tags / evidence = %v / %v, want lower-cased tags and both links", first.Tags, first.EvidenceLinks)
	}

	rows := list(t, srv, nil)
	if len(rows) != 2 || rows[0].ID != second.ID || rows[1].ID != first.ID {
		t.Fatalf("list = %+v, want newest (second) first", rows)
	}
	if rows[1].EvidenceCount != 2 || rows[1].CurrentVersion != 1 || rows[1].ResultSummary != "コスト控除後は改善せず" {
		t.Errorf("light row = %+v", rows[1])
	}

	// Search: substring over the text fields and the tags, ASCII case-insensitive.
	for query, want := range map[string][]int64{
		"スプレッド":                    {second.ID},
		"RESEARCH_HORIZON":         {first.ID},
		"広いと":                      {second.ID},
		"volume":                   {first.ID},
		"horizon":                  {first.ID},
		"の関係":                      {second.ID, first.ID},
		"nothing like this at all": nil,
	} {
		var got []int64
		for _, r := range list(t, srv, map[string]any{"query": query}) {
			got = append(got, r.ID)
		}
		if len(got) != len(want) || (len(got) > 0 && got[0] != want[0]) {
			t.Errorf("query %q = %v, want %v", query, got, want)
		}
	}
	if got := list(t, srv, map[string]any{"tag": "SPREAD"}); len(got) != 1 || got[0].ID != second.ID {
		t.Errorf("tag filter = %+v", got)
	}
	if got := list(t, srv, map[string]any{"conclusion": "unsupported"}); len(got) != 1 || got[0].ID != first.ID {
		t.Errorf("conclusion filter = %+v", got)
	}
	// Period: updated_at in the JST days [10-15, 10-16) holds only the second.
	if got := list(t, srv, map[string]any{"from": "2026-10-15", "to": "2026-10-16"}); len(got) != 1 || got[0].ID != second.ID {
		t.Errorf("period filter = %+v", got)
	}
	if got := list(t, srv, map[string]any{"limit": 1}); len(got) != 1 || got[0].ID != second.ID {
		t.Errorf("limit 1 = %+v", got)
	}

	// Update: a new version; the earlier one stays readable and unchanged.
	srv.tick(t, day3)
	var updated struct {
		ID             int64    `json:"id"`
		CurrentVersion int      `json:"current_version"`
		Conclusion     string   `json:"conclusion"`
		Tags           []string `json:"tags"`
		Question       string   `json:"question"`
	}
	update(t, srv, first.ID, 1, map[string]any{"conclusion": "supported", "result_summary": "取引量で割ると改善した", "tags": []string{"volume", "rerun"}}).
		data(t, "research_note_update", &updated)
	if updated.CurrentVersion != 2 || updated.Conclusion != "supported" || len(updated.Tags) != 2 || updated.Question != first.Question {
		t.Fatalf("updated note = %+v (a field that was not given must keep its value)", updated)
	}
	if rows := list(t, srv, nil); rows[0].ID != first.ID || rows[0].CurrentVersion != 2 || rows[0].Conclusion != "supported" {
		t.Errorf("list after the update = %+v, want the updated note first at version 2", rows)
	}
	d := get(t, srv, first.ID)
	if len(d.Versions) != 2 || d.Note.CurrentVersion != 2 {
		t.Fatalf("detail = %+v, want both versions", d)
	}
	v1, v2 := d.Versions[0], d.Versions[1]
	if v1.Version != 1 || v1.Conclusion != "unsupported" || v1.ResultSummary != "コスト控除後は改善せず" || len(v1.Tags) != 2 || v1.CreatedAt != "2026-10-14T01:00:00Z" {
		t.Errorf("version 1 changed or lost: %+v", v1)
	}
	if v2.Version != 2 || v2.Conclusion != "supported" || v2.ResultSummary != "取引量で割ると改善した" || v2.Question != first.Question || v2.CreatedAt != "2026-10-16T01:00:00Z" {
		t.Errorf("version 2 = %+v", v2)
	}
	if v1.Actor != "mcp" || v1.Client == "" {
		t.Errorf("version 1 actor / client = %q / %q, want the MCP and the caller's host", v1.Actor, v1.Client)
	}
	if n := count(t, w, `SELECT COUNT(*) FROM research_note_versions`); n != 3 {
		t.Errorf("%d version rows, want 3 (2 for the first note, 1 for the second)", n)
	}
}

// 更新の衝突: a stale expected_version is refused with a clear code and nothing is written.
func TestUpdateWithStaleExpectedVersionIsAConflict(t *testing.T) {
	srv, w := newServer(t)
	n := create(t, srv, noteArgs("q", nil))
	update(t, srv, n.ID, 1, map[string]any{"result_summary": "v2"}).data(t, "research_note_update", &struct{}{})

	r := update(t, srv, n.ID, 1, map[string]any{"result_summary": "stale writer"})
	r.wantError(t, http.StatusConflict, paperresultsmcp.CodeResearchNoteVersionConflict)
	if !strings.Contains(string(r.body), "current_version は 2") {
		t.Errorf("the conflict does not say the current version: %s", r.body)
	}
	update(t, srv, n.ID, 3, map[string]any{"result_summary": "from the future"}).wantError(t, http.StatusConflict, paperresultsmcp.CodeResearchNoteVersionConflict)
	if d := get(t, srv, n.ID); len(d.Versions) != 2 || d.Note.ResultSummary != "v2" {
		t.Fatalf("a refused update wrote: %+v", d)
	}
	update(t, srv, n.ID, 2, map[string]any{"result_summary": "v3"}).data(t, "research_note_update", &struct{}{})
	if n := count(t, w, `SELECT COUNT(*) FROM research_note_versions WHERE note_id = ?`, n.ID); n != 3 {
		t.Fatalf("%d versions, want 3", n)
	}
}

// 書き込み許可: create / update follow the write switch exactly like the
// experiments; list / get do not.
func TestNoteWritesFollowTheWriteSwitch(t *testing.T) {
	srv, w := newServer(t)
	n := create(t, srv, noteArgs("q", nil))
	srv.call(t, "settings_write_disable", map[string]any{}).data(t, "settings_write_disable", &struct{}{})

	srv.call(t, "research_note_create", noteArgs("second", nil)).wantError(t, http.StatusForbidden, paperresultsmcp.CodeWriteDisabled)
	update(t, srv, n.ID, 1, map[string]any{"result_summary": "x"}).wantError(t, http.StatusForbidden, paperresultsmcp.CodeWriteDisabled)
	if c, v := count(t, w, `SELECT COUNT(*) FROM research_notes`), count(t, w, `SELECT COUNT(*) FROM research_note_versions`); c != 1 || v != 1 {
		t.Fatalf("%d notes / %d versions written while the switch was off, want 1 / 1", c, v)
	}
	if rows := list(t, srv, nil); len(rows) != 1 {
		t.Errorf("list while writes are off = %d rows", len(rows))
	}
	if d := get(t, srv, n.ID); len(d.Versions) != 1 {
		t.Errorf("get while writes are off = %+v", d)
	}
	srv.call(t, "settings_write_enable", map[string]any{}).data(t, "settings_write_enable", &struct{}{})
	create(t, srv, noteArgs("second", nil))
}

// A search for % or _ or \ finds that character, not "anything".
func TestSearchTreatsLikeWildcardsAsPlainCharacters(t *testing.T) {
	srv, _ := newServer(t)
	percent := create(t, srv, noteArgs("勝率 50% の条件", map[string]any{"hypothesis": "", "actions": "", "result_summary": "", "tags": []string{}}))
	create(t, srv, noteArgs("勝率 50 の条件", map[string]any{"hypothesis": "", "actions": "", "result_summary": "", "tags": []string{}}))
	under := create(t, srv, noteArgs("risk_paper の設定", map[string]any{"hypothesis": "", "actions": "", "result_summary": "", "tags": []string{}}))
	create(t, srv, noteArgs("riskXpaper の設定", map[string]any{"hypothesis": "", "actions": "", "result_summary": "", "tags": []string{}}))
	slash := create(t, srv, noteArgs(`パス C:\temp の話`, map[string]any{"hypothesis": "", "actions": "", "result_summary": "", "tags": []string{}}))

	for query, want := range map[string]int64{"50%": percent.ID, "risk_paper": under.ID, `C:\temp`: slash.ID} {
		if got := list(t, srv, map[string]any{"query": query}); len(got) != 1 || got[0].ID != want {
			t.Errorf("query %q = %+v, want only note %d", query, got, want)
		}
	}
	if got := list(t, srv, map[string]any{"query": "%"}); len(got) != 1 {
		t.Errorf("query \"%%\" = %d rows, want only the note that holds a percent sign", len(got))
	}
}

func TestInvalidRequestsStoreNothing(t *testing.T) {
	srv, w := newServer(t)
	n := create(t, srv, noteArgs("q", nil))
	links21 := make([]map[string]any, 21)
	for i := range links21 {
		links21[i] = map[string]any{"kind": "other", "ref": "x"}
	}
	for name, args := range map[string]map[string]any{
		"no question":         {"hypothesis": "h"},
		"empty question":      noteArgs("  ", nil),
		"long question":       noteArgs(strings.Repeat("あ", 501), nil),
		"long result":         noteArgs("q", map[string]any{"result_summary": strings.Repeat("x", 3001)}),
		"unknown conclusion":  noteArgs("q", map[string]any{"conclusion": "maybe"}),
		"unknown argument":    noteArgs("q", map[string]any{"owner": "x"}),
		"21 links":            noteArgs("q", map[string]any{"evidence_links": links21}),
		"unknown link kind":   noteArgs("q", map[string]any{"evidence_links": []map[string]any{{"kind": "blog", "ref": "x"}}}),
		"empty link ref":      noteArgs("q", map[string]any{"evidence_links": []map[string]any{{"kind": "url", "ref": ""}}}),
		"unknown link field":  noteArgs("q", map[string]any{"evidence_links": []map[string]any{{"kind": "url", "ref": "x", "note": "y"}}}),
		"11 tags":             noteArgs("q", map[string]any{"tags": []string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "10", "11"}}),
		"long tag":            noteArgs("q", map[string]any{"tags": []string{strings.Repeat("t", 41)}}),
		"control character":   noteArgs("q\x00", nil),
		"question not string": noteArgs("q", map[string]any{"question": 5}),
	} {
		if r := srv.call(t, "research_note_create", args); r.status == http.StatusOK {
			t.Errorf("create %s: accepted: %s", name, r.body)
		}
	}
	for name, args := range map[string]map[string]any{
		"nothing to change": {},
		"same content":      {"question": "q"},
		"bad conclusion":    {"conclusion": "maybe"},
		"empty question":    {"question": " "},
		"unknown field":     {"title": "x"},
	} {
		if r := update(t, srv, n.ID, 1, args); r.status == http.StatusOK {
			t.Errorf("update %s: accepted: %s", name, r.body)
		}
	}
	update(t, srv, 999, 1, map[string]any{"question": "x"}).wantError(t, http.StatusNotFound, paperresultsmcp.CodeResearchNoteNotFound)
	srv.call(t, "research_note_get", map[string]any{"note_id": 999}).wantError(t, http.StatusNotFound, paperresultsmcp.CodeResearchNoteNotFound)
	srv.call(t, "research_note_update", map[string]any{"note_id": n.ID, "question": "x"}).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeInvalidArgument)
	srv.call(t, "research_notes_list", map[string]any{"limit": 101}).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeLimitExceeded)
	srv.call(t, "research_notes_list", map[string]any{"conclusion": "x"}).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeInvalidArgument)
	srv.call(t, "research_notes_list", map[string]any{"from": "2026-10-01"}).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeInvalidPeriod)
	srv.call(t, "research_notes_list", map[string]any{"query": strings.Repeat("a", 201)}).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeInvalidArgument)

	if c, v := count(t, w, `SELECT COUNT(*) FROM research_notes`), count(t, w, `SELECT COUNT(*) FROM research_note_versions`); c != 1 || v != 1 {
		t.Fatalf("refused requests left %d notes / %d versions, want 1 / 1", c, v)
	}
}

func TestVersionsPerNoteAreBounded(t *testing.T) {
	srv, _ := newServer(t)
	n := create(t, srv, noteArgs("q", nil))
	for v := 1; v < 50; v++ {
		update(t, srv, n.ID, v, map[string]any{"result_summary": strings.Repeat("r", v)}).data(t, "research_note_update", &struct{}{})
	}
	update(t, srv, n.ID, 50, map[string]any{"result_summary": "one too many"}).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeInvalidArgument)
	if d := get(t, srv, n.ID); len(d.Versions) != 50 {
		t.Fatalf("%d versions, want the bound 50", len(d.Versions))
	}
}
