package mcpset

import (
	"context"
	"database/sql"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/researchnote"
)

// ResearchNotes lists up to limit notes, most recently updated first, for the
// Settings screen's read-only list (issue #1095). The notes are written by the
// research-note tools through Writer.InTx.
func (g *Gate) ResearchNotes(ctx context.Context, limit int) ([]researchnote.Row, error) {
	var out []researchnote.Row
	err := g.readTx(ctx, func(tx *sql.Tx) (err error) {
		out, err = researchnote.List(ctx, tx, researchnote.Filter{}, limit)
		return err
	})
	return out, err
}
