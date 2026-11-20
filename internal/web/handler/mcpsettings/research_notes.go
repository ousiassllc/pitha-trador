package mcpsettings

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/researchnote"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/shared"
	"github.com/ousiassllc/pitha-trador/internal/web/organisms/mcp"
)

// ResearchNoteGate is the Settings screen's read-only side of the research
// notes; the application's *mcpset.Gate implements it. A gate without it
// (some tests') simply shows no research-note section.
type ResearchNoteGate interface {
	ResearchNotes(ctx context.Context, limit int) ([]researchnote.Row, error)
}

// researchNoteRows is how many notes the list shows (newest first).
const researchNoteRows = 20

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
// research notes, read-only. It changes nothing.
func (h *Handler) ResearchNotes(c *gin.Context) {
	noStore(c)
	ctx := c.Request.Context()
	gate, _ := h.researchNoteGate()
	rows, err := gate.ResearchNotes(ctx, researchNoteRows)
	if err != nil {
		slog.ErrorContext(ctx, "mcpsettings: list research notes", "error", err)
		shared.RespondActionError(c, http.StatusInternalServerError, "研究ノートの一覧を取得できませんでした。")
		return
	}
	shared.RenderHTML(c, http.StatusOK, mcp.ResearchNotes(researchNotesProps(rows)))
}

func researchNotesProps(rows []researchnote.Row) mcp.ResearchNotesProps {
	props := mcp.ResearchNotesProps{Rows: make([]mcp.ResearchNoteRow, 0, len(rows))}
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
