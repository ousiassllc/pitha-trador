package notetests

import (
	"encoding/json"
	"net/http"
	"sync"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp"
)

// Two writers that both read version 1 and update with expected_version 1:
// exactly one wins (version 2), the other is told research_note_version_conflict,
// and the history holds versions 1 and 2 only.
func TestConcurrentUpdatesWithTheSameExpectedVersionHaveOneWinner(t *testing.T) {
	srv, w := newServer(t)
	n := create(t, srv, noteArgs("q", nil))

	start := make(chan struct{})
	results := make([]response, 2)
	var wg sync.WaitGroup
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			results[i] = update(t, srv, n.ID, 1, map[string]any{"result_summary": "writer " + string(rune('A'+i))})
		}()
	}
	close(start)
	wg.Wait()

	won, conflicted := 0, 0
	for _, r := range results {
		var e struct {
			Code string `json:"code"`
		}
		_ = json.Unmarshal(r.body, &e)
		switch {
		case r.status == http.StatusOK:
			won++
		case r.status == http.StatusConflict && e.Code == string(paperresultsmcp.CodeResearchNoteVersionConflict):
			conflicted++
		default:
			t.Errorf("unexpected response: %d %s", r.status, r.body)
		}
	}
	if won != 1 || conflicted != 1 {
		t.Fatalf("%d winners / %d conflicts, want 1 / 1", won, conflicted)
	}
	d := get(t, srv, n.ID)
	if d.Note.CurrentVersion != 2 || len(d.Versions) != 2 || d.Versions[0].Version != 1 || d.Versions[1].Version != 2 {
		t.Errorf("detail = %+v, want current version 2 and versions 1, 2 only", d)
	}
	if c := count(t, w, `SELECT COUNT(*) FROM research_note_versions WHERE note_id = ?`, n.ID); c != 2 {
		t.Errorf("%d version rows, want 2", c)
	}
}

// A query is matched against the text and against each tag; the JSON
// punctuation of the stored tag list ([ ] " ,) is not searchable.
func TestSearchDoesNotMatchTheJSONPunctuationOfTags(t *testing.T) {
	srv, _ := newServer(t)
	n := create(t, srv, noteArgs("q", map[string]any{"tags": []string{"rerun", "volume"}}))
	for _, query := range []string{`"`, `[`, `]`, `","`, `["`} {
		if got := list(t, srv, map[string]any{"query": query}); len(got) != 0 {
			t.Errorf("query %q matched %d notes through the JSON text of the tags, want none", query, len(got))
		}
	}
	for _, query := range []string{"rerun", "ERUN", "volume"} {
		if got := list(t, srv, map[string]any{"query": query}); len(got) != 1 || got[0].ID != n.ID {
			t.Errorf("query %q = %+v, want the note with that tag", query, got)
		}
	}
}
