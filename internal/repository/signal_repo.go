package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// ErrSignalNotFound is returned by SignalRepository methods when no
// matching trade_signals row exists.
var ErrSignalNotFound = errors.New("repository: trade signal not found")

// SignalRepository persists trade_signals rows: every Policy Engine
// LONG/SHORT/NONE decision, not only the ones that pass
// (docs/architecture/er.md §trade_signals, functional.md FR-POLICY-5).
type SignalRepository struct {
	db *sql.DB
}

// NewSignalRepository returns a SignalRepository backed by db.
func NewSignalRepository(db *sql.DB) *SignalRepository {
	return &SignalRepository{db: db}
}

const insertSignalSQL = `
INSERT INTO trade_signals (
	instrument_id, jev_decision_id, symbol, timestamp, direction, score,
	entry_price_reference, policy_version, risk_passed, reject_reason, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

// Insert writes a single trade_signals row.
func (r *SignalRepository) Insert(ctx context.Context, s domain.TradeSignal) (domain.TradeSignal, error) {
	createdAt := s.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}

	res, err := r.db.ExecContext(ctx, insertSignalSQL,
		s.InstrumentID, nullableInt64(s.JevDecisionID), s.Symbol, formatTime(s.Timestamp), s.Direction,
		nullableFloat64(s.Score), nullableFloat64(s.EntryPriceReference), s.PolicyVersion, s.RiskPassed,
		nullableString(s.RejectReason), formatTime(createdAt),
	)
	if err != nil {
		return domain.TradeSignal{}, fmt.Errorf(
			"repository: insert trade signal for instrument %d (%s): %w", s.InstrumentID, s.Direction, err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return domain.TradeSignal{}, fmt.Errorf("repository: read trade signal id for instrument %d: %w", s.InstrumentID, err)
	}

	s.ID = id
	s.CreatedAt = createdAt
	return s, nil
}

// Get returns the trade_signals row with the given id, or
// ErrSignalNotFound.
func (r *SignalRepository) Get(ctx context.Context, id int64) (domain.TradeSignal, error) {
	row := r.db.QueryRowContext(ctx, signalSelectColumns+` WHERE id = ?`, id)
	return scanSignal(row)
}

// ListByInstrument returns up to limit trade_signals rows for
// instrumentID, most recent first, for the Symbol Detail UI's signal
// history and Backtesting/Calibration's per-instrument analysis.
func (r *SignalRepository) ListByInstrument(ctx context.Context, instrumentID int64, limit int) ([]domain.TradeSignal, error) {
	rows, err := r.db.QueryContext(ctx,
		signalSelectColumns+` WHERE instrument_id = ? ORDER BY timestamp DESC LIMIT ?`, instrumentID, limit)
	if err != nil {
		return nil, fmt.Errorf("repository: list trade signals for instrument %d: %w", instrumentID, err)
	}
	defer func() { _ = rows.Close() }()

	var signals []domain.TradeSignal
	for rows.Next() {
		s, err := scanSignal(rows)
		if err != nil {
			return nil, err
		}
		signals = append(signals, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repository: list trade signals for instrument %d: %w", instrumentID, err)
	}
	return signals, nil
}

// ListRecent returns up to limit trade_signals rows across every
// instrument, most recent first, for `GET /api/v1/signals`
// (docs/api/endpoints.md §5).
func (r *SignalRepository) ListRecent(ctx context.Context, limit int) ([]domain.TradeSignal, error) {
	rows, err := r.db.QueryContext(ctx,
		signalSelectColumns+` ORDER BY timestamp DESC, id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("repository: list recent trade signals: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var signals []domain.TradeSignal
	for rows.Next() {
		s, err := scanSignal(rows)
		if err != nil {
			return nil, err
		}
		signals = append(signals, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repository: list recent trade signals: %w", err)
	}
	return signals, nil
}

// CountDirectional returns how many trade_signals rows carry a LONG or
// SHORT direction (NONE rows, which only log why no trade was
// proposed, are excluded), for `GET /api/v1/performance`'s
// `signal_count`.
func (r *SignalRepository) CountDirectional(ctx context.Context) (int64, error) {
	var n int64
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM trade_signals WHERE direction IN (?, ?)`,
		domain.JevDirectionLong, domain.JevDirectionShort,
	).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("repository: count directional trade signals: %w", err)
	}
	return n, nil
}

const signalSelectColumns = `
SELECT id, instrument_id, jev_decision_id, symbol, timestamp, direction, score,
	entry_price_reference, policy_version, risk_passed, reject_reason, created_at
FROM trade_signals`

func scanSignal(row rowScanner) (domain.TradeSignal, error) {
	var (
		s             domain.TradeSignal
		jevDecisionID *int64
		timestamp     string
		rejectReason  sql.NullString
		createdAt     string
	)

	err := row.Scan(
		&s.ID, &s.InstrumentID, nullInt64(&jevDecisionID), &s.Symbol, &timestamp, &s.Direction,
		nullFloat(&s.Score), nullFloat(&s.EntryPriceReference), &s.PolicyVersion, &s.RiskPassed,
		&rejectReason, &createdAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.TradeSignal{}, ErrSignalNotFound
	}
	if err != nil {
		return domain.TradeSignal{}, fmt.Errorf("repository: scan trade signal: %w", err)
	}

	s.JevDecisionID = jevDecisionID

	if rejectReason.Valid {
		s.RejectReason = &rejectReason.String
	}

	s.Timestamp, err = parseTime(timestamp)
	if err != nil {
		return domain.TradeSignal{}, err
	}
	s.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return domain.TradeSignal{}, err
	}
	return s, nil
}
