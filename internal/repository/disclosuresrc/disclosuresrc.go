// Package disclosuresrc persists the extra disclosure / news sources of issue
// #1096: research_edinet_documents and research_edinet_fetch_days (the EDINET
// document list, metadata only), research_tachibana_news (the 立花 e支店 news)
// and research_disclosure_source_status (per source: requests, failures, last
// fetch), migration 000063.
//
// The record is for research only. Policy, Risk, order placement, Jev, the
// gates and the existing disclosure skip rules never read it; it writes only
// its own four tables (never disclosure_items, jev_decisions, market_snapshots
// or daily_bars). It MUST depend only on internal/domain and
// internal/repository/sqlutil. See docs/architecture/overview/integrations-disclosure-sources.md.
package disclosuresrc

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain/disclosuresrc"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

// jst is the zone of the days the record is keyed by. Japan has no DST.
var jst = time.FixedZone("JST", 9*60*60)

// Day is the JST calendar day (YYYY-MM-DD) of t.
func Day(t time.Time) string { return t.In(jst).Format(time.DateOnly) }

// Repository reads and writes the disclosure source tables.
type Repository struct {
	db *sql.DB
}

// NewRepository returns a Repository backed by db.
func NewRepository(db *sql.DB) *Repository { return &Repository{db: db} }

// UpsertEdinetDocuments stores docs in one transaction. A document already
// stored (same doc_id) is updated in place, first_seen_at kept, so a re-fetch
// changes no row count. It returns how many docs were written.
func (r *Repository) UpsertEdinetDocuments(ctx context.Context, docs []disclosuresrc.EdinetDocument, now time.Time) (n int, err error) {
	if len(docs) == 0 {
		return 0, nil
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("disclosuresrc: begin upsert edinet documents: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	stored := sqlutil.FormatTime(now)
	for _, d := range docs {
		if _, err = tx.ExecContext(ctx, `
INSERT INTO research_edinet_documents (doc_id, symbol, sec_code, edinet_code, filer_name, doc_type_code, title, submitted_at, submit_day, first_seen_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (doc_id) DO UPDATE SET
	symbol = excluded.symbol, sec_code = excluded.sec_code, edinet_code = excluded.edinet_code, filer_name = excluded.filer_name,
	doc_type_code = excluded.doc_type_code, title = excluded.title, submitted_at = excluded.submitted_at,
	submit_day = excluded.submit_day, updated_at = excluded.updated_at`,
			d.DocID, d.Symbol, d.SecCode, d.EdinetCode, d.FilerName, d.DocTypeCode, d.Title,
			sqlutil.FormatTime(d.SubmittedAt), Day(d.SubmittedAt), stored, stored); err != nil {
			return 0, fmt.Errorf("disclosuresrc: upsert edinet document: %w", err)
		}
	}
	if err = tx.Commit(); err != nil {
		return 0, fmt.Errorf("disclosuresrc: commit upsert edinet documents: %w", err)
	}
	return len(docs), nil
}

// MarkEdinetDay records that the document list of d.Day was fetched.
func (r *Repository) MarkEdinetDay(ctx context.Context, d disclosuresrc.EdinetDay) error {
	_, err := r.db.ExecContext(ctx, `
INSERT INTO research_edinet_fetch_days (day, fetched_at, total_count, recorded_count, skipped_count) VALUES (?, ?, ?, ?, ?)
ON CONFLICT (day) DO UPDATE SET fetched_at = excluded.fetched_at, total_count = excluded.total_count,
	recorded_count = excluded.recorded_count, skipped_count = excluded.skipped_count`,
		d.Day, sqlutil.FormatTime(d.FetchedAt), d.Total, d.Recorded, d.Skipped)
	if err != nil {
		return fmt.Errorf("disclosuresrc: mark edinet day %s: %w", d.Day, err)
	}
	return nil
}

// EdinetDayFetchedAt returns when the document list of day was last fetched, or
// ok=false when it never was.
func (r *Repository) EdinetDayFetchedAt(ctx context.Context, day string) (at time.Time, ok bool, err error) {
	var s string
	err = r.db.QueryRowContext(ctx, `SELECT fetched_at FROM research_edinet_fetch_days WHERE day = ?`, day).Scan(&s)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, fmt.Errorf("disclosuresrc: read edinet day %s: %w", day, err)
	}
	if at, err = sqlutil.ParseTime(s); err != nil {
		return time.Time{}, false, err
	}
	return at, true, nil
}

// UpsertTachibanaNews stores news rows in one transaction, idempotent per
// (news_id, symbol): a news item seen again updates its headline and time and
// keeps first_seen_at. It returns how many rows were written.
func (r *Repository) UpsertTachibanaNews(ctx context.Context, news []disclosuresrc.TachibanaNews, now time.Time) (n int, err error) {
	if len(news) == 0 {
		return 0, nil
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("disclosuresrc: begin upsert tachibana news: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	stored := sqlutil.FormatTime(now)
	for _, item := range news {
		if _, err = tx.ExecContext(ctx, `
INSERT INTO research_tachibana_news (news_id, symbol, headline, published_at, first_seen_at) VALUES (?, ?, ?, ?, ?)
ON CONFLICT (news_id, symbol) DO UPDATE SET headline = excluded.headline, published_at = excluded.published_at`,
			item.NewsID, item.Symbol, item.Headline, sqlutil.FormatTime(item.PublishedAt), stored); err != nil {
			return 0, fmt.Errorf("disclosuresrc: upsert tachibana news: %w", err)
		}
	}
	if err = tx.Commit(); err != nil {
		return 0, fmt.Errorf("disclosuresrc: commit upsert tachibana news: %w", err)
	}
	return len(news), nil
}

// Purge deletes the recorded rows older than cutoff: documents by their
// submission time, news by their time, fetch days by day. It returns how many
// rows it deleted. The status rows are never purged.
func (r *Repository) Purge(ctx context.Context, cutoff time.Time) (int64, error) {
	var total int64
	for _, q := range []struct {
		sql string
		arg string
	}{
		{`DELETE FROM research_edinet_documents WHERE submitted_at < ?`, sqlutil.FormatTime(cutoff)},
		{`DELETE FROM research_tachibana_news WHERE published_at < ?`, sqlutil.FormatTime(cutoff)},
		{`DELETE FROM research_edinet_fetch_days WHERE day < ?`, Day(cutoff)},
	} {
		res, err := r.db.ExecContext(ctx, q.sql, q.arg)
		if err != nil {
			return total, fmt.Errorf("disclosuresrc: purge: %w", err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return total, fmt.Errorf("disclosuresrc: purge: rows affected: %w", err)
		}
		total += n
	}
	return total, nil
}
