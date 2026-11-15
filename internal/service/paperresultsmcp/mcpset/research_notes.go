package mcpset

import (
	"context"
	"database/sql"

	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/researchnote"
)

// The research-note side of the Writer (issue #1095). Its whole write surface
// is two transactions: CreateResearchNote (one research_notes INSERT and the
// version 1 INSERT) and UpdateResearchNote (the next version's INSERT and one
// UPDATE of the note's head row). It touches no setting, audit row, position
// or signal. Like the experiments, the rows carry their own actor and client,
// so there is no mcp_settings_audit row.

// CreateResearchNote stores f as a new note (version 1), behind the write
// switch (ErrWriteDisabled while it is off, like settings_set). client is the
// caller's host. f is validated and masked by researchnote.Insert.
func (w *Writer) CreateResearchNote(ctx context.Context, client string, f researchnote.Fields) (researchnote.Note, error) {
	var out researchnote.Note
	err := runInTx(ctx, w.db, "research note transaction", func(tx *sql.Tx) error {
		if err := requireWriteSwitch(ctx, tx); err != nil {
			return err
		}
		var err error
		out, err = researchnote.Insert(ctx, tx, w.now(), client, f)
		return err
	})
	return out, err
}

// UpdateResearchNote writes the next version of note id when its current
// version is expected (researchnote.ErrVersionConflict otherwise), behind the
// write switch. The earlier versions stay.
func (w *Writer) UpdateResearchNote(ctx context.Context, client string, id int64, expected int, p researchnote.Patch) (researchnote.Note, error) {
	var out researchnote.Note
	err := runInTx(ctx, w.db, "research note update transaction", func(tx *sql.Tx) error {
		if err := requireWriteSwitch(ctx, tx); err != nil {
			return err
		}
		var err error
		out, err = researchnote.Update(ctx, tx, w.now(), client, id, expected, p)
		return err
	})
	return out, err
}

// requireWriteSwitch is ErrWriteDisabled while the write switch is off.
func requireWriteSwitch(ctx context.Context, tx *sql.Tx) error {
	on, err := WriteEnabled(ctx, system.NewRuntimeSettingsRepositoryOn(tx))
	if err != nil {
		return err
	}
	if !on {
		return ErrWriteDisabled
	}
	return nil
}

// ResearchNotes lists up to limit notes, most recently updated first, for the
// Settings screen's read-only list.
func (g *Gate) ResearchNotes(ctx context.Context, limit int) ([]researchnote.Row, error) {
	var out []researchnote.Row
	err := g.readTx(ctx, func(tx *sql.Tx) (err error) {
		out, err = researchnote.List(ctx, tx, researchnote.Filter{}, limit)
		return err
	})
	return out, err
}
