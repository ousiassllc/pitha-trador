package mcpsettings

import (
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/researchnote"
)

func TestResearchNotesPropsFormatsTheRows(t *testing.T) {
	props := researchNotesProps([]researchnote.Row{
		{ID: 3, Question: "q3", Conclusion: researchnote.ConclusionSupported, Tags: []string{"a"}, EvidenceCount: 2, CurrentVersion: 4, UpdatedAt: "2026-10-14T01:30:00Z"},
		{ID: 2, Question: "q2", Conclusion: researchnote.ConclusionUnsupported, CurrentVersion: 1, UpdatedAt: "bad"},
		{ID: 1, Question: "q1", Conclusion: researchnote.ConclusionPending, CurrentVersion: 1},
	}, researchnote.Filter{})
	if len(props.Rows) != 3 {
		t.Fatalf("%d rows, want 3", len(props.Rows))
	}
	if r := props.Rows[0]; r.ConclusionLabel != "支持" || r.Updated != "2026-10-14 10:30" || r.Version != 4 || r.EvidenceCount != 2 || len(r.Tags) != 1 {
		t.Errorf("row 0 = %+v", r)
	}
	if r := props.Rows[1]; r.ConclusionLabel != "不支持" || r.Updated != "bad" {
		t.Errorf("row 1 = %+v (a time that does not parse is shown as it is)", r)
	}
	if r := props.Rows[2]; r.ConclusionLabel != "保留" {
		t.Errorf("row 2 = %+v", r)
	}
	if props.Filtered || props.Capped || props.Limit != researchNoteRows {
		t.Errorf("props = %+v, want no filter, not capped, limit %d", props, researchNoteRows)
	}
	if got := conclusionLabel("other"); got != "other" {
		t.Errorf("an unknown conclusion label = %q", got)
	}
}

// The wording on the screen is product wording: no issue number, no development note.
func TestResearchNotesWordingHasNoDevelopmentNotes(t *testing.T) {
	for _, label := range []string{conclusionLabel(researchnote.ConclusionSupported), conclusionLabel(researchnote.ConclusionUnsupported), conclusionLabel(researchnote.ConclusionPending)} {
		if strings.ContainsAny(label, "#0123456789") {
			t.Errorf("label %q has an issue number or a dev note", label)
		}
	}
}

// The applied filter is handed back to the form, and the list is capped at the
// rows limit (a full page may have more behind it).
func TestResearchNotesPropsKeepsTheFilterAndTheCap(t *testing.T) {
	rows := make([]researchnote.Row, researchNoteRows)
	props := researchNotesProps(rows, researchnote.Filter{Query: "q", Tag: "t", Conclusion: researchnote.ConclusionSupported})
	if !props.Filtered || !props.Capped || props.Query != "q" || props.Tag != "t" || props.Conclusion != researchnote.ConclusionSupported {
		t.Errorf("props = %+v", props)
	}
	if props := researchNotesProps(rows[:researchNoteRows-1], researchnote.Filter{Tag: "t"}); props.Capped || !props.Filtered {
		t.Errorf("a short page is capped: %+v", props)
	}
}
