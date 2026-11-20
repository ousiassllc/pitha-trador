package researchnote

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// Tx is the transaction handle the writes get: *sql.Tx.
type Tx interface {
	query.Q
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// The whole write surface of this package: an INSERT into each of the two
// tables and one UPDATE of the head row. No statement UPDATEs or DELETEs a
// version row.
const (
	insertNoteSQL = `INSERT INTO research_notes
(question, hypothesis, actions, result_summary, conclusion, evidence_links, tags, current_version, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, 1, ?, ?)`
	insertVersionSQL = `INSERT INTO research_note_versions
(note_id, version, question, hypothesis, actions, result_summary, conclusion, evidence_links, tags, actor, client, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	updateHeadSQL = `UPDATE research_notes
SET question = ?, hypothesis = ?, actions = ?, result_summary = ?, conclusion = ?, evidence_links = ?, tags = ?,
    current_version = ?, updated_at = ?
WHERE id = ? AND current_version = ?`
)

// Insert stores f (validated and masked by Fields.Clean) as a new note at
// version 1, in tx, and returns it. client is the caller's host.
func Insert(ctx context.Context, tx Tx, now time.Time, client string, f Fields) (Note, error) {
	f, err := f.Clean()
	if err != nil {
		return Note{}, err
	}
	evidence, tags, err := encodeLists(f)
	if err != nil {
		return Note{}, err
	}
	at := sqlutil.FormatTime(now)
	res, err := tx.ExecContext(ctx, insertNoteSQL, f.Question, f.Hypothesis, f.Actions, f.ResultSummary, f.Conclusion, evidence, tags, at, at)
	if err != nil {
		return Note{}, fmt.Errorf("researchnote: insert note: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Note{}, fmt.Errorf("researchnote: note id: %w", err)
	}
	if err := insertVersion(ctx, tx, id, 1, f, evidence, tags, client, at); err != nil {
		return Note{}, err
	}
	return Get(ctx, tx, id)
}

// Update applies p to note id when its current version is expected, writing
// the next version (the previous ones stay), and returns the note. ErrNotFound
// for an unknown id; a *ConflictError (errors.Is ErrVersionConflict) when the
// note is at another version; an *InvalidError for an empty patch, a patch that
// changes nothing, an invalid result, or a note that has reached
// MaxVersionsPerNote. Nothing is stored then.
func Update(ctx context.Context, tx Tx, now time.Time, client string, id int64, expected int, p Patch) (Note, error) {
	cur, err := head(ctx, tx, id)
	if err != nil {
		return Note{}, err
	}
	if cur.CurrentVersion != expected {
		return Note{}, &ConflictError{Current: cur.CurrentVersion}
	}
	if p.empty() {
		return Note{}, invalid("更新する項目がありません")
	}
	f, err := p.apply(cur.Fields).Clean()
	if err != nil {
		return Note{}, err
	}
	if sameFields(f, cur.Fields) {
		return Note{}, invalid("内容が現在の版と同じです（変更がありません）")
	}
	if cur.CurrentVersion >= MaxVersionsPerNote {
		return Note{}, invalid("1 つのノートの版は %d までです。新しいノートを作ってください", MaxVersionsPerNote)
	}
	evidence, tags, err := encodeLists(f)
	if err != nil {
		return Note{}, err
	}
	next, at := cur.CurrentVersion+1, sqlutil.FormatTime(now)
	if err := insertVersion(ctx, tx, id, next, f, evidence, tags, client, at); err != nil {
		return Note{}, err
	}
	res, err := tx.ExecContext(ctx, updateHeadSQL, f.Question, f.Hypothesis, f.Actions, f.ResultSummary, f.Conclusion, evidence, tags, next, at, id, cur.CurrentVersion)
	if err != nil {
		return Note{}, fmt.Errorf("researchnote: update note %d: %w", id, err)
	}
	if n, err := res.RowsAffected(); err != nil || n == 0 {
		return Note{}, &ConflictError{Current: cur.CurrentVersion}
	}
	return Get(ctx, tx, id)
}

func insertVersion(ctx context.Context, tx Tx, id int64, version int, f Fields, evidence, tags, client, at string) error {
	_, err := tx.ExecContext(ctx, insertVersionSQL, id, version, f.Question, f.Hypothesis, f.Actions, f.ResultSummary, f.Conclusion, evidence, tags, ActorMCP, client, at)
	if err != nil {
		return fmt.Errorf("researchnote: insert version %d of note %d: %w", version, id, err)
	}
	return nil
}

// encodeLists renders the evidence links and tags as the JSON arrays the
// columns hold ("[]" for none; HTML characters are not escaped, so the stored
// text is what the caller wrote).
func encodeLists(f Fields) (evidence, tags string, err error) {
	if evidence, err = encodeJSON(f.EvidenceLinks); err != nil {
		return "", "", err
	}
	if len(evidence) > maxEvidenceJSONLength {
		return "", "", invalid("evidence_links が長すぎます")
	}
	tags, err = encodeJSON(f.Tags)
	return evidence, tags, err
}

func encodeJSON(v any) (string, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return "", fmt.Errorf("researchnote: encode: %w", err)
	}
	return string(bytes.TrimSpace(buf.Bytes())), nil
}

func sameFields(a, b Fields) bool {
	if a.Question != b.Question || a.Hypothesis != b.Hypothesis || a.Actions != b.Actions ||
		a.ResultSummary != b.ResultSummary || a.Conclusion != b.Conclusion ||
		len(a.EvidenceLinks) != len(b.EvidenceLinks) || len(a.Tags) != len(b.Tags) {
		return false
	}
	for i := range a.EvidenceLinks {
		if a.EvidenceLinks[i] != b.EvidenceLinks[i] {
			return false
		}
	}
	for i := range a.Tags {
		if a.Tags[i] != b.Tags[i] {
			return false
		}
	}
	return true
}

// head reads the note's current content, or ErrNotFound.
func head(ctx context.Context, q query.Q, id int64) (Note, error) {
	n, err := scanNote(q.QueryRowContext(ctx, `SELECT `+noteColumns+` FROM research_notes WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Note{}, ErrNotFound
	}
	return n, err
}
