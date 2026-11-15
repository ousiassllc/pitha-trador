package researchnote

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// Note is one stored research_notes row: the current version's content.
type Note struct {
	Fields
	ID             int64  `json:"id"`
	CurrentVersion int    `json:"current_version"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
}

// Version is one stored research_note_versions row: the whole content of that
// version and who wrote it.
type Version struct {
	Fields
	Version   int    `json:"version"`
	Actor     string `json:"actor"`
	Client    string `json:"client"`
	CreatedAt string `json:"created_at"`
}

// Detail is a note with all its versions, oldest first.
type Detail struct {
	Note     Note      `json:"note"`
	Versions []Version `json:"versions"`
}

// Row is a light list row: the question, the start of the result and the
// counts, not the whole content (note_get returns that).
type Row struct {
	ID             int64    `json:"id"`
	Question       string   `json:"question"`
	Hypothesis     string   `json:"hypothesis"`
	ResultSummary  string   `json:"result_summary"`
	Conclusion     string   `json:"conclusion"`
	Tags           []string `json:"tags"`
	EvidenceCount  int      `json:"evidence_count"`
	CurrentVersion int      `json:"current_version"`
	CreatedAt      string   `json:"created_at"`
	UpdatedAt      string   `json:"updated_at"`
}

// Filter narrows List. The zero Filter lists everything.
type Filter struct {
	// Query is a case-insensitive (ASCII) substring searched in the question,
	// hypothesis, actions, result summary and tags. "" is no search.
	Query string
	// Tag is one tag (compared lower-cased and exactly). "" is any.
	Tag string
	// Conclusion is one of the conclusions. "" is any.
	Conclusion string
	// UpdatedFrom / UpdatedTo bound updated_at to [from, to) when non-zero.
	UpdatedFrom, UpdatedTo time.Time
}

const noteColumns = `id, question, hypothesis, actions, result_summary, conclusion, evidence_links, tags, current_version, created_at, updated_at`

const versionColumns = `version, question, hypothesis, actions, result_summary, conclusion, evidence_links, tags, actor, client, created_at`

// Get returns note id's current content, or ErrNotFound.
func Get(ctx context.Context, q query.Q, id int64) (Note, error) { return head(ctx, q, id) }

// GetDetail returns note id with all its versions (at most MaxVersionsPerNote),
// oldest first, or ErrNotFound.
func GetDetail(ctx context.Context, q query.Q, id int64) (Detail, error) {
	n, err := head(ctx, q, id)
	if err != nil {
		return Detail{}, err
	}
	rows, err := q.QueryContext(ctx, `SELECT `+versionColumns+` FROM research_note_versions WHERE note_id = ? ORDER BY version LIMIT ?`, id, MaxVersionsPerNote)
	if err != nil {
		return Detail{}, fmt.Errorf("researchnote: versions of %d: %w", id, err)
	}
	defer func() { _ = rows.Close() }()
	d := Detail{Note: n, Versions: []Version{}}
	for rows.Next() {
		v, err := scanVersion(rows)
		if err != nil {
			return Detail{}, err
		}
		d.Versions = append(d.Versions, v)
	}
	if err := rows.Err(); err != nil {
		return Detail{}, fmt.Errorf("researchnote: versions of %d: %w", id, err)
	}
	return d, nil
}

// List returns up to limit rows matching f, most recently updated first.
func List(ctx context.Context, q query.Q, f Filter, limit int) ([]Row, error) {
	var where []string
	var args []any
	if f.Query != "" {
		pattern := "%" + escapeLike(f.Query) + "%"
		where = append(where, `(question LIKE ? ESCAPE '\' OR hypothesis LIKE ? ESCAPE '\' OR actions LIKE ? ESCAPE '\' OR result_summary LIKE ? ESCAPE '\' OR EXISTS (SELECT 1 FROM json_each(research_notes.tags) WHERE value LIKE ? ESCAPE '\'))`)
		args = append(args, pattern, pattern, pattern, pattern, pattern)
	}
	if f.Tag != "" {
		where = append(where, `EXISTS (SELECT 1 FROM json_each(research_notes.tags) WHERE value = ?)`)
		args = append(args, strings.ToLower(f.Tag))
	}
	if f.Conclusion != "" {
		where = append(where, `conclusion = ?`)
		args = append(args, f.Conclusion)
	}
	if !f.UpdatedFrom.IsZero() {
		where = append(where, `updated_at >= ?`)
		args = append(args, sqlutil.FormatTime(f.UpdatedFrom))
	}
	if !f.UpdatedTo.IsZero() {
		where = append(where, `updated_at < ?`)
		args = append(args, sqlutil.FormatTime(f.UpdatedTo))
	}
	stmt := `SELECT ` + noteColumns + ` FROM research_notes`
	if len(where) > 0 {
		stmt += ` WHERE ` + strings.Join(where, ` AND `)
	}
	rows, err := q.QueryContext(ctx, stmt+` ORDER BY updated_at DESC, id DESC LIMIT ?`, append(args, limit)...)
	if err != nil {
		return nil, fmt.Errorf("researchnote: list: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []Row{}
	for rows.Next() {
		n, err := scanNote(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n.row())
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("researchnote: list: %w", err)
	}
	return out, nil
}

func (n Note) row() Row {
	return Row{
		ID: n.ID, Question: n.Question, Hypothesis: excerpt(n.Hypothesis), ResultSummary: excerpt(n.ResultSummary),
		Conclusion: n.Conclusion, Tags: n.Tags, EvidenceCount: len(n.EvidenceLinks), CurrentVersion: n.CurrentVersion,
		CreatedAt: n.CreatedAt, UpdatedAt: n.UpdatedAt,
	}
}

// excerpt cuts s to listExcerptRunes characters, marking a cut with "…".
func excerpt(s string) string {
	if utf8.RuneCountInString(s) <= listExcerptRunes {
		return s
	}
	return string([]rune(s)[:listExcerptRunes]) + "…"
}

// escapeLike escapes the LIKE wildcards (% _) and the escape character (\), so
// a search for "50%" finds "50%" and not "50" followed by anything.
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

type rowScanner interface{ Scan(dest ...any) error }

func scanNote(r rowScanner) (Note, error) {
	var (
		n                    Note
		evidence, tags       string
		createdAt, updatedAt string
	)
	err := r.Scan(&n.ID, &n.Question, &n.Hypothesis, &n.Actions, &n.ResultSummary, &n.Conclusion, &evidence, &tags,
		&n.CurrentVersion, &createdAt, &updatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Note{}, err
		}
		return Note{}, fmt.Errorf("researchnote: scan note: %w", err)
	}
	if n.EvidenceLinks, n.Tags, err = decodeLists(evidence, tags); err != nil {
		return Note{}, err
	}
	n.CreatedAt, n.UpdatedAt = query.FormatOut(createdAt), query.FormatOut(updatedAt)
	return n, nil
}

func scanVersion(r rowScanner) (Version, error) {
	var (
		v              Version
		evidence, tags string
		createdAt      string
	)
	err := r.Scan(&v.Version, &v.Question, &v.Hypothesis, &v.Actions, &v.ResultSummary, &v.Conclusion, &evidence, &tags,
		&v.Actor, &v.Client, &createdAt)
	if err != nil {
		return Version{}, fmt.Errorf("researchnote: scan version: %w", err)
	}
	if v.EvidenceLinks, v.Tags, err = decodeLists(evidence, tags); err != nil {
		return Version{}, err
	}
	v.CreatedAt = query.FormatOut(createdAt)
	return v, nil
}

func decodeLists(evidence, tags string) ([]Evidence, []string, error) {
	links, names := []Evidence{}, []string{}
	if err := json.Unmarshal([]byte(evidence), &links); err != nil {
		return nil, nil, fmt.Errorf("researchnote: decode evidence_links: %w", err)
	}
	if err := json.Unmarshal([]byte(tags), &names); err != nil {
		return nil, nil, fmt.Errorf("researchnote: decode tags: %w", err)
	}
	return links, names, nil
}
