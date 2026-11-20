package mcproute_test

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/router"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/mcpset"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/researchnote"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/researchnote/notetool"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

// notesGate is the write switch plus the real gate's research-note list.
type notesGate struct {
	*fakeGate
	gate *mcpset.Gate
}

func (g notesGate) ResearchNotes(ctx context.Context, f researchnote.Filter, limit int) ([]researchnote.Row, error) {
	return g.gate.ResearchNotes(ctx, f, limit)
}

// writeMarkup matches anything that would let the research-note section write:
// a POST form (method or formmethod, any case, quoted or not, spaces around =)
// or an htmx write attribute. A GET filter form is allowed.
var writeMarkup = regexp.MustCompile(`(?i)(form)?method\s*=\s*["']?post|hx-(post|put|patch|delete)`)

// The write-markup guard must fire on every spelling it exists to catch, and
// stay quiet on the GET filter form.
func TestWriteMarkupGuardCatchesEverySpelling(t *testing.T) {
	for _, bad := range []string{
		`<form method="post">`, `<form method='post'>`, `<form method='POST'>`, `<FORM METHOD="POST">`,
		`<form method = post>`, `<form method=post>`, `<button formmethod="post">`, `<button FORMMETHOD='Post'>`,
		`<div hx-post="/x">`, `<div HX-POST="/x">`, `<div hx-put="/x">`, `<div HX-Put="/x">`,
		`<div hx-patch="/x">`, `<div HX-PATCH="/x">`, `<div hx-delete="/x">`, `<div Hx-Delete="/x">`,
	} {
		if !writeMarkup.MatchString(bad) {
			t.Errorf("the guard lets %q through", bad)
		}
	}
	for _, ok := range []string{`<form method="get" hx-get="/settings/mcp/research-notes">`, `<form hx-get="/x">`, `<div hx-get="/x">`} {
		if writeMarkup.MatchString(ok) {
			t.Errorf("the guard rejects the read-only %q", ok)
		}
	}
}

// The Settings card loads a read-only research-note list under the write
// section: newest first with the conclusion, tags, update time and version
// count; no form, no write route, and an unstyled screen text has no issue
// number or development note.
func TestResearchNotesSectionIsAReadOnlyList(t *testing.T) {
	path, db := testkit.NewMigratedDB(t)
	_ = path
	g := mcpset.NewGate(db)
	a := newApp(t, fullSecrets{})
	a.engine = router.New(router.WithSecretsStore(fullSecrets{}), router.WithMCPSettings(a.svc),
		router.WithMCPWriteGate(notesGate{fakeGate: &fakeGate{enabled: true, ExperimentGate: g}, gate: g}))
	a.session()

	if body := a.get("/settings/mcp-panel").Body.String(); !strings.Contains(body, `hx-get="/settings/mcp/research-notes"`) {
		t.Fatal("the MCP card does not load the research-note section")
	}
	rec := a.get("/settings/mcp/research-notes")
	if body := rec.Body.String(); rec.Code != http.StatusOK || !strings.Contains(body, "mcp-research-notes-empty") || !strings.Contains(body, "研究ノートはありません") {
		t.Fatalf("empty section = %d: %s", rec.Code, body)
	}

	w, err := mcpset.OpenWriter("sqlite", path, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	at := time.Date(2026, 10, 14, 1, 30, 0, 0, time.UTC)
	w.SetClock(func() time.Time { return at })
	first, err := notetool.CreateNote(t.Context(), w, "100.64.0.9", researchnote.Fields{
		Question: "出来高と 30 分リターンの関係", Hypothesis: "出来高が増えると改善する", ResultSummary: "コスト控除後は改善せず",
		Conclusion: researchnote.ConclusionUnsupported, Tags: []string{"volume", "horizon"},
		EvidenceLinks: []researchnote.Evidence{{Kind: researchnote.KindExperimentID, Ref: "12"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	at = at.Add(time.Hour)
	if _, err := notetool.CreateNote(t.Context(), w, "100.64.0.9", researchnote.Fields{Question: "スプレッドとコスト"}); err != nil {
		t.Fatal(err)
	}
	at = at.Add(time.Hour)
	if _, err := notetool.UpdateNote(t.Context(), w, "100.64.0.9", first.ID, 1, researchnote.Patch{Conclusion: new(researchnote.ConclusionSupported)}); err != nil {
		t.Fatal(err)
	}

	rec = a.get("/settings/mcp/research-notes")
	body := rec.Body.String()
	if rec.Code != http.StatusOK || strings.Count(body, `data-testid="mcp-research-note-row"`) != 2 {
		t.Fatalf("list = %d: %s", rec.Code, body)
	}
	// Newest update first: the note updated last (the first one, version 2) heads the list.
	if strings.Index(body, "出来高と 30 分リターンの関係") > strings.Index(body, "スプレッドとコスト") {
		t.Errorf("the notes are not newest first: %s", body)
	}
	for _, want := range []string{
		`data-conclusion="supported"`, `data-conclusion="pending"`, ">支持<", ">保留<", "版 2", "版 1", "2026-10-14 12:30", "2026-10-14 11:30",
		`<code class="rounded-pill bg-surface px-3">volume</code>`, "根拠リンク 1 件", "コスト控除後は改善せず", "出来高が増えると改善する",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the list does not contain %q: %s", want, body)
		}
	}
	for _, banned := range []string{"#1095", "1095", "issue", "TODO"} {
		if strings.Contains(strings.ToLower(body), strings.ToLower(banned)) {
			t.Errorf("the section contains %q", banned)
		}
	}
	if m := writeMarkup.FindString(body); m != "" {
		t.Errorf("the section contains write markup %q", m)
	}
	// Read-only: there is no write route for notes.
	if rec := a.post("/settings/mcp/research-notes", url.Values{"question": {"x"}}, true); rec.Code == http.StatusOK {
		t.Errorf("POST /settings/mcp/research-notes = %d, want no such route", rec.Code)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM research_notes`).Scan(&n); err != nil || n != 2 {
		t.Errorf("notes = %d (%v), want the 2 written through the writer", n, err)
	}
}

// A gate without the research-note list (the other tests') shows no section and
// mounts no route.
func TestResearchNotesSectionNeedsTheNoteGate(t *testing.T) {
	a := newAppWithGate(t, &fakeGate{enabled: true})
	if body := a.get("/settings/mcp-panel").Body.String(); strings.Contains(body, "research-notes") {
		t.Error("the research-note section is shown without a gate that serves it")
	}
	if rec := a.get("/settings/mcp/research-notes"); rec.Code == http.StatusOK {
		t.Errorf("GET /settings/mcp/research-notes = %d without a gate", rec.Code)
	}
}
