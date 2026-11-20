package mcpsettings_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/mcpset"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/researchnote"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/researchnote/notetool"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/mcpsettings"
)

var noteIDAttr = regexp.MustCompile(`data-note-id="(\d+)"`)

// notesOnlyGate is a write gate whose only working part is the research-note
// list of the real gate (the other methods are never called by the list route).
type notesOnlyGate struct {
	mcpsettings.WriteGate
	gate *mcpset.Gate
}

func (g notesOnlyGate) ResearchNotes(ctx context.Context, f researchnote.Filter, limit int) ([]researchnote.Row, error) {
	return g.gate.ResearchNotes(ctx, f, limit)
}

// filterNotes is the Settings handler on a real gate with five notes written
// through the research-note tools, and the MCP's research_notes_list over the
// same database, to compare the screen with.
type filterNotes struct {
	engine *gin.Engine
	list   func(args string) []int64
	add    func(n researchnote.Fields) int64
	ids    map[string]int64
}

func newFilterNotes(t *testing.T) *filterNotes {
	t.Helper()
	path, db := testkit.NewMigratedDB(t)
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	mcpsettings.New(nil).WithWriteGate(notesOnlyGate{gate: mcpset.NewGate(db)}).Register(engine)

	w, err := mcpset.OpenWriter("sqlite", path, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	at := time.Date(2026, 10, 14, 1, 0, 0, 0, time.UTC)
	w.SetClock(func() time.Time { return at })
	f := &filterNotes{engine: engine, ids: map[string]int64{}}
	f.add = func(n researchnote.Fields) int64 {
		t.Helper()
		at = at.Add(time.Hour)
		note, err := notetool.CreateNote(t.Context(), w, "100.64.0.9", n)
		if err != nil {
			t.Fatal(err)
		}
		return note.ID
	}
	for _, n := range []researchnote.Fields{
		{Question: "出来高と 30 分リターン", Conclusion: researchnote.ConclusionUnsupported, Tags: []string{"Volume", "horizon"}},
		{Question: "スプレッドとコスト", ResultSummary: "広いと負ける", Conclusion: researchnote.ConclusionSupported, Tags: []string{"spread"}},
		{Question: "板の厚み", Hypothesis: "50% 厚いと改善", Tags: []string{"volume"}},
		{Question: "メモ", ResultSummary: "鍵は sk-abcdefghijklmnop1234 でした", Tags: []string{"memo"}},
		{Question: "時間帯", Conclusion: researchnote.ConclusionSupported, Tags: []string{"volume", "session"}},
	} {
		f.ids[n.Question] = f.add(n)
	}
	f.list = func(args string) []int64 {
		t.Helper()
		for _, spec := range notetool.Specs() {
			if spec.Name != "research_notes_list" {
				continue
			}
			run, err := spec.Decode(json.RawMessage(args))
			if err != nil {
				t.Fatalf("research_notes_list %s: %v", args, err)
			}
			data, _, err := run(t.Context(), notetool.Env{Q: db, Now: at})
			if err != nil {
				t.Fatal(err)
			}
			var out []int64
			for _, r := range data.(map[string]any)["notes"].([]researchnote.Row) {
				out = append(out, r.ID)
			}
			return out
		}
		t.Fatal("no research_notes_list tool")
		return nil
	}
	return f
}

// request is a request to the list route with any method and no body.
func (f *filterNotes) request(method, rawQuery string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	f.engine.ServeHTTP(rec, httptest.NewRequest(method, "/settings/mcp/research-notes?"+rawQuery, nil))
	return rec
}

// screen returns the note ids the Settings list shows, in order.
func (f *filterNotes) screen(t *testing.T, rawQuery string) []int64 {
	t.Helper()
	rec := f.request(http.MethodGet, rawQuery)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET ?%s = %d: %s", rawQuery, rec.Code, rec.Body.String())
	}
	var out []int64
	for _, m := range noteIDAttr.FindAllStringSubmatch(rec.Body.String(), -1) {
		id, _ := strconv.ParseInt(m[1], 10, 64)
		out = append(out, id)
	}
	return out
}

