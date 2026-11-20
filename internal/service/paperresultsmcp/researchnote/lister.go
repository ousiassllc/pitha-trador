package researchnote

import (
	"context"
	"database/sql"
	"fmt"
)

// Lister is the Settings screen's read-only list of notes (issue #1095): the
// application's *mcpset.Gate embeds one, so the Gate serves the list without
// mcpset holding any note code. It reads only; the notes are written by the
// research-note tools (notetool) through mcpset.Writer.InTx.
type Lister struct{ db *sql.DB }

// NewLister returns a Lister over the application's database.
func NewLister(db *sql.DB) *Lister { return &Lister{db: db} }

// ResearchNotes lists up to limit notes matching f, most recently updated
// first, in one read-only transaction (issue #1132): the same List as the MCP's
// research_notes_list, so a search shows the same notes. The zero Filter lists
// everything.
func (l *Lister) ResearchNotes(ctx context.Context, f Filter, limit int) ([]Row, error) {
	tx, err := l.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, fmt.Errorf("researchnote: begin read transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	return List(ctx, tx, f, limit)
}
