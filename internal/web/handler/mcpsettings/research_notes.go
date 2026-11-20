package mcpsettings

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/researchnote"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/shared"
	"github.com/ousiassllc/pitha-trador/internal/web/organisms/mcp"
)

// ResearchNoteGate is the Settings screen's read-only side of the research
// notes; the application's *mcpset.Gate implements it. A gate without it
// (some tests') simply shows no research-note section.
type ResearchNoteGate interface {
	ResearchNotes(ctx context.Context, f researchnote.Filter, limit int) ([]researchnote.Row, error)
}

// researchNoteRows is how many notes the list shows (newest first), filtered
// or not. There is no paging: a search that matches more shows the newest
// researchNoteRows and says so.
const researchNoteRows = 20

// researchNoteFilterError is the inline-alert slot of the filter form
// (mcp.ResearchNotes): a rejected filter puts its reason there.
const researchNoteFilterError = "#mcp-research-notes-filter-error"

// researchNoteGate returns the gate's research-note side, when it has one.
func (h *Handler) researchNoteGate() (ResearchNoteGate, bool) {
	if h.gate == nil {
		return nil, false
	}
	g, ok := h.gate.(ResearchNoteGate)
	return g, ok
}

// registerResearchNotes mounts the read-only list when the gate can serve it.
func (h *Handler) registerResearchNotes(engine *gin.Engine) {
	if _, ok := h.researchNoteGate(); ok {
		engine.GET("/settings/mcp/research-notes", h.ResearchNotes)
	}
}

// ResearchNotes implements `GET /settings/mcp/research-notes`: the newest
// research notes, read-only, narrowed by `q` (text search), `tag` and
// `conclusion` the way the MCP's research_notes_list narrows them (the same
// researchnote.List, so the same notes). It changes nothing.
func (h *Handler) ResearchNotes(c *gin.Context) {
	noStore(c)
	ctx := c.Request.Context()
	filter, ok := researchNoteFilter(c)
	if !ok {
		return
	}
	gate, _ := h.researchNoteGate()
	rows, err := gate.ResearchNotes(ctx, filter, researchNoteRows)
	if err != nil {
		slog.ErrorContext(ctx, "mcpsettings: list research notes", "error", err)
		shared.RespondActionError(c, http.StatusInternalServerError, "研究ノートの一覧を取得できませんでした。")
		return
	}
	shared.RenderHTML(c, http.StatusOK, mcp.ResearchNotes(researchNotesProps(rows, filter)))
}

// researchNoteFilter reads the filter from the query string with the limits of
// research_notes_list: q up to MaxQueryRunes characters, tag up to MaxTagRunes,
// conclusion one of the three. A bad value is answered here (422) and ok is
// false.
func researchNoteFilter(c *gin.Context) (f researchnote.Filter, ok bool) {
	f = researchnote.Filter{Query: strings.TrimSpace(c.Query("q")), Tag: strings.TrimSpace(c.Query("tag")), Conclusion: c.Query("conclusion")}
	switch {
	case utf8.RuneCountInString(f.Query) > researchnote.MaxQueryRunes:
		shared.RespondFormError(c, researchNoteFilterError, "検索語は "+strconv.Itoa(researchnote.MaxQueryRunes)+" 文字以内にしてください。")
	case utf8.RuneCountInString(f.Tag) > researchnote.MaxTagRunes:
		shared.RespondFormError(c, researchNoteFilterError, "タグは "+strconv.Itoa(researchnote.MaxTagRunes)+" 文字以内にしてください。")
	case !validConclusionFilter(f.Conclusion):
		shared.RespondFormError(c, researchNoteFilterError, "結論は支持・不支持・保留のどれかを選んでください。")
	default:
		return f, true
	}
	return f, false
}

func validConclusionFilter(code string) bool {
	switch code {
	case "", researchnote.ConclusionSupported, researchnote.ConclusionUnsupported, researchnote.ConclusionPending:
		return true
	}
	return false
}

func researchNotesProps(rows []researchnote.Row, f researchnote.Filter) mcp.ResearchNotesProps {
	props := mcp.ResearchNotesProps{
		Rows: make([]mcp.ResearchNoteRow, 0, len(rows)), Query: f.Query, Tag: f.Tag, Conclusion: f.Conclusion,
		Filtered: f.Query != "" || f.Tag != "" || f.Conclusion != "", Capped: len(rows) >= researchNoteRows, Limit: researchNoteRows,
	}
	for _, r := range rows {
		props.Rows = append(props.Rows, mcp.ResearchNoteRow{
			ID: r.ID, Question: r.Question, Hypothesis: r.Hypothesis, ResultSummary: r.ResultSummary,
			Conclusion: r.Conclusion, ConclusionLabel: conclusionLabel(r.Conclusion), Tags: r.Tags,
			EvidenceCount: r.EvidenceCount, Version: r.CurrentVersion, Updated: jst(r.UpdatedAt),
		})
	}
	return props
}

func conclusionLabel(code string) string {
	switch code {
	case researchnote.ConclusionSupported:
		return "支持"
	case researchnote.ConclusionUnsupported:
		return "不支持"
	case researchnote.ConclusionPending:
		return "保留"
	}
	return code
}
