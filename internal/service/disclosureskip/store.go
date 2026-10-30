package disclosureskip

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

// Store reads and writes the disclosure skip tables (migration 000049) and
// reads jev_decisions. It never writes anything but those tables.
type Store struct {
	db *sql.DB
}

// NewStore returns a Store over db.
func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// WriteFetch stores one fetch and its disclosures in one transaction.
func (s *Store) WriteFetch(ctx context.Context, ev FetchEvent) (err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("disclosureskip: begin write fetch: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	outcome, oldest := "ok", sql.NullString{}
	if !ev.OK {
		outcome = "error"
	}
	if !ev.Oldest.IsZero() {
		oldest = sql.NullString{String: sqlutil.FormatTime(ev.Oldest), Valid: true}
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO disclosure_fetches (symbol, fetched_at, outcome, item_count, truncated, undated_count, oldest_published_at)
VALUES (?, ?, ?, ?, ?, ?, ?)`,
		ev.Symbol, sqlutil.FormatTime(ev.At), outcome, ev.ItemCount, ev.Truncated, ev.Undated, oldest); err != nil {
		return fmt.Errorf("disclosureskip: insert fetch: %w", err)
	}
	for _, d := range ev.Items {
		var link sql.NullString
		if d.URL != "" {
			link = sql.NullString{String: d.URL, Valid: true}
		}
		if _, err = tx.ExecContext(ctx, `
INSERT INTO disclosure_items (symbol, source_id, published_at, title, document_url, first_seen_at)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT (symbol, source_id) DO UPDATE SET
	published_at = excluded.published_at, title = excluded.title, document_url = excluded.document_url`,
			d.Symbol, d.ID, sqlutil.FormatTime(d.PublishedAt), d.Title, link, sqlutil.FormatTime(ev.At)); err != nil {
			return fmt.Errorf("disclosureskip: upsert disclosure: %w", err)
		}
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("disclosureskip: commit write fetch: %w", err)
	}
	return nil
}

// Decision is one Jev entry-candidate decision still without a record.
type Decision struct {
	ID     int64
	Symbol string
	At     time.Time
}

// Pending lists the entry-candidate decisions made at or after since that have
// no record yet, oldest first: the live Jev trader decisions with direction
// LONG or SHORT (the ones Policy turns into entry signals and the ones the
// outcome labeler labels).
func (s *Store) Pending(ctx context.Context, since time.Time, limit int) ([]Decision, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT d.id, d.symbol, d.timestamp
FROM jev_decisions d
WHERE d.decision_type = 'trader' AND d.direction IN ('LONG', 'SHORT') AND d.timestamp >= ?
	AND NOT EXISTS (SELECT 1 FROM disclosure_skip_records r WHERE r.jev_decision_id = d.id)
ORDER BY d.id LIMIT ?`, sqlutil.FormatTime(since), limit)
	if err != nil {
		return nil, fmt.Errorf("disclosureskip: list pending decisions: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []Decision
	for rows.Next() {
		var (
			d  Decision
			at string
		)
		if err := rows.Scan(&d.ID, &d.Symbol, &at); err != nil {
			return nil, fmt.Errorf("disclosureskip: scan pending decision: %w", err)
		}
		if d.At, err = sqlutil.ParseTime(at); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("disclosureskip: list pending decisions: %w", err)
	}
	return out, nil
}

// Disclosures lists the stored disclosures of symbol published in [from, to].
func (s *Store) Disclosures(ctx context.Context, symbol string, from, to time.Time) ([]Disclosure, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT source_id, published_at, title, document_url FROM disclosure_items
WHERE symbol = ? AND published_at >= ? AND published_at <= ? ORDER BY published_at, id`,
		symbol, sqlutil.FormatTime(from), sqlutil.FormatTime(to))
	if err != nil {
		return nil, fmt.Errorf("disclosureskip: list disclosures: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []Disclosure
	for rows.Next() {
		var (
			d    Disclosure
			at   string
			link sql.NullString
		)
		if err := rows.Scan(&d.ID, &at, &d.Title, &link); err != nil {
			return nil, fmt.Errorf("disclosureskip: scan disclosure: %w", err)
		}
		if d.PublishedAt, err = sqlutil.ParseTime(at); err != nil {
			return nil, err
		}
		d.Symbol, d.URL = symbol, link.String
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("disclosureskip: list disclosures: %w", err)
	}
	return out, nil
}

// maxFetchRows bounds the fetches one evaluation reads (the evaluation window
// is an hour of one-minute polls).
const maxFetchRows = 500

// Fetches lists the recorded fetches of symbol that started in [from, to].
func (s *Store) Fetches(ctx context.Context, symbol string, from, to time.Time) ([]Fetch, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT fetched_at, outcome, truncated, undated_count, oldest_published_at FROM disclosure_fetches
WHERE symbol = ? AND fetched_at >= ? AND fetched_at <= ? ORDER BY fetched_at LIMIT ?`,
		symbol, sqlutil.FormatTime(from), sqlutil.FormatTime(to), maxFetchRows)
	if err != nil {
		return nil, fmt.Errorf("disclosureskip: list fetches: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []Fetch
	for rows.Next() {
		var (
			f           Fetch
			at, outcome string
			oldest      sql.NullString
		)
		if err := rows.Scan(&at, &outcome, &f.Truncated, &f.Undated, &oldest); err != nil {
			return nil, fmt.Errorf("disclosureskip: scan fetch: %w", err)
		}
		if f.At, err = sqlutil.ParseTime(at); err != nil {
			return nil, err
		}
		f.Symbol, f.OK = symbol, outcome == "ok"
		if oldestAt, err := sqlutil.ParseNullableTime(oldest); err != nil {
			return nil, err
		} else if oldestAt != nil {
			f.Oldest = *oldestAt
		}
		out = append(out, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("disclosureskip: list fetches: %w", err)
	}
	return out, nil
}

// SaveRecord stores the verdict of d and its matches in one transaction. A
// decision that already has a record is left as it is.
func (s *Store) SaveRecord(ctx context.Context, d Decision, res Result, evaluatedAt time.Time) (err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("disclosureskip: begin save record: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	var reason, covered sql.NullString
	if res.Reason != "" {
		reason = sql.NullString{String: string(res.Reason), Valid: true}
	}
	if res.CoveredBy != nil {
		covered = sql.NullString{String: sqlutil.FormatTime(*res.CoveredBy), Valid: true}
	}
	var recordID int64
	err = tx.QueryRowContext(ctx, `
INSERT INTO disclosure_skip_records (jev_decision_id, symbol, decided_at, verdict, unknown_reason, window_from, covered_by_fetch_at, rule_version, evaluated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (jev_decision_id) DO NOTHING
RETURNING id`,
		d.ID, d.Symbol, sqlutil.FormatTime(d.At), string(res.Verdict), reason, sqlutil.FormatTime(res.WindowFrom), covered, RuleVersion,
		sqlutil.FormatTime(evaluatedAt)).Scan(&recordID)
	if errors.Is(err, sql.ErrNoRows) {
		return tx.Commit() // already recorded
	}
	if err != nil {
		return fmt.Errorf("disclosureskip: insert record: %w", err)
	}
	for _, m := range res.Matches {
		var link sql.NullString
		if m.URL != "" {
			link = sql.NullString{String: m.URL, Valid: true}
		}
		if _, err = tx.ExecContext(ctx, `
INSERT INTO disclosure_skip_matches (record_id, published_at, title, kind, document_url) VALUES (?, ?, ?, ?, ?)`,
			recordID, sqlutil.FormatTime(m.PublishedAt), m.Title, string(m.Kind), link); err != nil {
			return fmt.Errorf("disclosureskip: insert match: %w", err)
		}
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("disclosureskip: commit save record: %w", err)
	}
	return nil
}

// PruneFetches deletes the recorded fetches that started before cutoff: a
// decision is evaluated within hours, so older fetches are no longer read.
func (s *Store) PruneFetches(ctx context.Context, cutoff time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM disclosure_fetches WHERE fetched_at < ?`, sqlutil.FormatTime(cutoff))
	if err != nil {
		return 0, fmt.Errorf("disclosureskip: prune fetches: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("disclosureskip: prune fetches: %w", err)
	}
	return n, nil
}
