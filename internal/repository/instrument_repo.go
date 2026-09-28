package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
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
	res, err := r.db.ExecContext(ctx,
		`INSERT INTO instruments (symbol, name, market, sector, is_active, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		in.Symbol, in.Name, in.Market, nullableString(in.Sector), in.IsActive, formatTime(now), formatTime(now),
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

// Get returns the instrument with the given id, or ErrInstrumentNotFound.
func (r *InstrumentRepository) Get(ctx context.Context, id int64) (domain.Instrument, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, symbol, name, market, sector, is_active, created_at, updated_at
		 FROM instruments WHERE id = ?`, id)
	return scanInstrument(row)
}

// GetBySymbol returns the instrument with the given symbol, or
// ErrInstrumentNotFound.
func (r *InstrumentRepository) GetBySymbol(ctx context.Context, symbol string) (domain.Instrument, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, symbol, name, market, sector, is_active, created_at, updated_at
		 FROM instruments WHERE symbol = ?`, symbol)
	return scanInstrument(row)
}

// ListActive returns every instrument with is_active = true, ordered by
// symbol, for the Fast Screener's scan universe.
func (r *InstrumentRepository) ListActive(ctx context.Context) ([]domain.Instrument, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, symbol, name, market, sector, is_active, created_at, updated_at
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

// Update overwrites the mutable fields (name, market, sector, is_active) of
// the instrument identified by in.ID, refreshing updated_at. It returns
// ErrInstrumentNotFound if no row with that ID exists.
func (r *InstrumentRepository) Update(ctx context.Context, in domain.Instrument) (domain.Instrument, error) {
	now := time.Now().UTC()
	res, err := r.db.ExecContext(ctx,
		`UPDATE instruments SET name = ?, market = ?, sector = ?, is_active = ?, updated_at = ?
		 WHERE id = ?`,
		in.Name, in.Market, nullableString(in.Sector), in.IsActive, formatTime(now), in.ID,
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
	return in, nil
}

// rowScanner is satisfied by both *sql.Row and *sql.Rows, letting scan
// helpers work for single-row and multi-row queries alike.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanInstrument(row rowScanner) (domain.Instrument, error) {
	var (
		inst      domain.Instrument
		sector    sql.NullString
		createdAt string
		updatedAt string
	)

	err := row.Scan(&inst.ID, &inst.Symbol, &inst.Name, &inst.Market, &sector, &inst.IsActive, &createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Instrument{}, ErrInstrumentNotFound
	}
	if err != nil {
		return domain.Instrument{}, fmt.Errorf("repository: scan instrument: %w", err)
	}

	if sector.Valid {
		inst.Sector = &sector.String
	}

	inst.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return domain.Instrument{}, err
	}
	inst.UpdatedAt, err = parseTime(updatedAt)
	if err != nil {
		return domain.Instrument{}, err
	}
	return inst, nil
}
