package market

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

// watchListRetentionDays is how long decided lists are kept: Save drops the
// lists older than that, relative to the list it stores.
const watchListRetentionDays = 60

// WatchListRepository persists watch_lists and watch_list_entries: the 立花
// 監視リスト decided for each 立会日 (docs/architecture/er/tables-market.md
// §watch_lists).
type WatchListRepository struct {
	db *sql.DB
}

// NewWatchListRepository returns a WatchListRepository backed by db.
func NewWatchListRepository(db *sql.DB) *WatchListRepository {
	return &WatchListRepository{db: db}
}

// Save stores list, replacing the stored list of the same ListDate, and drops
// the lists older than watchListRetentionDays before it.
func (r *WatchListRepository) Save(ctx context.Context, list domain.WatchList) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("repository: begin watch list save: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `INSERT INTO watch_lists (list_date, source, reason, basis_date, decided_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (list_date) DO UPDATE SET source = excluded.source, reason = excluded.reason,
		basis_date = excluded.basis_date, decided_at = excluded.decided_at`,
		list.ListDate, list.Source, list.Reason, list.BasisDate, sqlutil.FormatTime(list.DecidedAt)); err != nil {
		return fmt.Errorf("repository: save watch list %q: %w", list.ListDate, err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM watch_list_entries WHERE list_date = ?`, list.ListDate); err != nil {
		return fmt.Errorf("repository: replace entries of watch list %q: %w", list.ListDate, err)
	}
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO watch_list_entries (list_date, position, symbol, origin, indicators)
		VALUES (?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("repository: prepare watch list entry save: %w", err)
	}
	defer func() { _ = stmt.Close() }()
	for i, e := range list.Entries {
		if _, err := stmt.ExecContext(ctx, list.ListDate, i, e.Symbol, e.Origin, strings.Join(e.Indicators, ",")); err != nil {
			return fmt.Errorf("repository: save entry of watch list %q: %w", list.ListDate, err)
		}
	}
	cutoff := fmt.Sprintf("-%d days", watchListRetentionDays)
	if _, err := tx.ExecContext(ctx, `DELETE FROM watch_list_entries WHERE list_date < date(?, ?)`, list.ListDate, cutoff); err != nil {
		return fmt.Errorf("repository: prune watch list entries: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM watch_lists WHERE list_date < date(?, ?)`, list.ListDate, cutoff); err != nil {
		return fmt.Errorf("repository: prune watch lists: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("repository: commit watch list save: %w", err)
	}
	return nil
}

// Get returns the list of listDate, or (zero, false, nil) when there is none.
func (r *WatchListRepository) Get(ctx context.Context, listDate string) (domain.WatchList, bool, error) {
	return r.one(ctx, `WHERE list_date = ?`, listDate)
}

// AtOrBefore returns the list with the greatest ListDate that is not after
// date, or (zero, false, nil) when there is none.
func (r *WatchListRepository) AtOrBefore(ctx context.Context, date string) (domain.WatchList, bool, error) {
	return r.one(ctx, `WHERE list_date <= ? ORDER BY list_date DESC LIMIT 1`, date)
}

// Latest returns the list with the greatest ListDate, or (zero, false, nil).
func (r *WatchListRepository) Latest(ctx context.Context) (domain.WatchList, bool, error) {
	return r.one(ctx, `ORDER BY list_date DESC LIMIT 1`)
}

// Recent returns up to limit lists, newest ListDate first.
func (r *WatchListRepository) Recent(ctx context.Context, limit int) ([]domain.WatchList, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT list_date, source, reason, basis_date, decided_at
		FROM watch_lists ORDER BY list_date DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("repository: list watch lists: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []domain.WatchList
	for rows.Next() {
		l, err := scanWatchList(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repository: list watch lists: %w", err)
	}
	_ = rows.Close() // the entries are read on the same connection pool
	for i := range out {
		if out[i].Entries, err = r.entries(ctx, out[i].ListDate); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (r *WatchListRepository) one(ctx context.Context, tail string, args ...any) (domain.WatchList, bool, error) {
	l, err := scanWatchList(r.db.QueryRowContext(ctx,
		`SELECT list_date, source, reason, basis_date, decided_at FROM watch_lists `+tail, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.WatchList{}, false, nil
	}
	if err != nil {
		return domain.WatchList{}, false, err
	}
	if l.Entries, err = r.entries(ctx, l.ListDate); err != nil {
		return domain.WatchList{}, false, err
	}
	return l, true, nil
}

func scanWatchList(row interface{ Scan(...any) error }) (domain.WatchList, error) {
	var (
		l       domain.WatchList
		decided string
	)
	if err := row.Scan(&l.ListDate, &l.Source, &l.Reason, &l.BasisDate, &decided); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.WatchList{}, err
		}
		return domain.WatchList{}, fmt.Errorf("repository: scan watch list: %w", err)
	}
	var err error
	if l.DecidedAt, err = sqlutil.ParseTime(decided); err != nil {
		return domain.WatchList{}, err
	}
	return l, nil
}

func (r *WatchListRepository) entries(ctx context.Context, listDate string) ([]domain.WatchListEntry, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT symbol, origin, indicators FROM watch_list_entries
		WHERE list_date = ? ORDER BY position`, listDate)
	if err != nil {
		return nil, fmt.Errorf("repository: list entries of watch list %q: %w", listDate, err)
	}
	defer func() { _ = rows.Close() }()
	var out []domain.WatchListEntry
	for rows.Next() {
		var (
			e          domain.WatchListEntry
			indicators string
		)
		if err := rows.Scan(&e.Symbol, &e.Origin, &indicators); err != nil {
			return nil, fmt.Errorf("repository: scan entry of watch list %q: %w", listDate, err)
		}
		if indicators != "" {
			e.Indicators = strings.Split(indicators, ",")
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repository: list entries of watch list %q: %w", listDate, err)
	}
	return out, nil
}
