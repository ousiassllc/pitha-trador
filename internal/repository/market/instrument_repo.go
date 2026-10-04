package market

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

// ErrInstrumentNotFound is returned by InstrumentRepository methods when no
// matching instruments row exists.
var ErrInstrumentNotFound = errors.New("repository: instrument not found")

// InstrumentRepository provides CRUD access to the instruments table
// (docs/architecture/er.md §instruments), the target-universe master
// record every other table references by ID.
type InstrumentRepository struct {
	db *sql.DB
}

// NewInstrumentRepository returns an InstrumentRepository backed by db.
func NewInstrumentRepository(db *sql.DB) *InstrumentRepository {
	return &InstrumentRepository{db: db}
}

// Create inserts a new instrument row and returns it with its assigned ID
// and created_at/updated_at populated.
func (r *InstrumentRepository) Create(ctx context.Context, in domain.Instrument) (domain.Instrument, error) {
	now := time.Now().UTC()
	if in.Kind == "" {
		in.Kind = domain.InstrumentKindStock
	}
	res, err := r.db.ExecContext(ctx,
		`INSERT INTO instruments (symbol, name, market, sector, kind, is_active, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		in.Symbol, in.Name, in.Market, sqlutil.NullableString(in.Sector), in.Kind, in.IsActive, sqlutil.FormatTime(now), sqlutil.FormatTime(now),
	)
	if err != nil {
		return domain.Instrument{}, fmt.Errorf("repository: create instrument %q: %w", in.Symbol, err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return domain.Instrument{}, fmt.Errorf("repository: read instrument id for %q: %w", in.Symbol, err)
	}

	in.ID = id
	in.CreatedAt = now
	in.UpdatedAt = now
	return in, nil
}

// Upsert syncs the instrument master from ins in one transaction, keyed by
// symbol: a new symbol is inserted as active, an existing one has its
// name/market/sector/kind refreshed. is_active is never touched on an
// existing row, so an operator's exclusion survives a re-sync, and a row
// whose master fields already match is left alone (updated_at unchanged),
// making a re-run with the same input a no-op. It returns how many rows
// were inserted or changed. An empty Kind is stored as
// domain.InstrumentKindStock.
func (r *InstrumentRepository) Upsert(ctx context.Context, ins []domain.Instrument) (int, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("repository: begin instrument upsert: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.PrepareContext(ctx,
		`INSERT INTO instruments (symbol, name, market, sector, kind, is_active, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, 1, ?, ?)
		 ON CONFLICT (symbol) DO UPDATE SET
		   name = excluded.name, market = excluded.market, sector = excluded.sector,
		   kind = excluded.kind, updated_at = excluded.updated_at
		 WHERE name IS NOT excluded.name OR market IS NOT excluded.market
		   OR sector IS NOT excluded.sector OR kind IS NOT excluded.kind`)
	if err != nil {
		return 0, fmt.Errorf("repository: prepare instrument upsert: %w", err)
	}
	defer func() { _ = stmt.Close() }()

	now := sqlutil.FormatTime(time.Now().UTC())
	changed := 0
	for _, in := range ins {
		if in.Kind == "" {
			in.Kind = domain.InstrumentKindStock
		}
		res, err := stmt.ExecContext(ctx, in.Symbol, in.Name, in.Market, sqlutil.NullableString(in.Sector), in.Kind, now, now)
		if err != nil {
			return 0, fmt.Errorf("repository: upsert instrument %q: %w", in.Symbol, err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return 0, fmt.Errorf("repository: upsert instrument %q: %w", in.Symbol, err)
		}
		changed += int(n)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("repository: commit instrument upsert: %w", err)
	}
	return changed, nil
}

// Get returns the instrument with the given id, or ErrInstrumentNotFound.
func (r *InstrumentRepository) Get(ctx context.Context, id int64) (domain.Instrument, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, symbol, name, market, sector, kind, is_active, created_at, updated_at
		 FROM instruments WHERE id = ?`, id)
	return scanInstrument(row)
}