func sameIDs(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// A search word, a tag and a conclusion show on the screen the notes the MCP's
// research_notes_list returns for the same arguments, in the same order, and a
// note that does not match is absent.
func TestResearchNotesFilterMatchesTheMCPList(t *testing.T) {
	f := newFilterNotes(t)
	for _, c := range []struct{ query, args string }{
		{"", `{}`},
		{"q=" + url.QueryEscape("スプレッド"), `{"query":"スプレッド"}`},
		{"q=VOLUME", `{"query":"VOLUME"}`},
		{"q=" + url.QueryEscape("50%"), `{"query":"50%"}`},
		{"q=" + url.QueryEscape("%"), `{"query":"%"}`},
		{"tag=volume", `{"tag":"volume"}`},
		{"tag=Volume", `{"tag":"Volume"}`},
		{"conclusion=supported", `{"conclusion":"supported"}`},
		{"tag=volume&conclusion=supported", `{"tag":"volume","conclusion":"supported"}`},
		{"q=" + url.QueryEscape(" 厚い ") + "&tag=volume", `{"query":"厚い","tag":"volume"}`},
		{"q=nothing+like+this", `{"query":"nothing like this"}`},
		{"tag=spre", `{"tag":"spre"}`},
	} {
		got, want := f.screen(t, c.query), f.list(c.args)
		if !sameIDs(got, want) {
			t.Errorf("?%s shows %v, research_notes_list %s returns %v", c.query, got, c.args, want)
		}
	}
	if got := f.screen(t, "tag=volume"); len(got) != 3 {
		t.Errorf("tag volume shows %v, want the 3 notes with it", got)
	}
	if got := f.screen(t, "tag=volume&conclusion=supported"); len(got) != 1 || got[0] != f.ids["時間帯"] {
		t.Errorf("tag + conclusion shows %v, want only the 時間帯 note", got)
	}
	if got := f.screen(t, "q=nothing+like+this"); len(got) != 0 {
		t.Errorf("a search with no match shows %v", got)
	}
}

// The form shows the applied filter again; an empty result says no note matches.
func TestResearchNotesFilterFormShowsTheAppliedFilter(t *testing.T) {
	f := newFilterNotes(t)
	body := f.request(http.MethodGet, "q="+url.QueryEscape(`"><script>x</script>`)+"&tag=volume&conclusion=pending").Body.String()
	for _, want := range []string{
		`data-testid="mcp-research-notes-filter"`, `hx-get="/settings/mcp/research-notes"`, `method="get"`,
		`value="volume"`, `value="pending" selected`, "条件に合う研究ノートはありません",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the page does not contain %q: %s", want, body)
		}
	}
	if strings.Contains(body, "<script>x") {
		t.Errorf("the search word is not escaped: %s", body)
	}
}

// Over-long or unknown filter values are refused with the reason, as the MCP
// refuses them, and show nothing.
func TestResearchNotesFilterRejectsBadValues(t *testing.T) {
	f := newFilterNotes(t)
	for _, raw := range []string{
		"q=" + strings.Repeat("a", researchnote.MaxQueryRunes+1),
		"tag=" + strings.Repeat("a", researchnote.MaxTagRunes+1),
		"conclusion=maybe",
	} {
		rec := f.request(http.MethodGet, raw)
		if rec.Code != http.StatusUnprocessableEntity || strings.Contains(rec.Body.String(), "mcp-research-note-row") {
			t.Errorf("?%.40s = %d: %s", raw, rec.Code, rec.Body.String())
		}
	}
	// The limits are inclusive, like research_notes_list's.
	if rec := f.request(http.MethodGet, "q="+strings.Repeat("a", researchnote.MaxQueryRunes)); rec.Code != http.StatusOK {
		t.Errorf("a search at the limit = %d", rec.Code)
	}
}

// A note holding a key-shaped string shows only its stored, masked text; the
// raw key is neither on the screen nor findable by searching for it.
func TestResearchNotesFilterShowsOnlyTheMaskedBody(t *testing.T) {
	f := newFilterNotes(t)
	body := f.request(http.MethodGet, "q=memo").Body.String()
	if !strings.Contains(body, "[masked]") || strings.Contains(body, "sk-abcdefghijklmnop1234") {
		t.Errorf("the masked body is not shown as stored: %s", body)
	}
	if got := f.screen(t, "q=sk-abcdefghijklmnop1234"); len(got) != 0 {
		t.Errorf("searching for the raw key found %v", got)
	}
}

// The list stays read-only with filters: no write verb is routed for the list,
// and the tag chips are links to the same GET list.
func TestResearchNotesFilterAddsNoWriteRoute(t *testing.T) {
	f := newFilterNotes(t)
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		if rec := f.request(method, "tag=volume"); rec.Code != http.StatusNotFound && rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s /settings/mcp/research-notes = %d, want no such route", method, rec.Code)
		}
	}
	body := f.request(http.MethodGet, "").Body.String()
	if !strings.Contains(body, `hx-get="/settings/mcp/research-notes?tag=volume"`) || strings.Contains(body, "hx-post") {
		t.Errorf("tag links: %s", body)
	}
}

// The screen never lists more than 20 notes, filtered or not, shows the same
// newest 20 as research_notes_list (limit 20) and says when it stopped there.
func TestResearchNotesListIsCapped(t *testing.T) {
	f := newFilterNotes(t)
	if body := f.request(http.MethodGet, "").Body.String(); strings.Contains(body, "mcp-research-notes-capped") {
		t.Error("the cap notice is shown below the cap")
	}
	for i := range 20 {
		f.add(researchnote.Fields{Question: "cap " + strconv.Itoa(i), Tags: []string{"cap"}})
	}
	for _, raw := range []string{"", "tag=cap", "q=cap"} {
		got := f.screen(t, raw)
		if len(got) != 20 {
			t.Errorf("?%s shows %d notes, want 20", raw, len(got))
		}
		if want := f.list(`{"limit":20}`); raw == "" && !sameIDs(got, want) {
			t.Errorf("the capped list = %v, research_notes_list = %v", got, want)
		}
		if body := f.request(http.MethodGet, raw).Body.String(); !strings.Contains(body, "mcp-research-notes-capped") {
			t.Errorf("?%s: no notice that the list stopped at 20", raw)
		}
	}
}