// GetBySymbol returns the instrument with the given symbol, or
// ErrInstrumentNotFound.
func (r *InstrumentRepository) GetBySymbol(ctx context.Context, symbol string) (domain.Instrument, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, symbol, name, market, sector, kind, is_active, created_at, updated_at
		 FROM instruments WHERE symbol = ?`, symbol)
	return scanInstrument(row)
}

// ListActive returns every instrument with is_active = true, ordered by
// symbol, for the Fast Screener's scan universe.
func (r *InstrumentRepository) ListActive(ctx context.Context) ([]domain.Instrument, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, symbol, name, market, sector, kind, is_active, created_at, updated_at
		 FROM instruments WHERE is_active = 1 ORDER BY symbol`)
	if err != nil {
		return nil, fmt.Errorf("repository: list active instruments: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []domain.Instrument
	for rows.Next() {
		inst, err := scanInstrument(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, inst)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repository: list active instruments: %w", err)
	}
	return out, nil
}

// ListActiveByKind returns every active instrument of the given
// domain.InstrumentKind*, ordered by symbol. Feature Engine reads the
// market_index / sector_index instruments through it to derive market
// context (functional.md §4.1).
func (r *InstrumentRepository) ListActiveByKind(ctx context.Context, kind string) ([]domain.Instrument, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, symbol, name, market, sector, kind, is_active, created_at, updated_at
		 FROM instruments WHERE is_active = 1 AND kind = ? ORDER BY symbol`, kind)
	if err != nil {
		return nil, fmt.Errorf("repository: list active %s instruments: %w", kind, err)
	}
	defer func() { _ = rows.Close() }()

	var out []domain.Instrument
	for rows.Next() {
		inst, err := scanInstrument(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, inst)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repository: list active %s instruments: %w", kind, err)
	}
	return out, nil
}

// LatestStockReturns5m returns the return_5m of every active stock
// instrument's most recent market_snapshots bar timestamped in
// [since, until], skipping bars whose return_5m is NULL. It feeds
// market_breadth (functional.md §4.1): index instruments are excluded so
// only the tradable universe counts, and one bar per instrument so a
// symbol scanned more often is not over-weighted. until keeps a caller
// computing a bar at time T from reading bars written after T (FR-FE-1).
//
// The outer s repeats the [since, until] bound so each instrument's lookup
// is an index range search over just that window; without it the planner
// walks every retained bar of every active instrument (90 days of
// market_snapshots) on each market-data job, which made a 4,000-symbol
// cycle quadratic (non-functional.md §2.3, issue #391).
func (r *InstrumentRepository) LatestStockReturns5m(ctx context.Context, since, until time.Time) ([]float64, error) {
	from, to := sqlutil.FormatTime(since), sqlutil.FormatTime(until)
	rows, err := r.db.QueryContext(ctx, `
SELECT s.return_5m
FROM market_snapshots s
JOIN instruments i ON i.id = s.instrument_id
WHERE i.is_active = 1 AND i.kind = ? AND s.return_5m IS NOT NULL
  AND s.timestamp >= ? AND s.timestamp <= ?
  AND s.timestamp = (
	SELECT MAX(m.timestamp) FROM market_snapshots m
	WHERE m.instrument_id = s.instrument_id AND m.timestamp >= ? AND m.timestamp <= ?)`,
		domain.InstrumentKindStock, from, to, from, to)
	if err != nil {
		return nil, fmt.Errorf("repository: list latest stock returns: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []float64
	for rows.Next() {
		var v float64
		if err := rows.Scan(&v); err != nil {
			return nil, fmt.Errorf("repository: scan latest stock return: %w", err)
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repository: list latest stock returns: %w", err)
	}
	return out, nil
}

// Update overwrites the mutable fields (name, market, sector, kind, is_active) of
// the instrument identified by in.ID, refreshing updated_at; an empty Kind
// keeps the stored kind. It returns
// ErrInstrumentNotFound if no row with that ID exists.
func (r *InstrumentRepository) Update(ctx context.Context, in domain.Instrument) (domain.Instrument, error) {
	now := time.Now().UTC()
	res, err := r.db.ExecContext(ctx,
		`UPDATE instruments SET name = ?, market = ?, sector = ?, kind = COALESCE(NULLIF(?, ''), kind), is_active = ?, updated_at = ?
		 WHERE id = ?`,
		in.Name, in.Market, sqlutil.NullableString(in.Sector), in.Kind, in.IsActive, sqlutil.FormatTime(now), in.ID,
	)
	if err != nil {
		return domain.Instrument{}, fmt.Errorf("repository: update instrument %d: %w", in.ID, err)
	}

	n, err := res.RowsAffected()
	if err != nil {
		return domain.Instrument{}, fmt.Errorf("repository: update instrument %d: %w", in.ID, err)
	}
	if n == 0 {
		return domain.Instrument{}, ErrInstrumentNotFound
	}

	in.UpdatedAt = now
	if in.Kind == "" {
		stored, err := r.Get(ctx, in.ID)
		if err != nil {
			return domain.Instrument{}, err
		}
		in.Kind = stored.Kind
	}
	return in, nil
}

func scanInstrument(row sqlutil.RowScanner) (domain.Instrument, error) {
	var (
		inst      domain.Instrument
		sector    sql.NullString
		createdAt string
		updatedAt string
	)

	err := row.Scan(&inst.ID, &inst.Symbol, &inst.Name, &inst.Market, &sector, &inst.Kind, &inst.IsActive, &createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Instrument{}, ErrInstrumentNotFound
	}
	if err != nil {
		return domain.Instrument{}, fmt.Errorf("repository: scan instrument: %w", err)
	}

	if sector.Valid {
		inst.Sector = &sector.String
	}

	inst.CreatedAt, err = sqlutil.ParseTime(createdAt)
	if err != nil {
		return domain.Instrument{}, err
	}
	inst.UpdatedAt, err = sqlutil.ParseTime(updatedAt)
	if err != nil {
		return domain.Instrument{}, err
	}
	return inst, nil
}
