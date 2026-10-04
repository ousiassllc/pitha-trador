package judgement

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

// ErrDecisionNotFound is returned by DecisionRepository methods when no
// matching jev_decisions row exists.
var ErrDecisionNotFound = errors.New("repository: jev decision not found")

// DecisionRepository persists jev_decisions rows: every Jev Scout/Trader
// call's input, raw output, and calibration metadata
// (docs/architecture/er.md §jev_decisions).
type DecisionRepository struct {
	db       *sql.DB
	observer DecisionObserver
}

// DecisionObserver is notified after every committed Insert with the
// stored row (docs/architecture/overview.md §12). It runs synchronously
// on the writer's goroutine after the write has committed and cannot fail
// the write, so it must return quickly.
type DecisionObserver func(ctx context.Context, d domain.JevDecision)

// SetObserver registers fn to be called after every committed Insert,
// replacing any previous observer. It is not safe to call concurrently
// with other DecisionRepository methods; wire it once during
// composition.
func (r *DecisionRepository) SetObserver(fn DecisionObserver) { r.observer = fn }

// NewDecisionRepository returns a DecisionRepository backed by db.
func NewDecisionRepository(db *sql.DB) *DecisionRepository {
	return &DecisionRepository{db: db}
}

const insertDecisionSQL = `
INSERT INTO jev_decisions (
	instrument_id, symbol, timestamp, decision_type, state_hash, state_json,
	question_version, response_json, direction, confidence, latency_ms,
	model_id, request_cost, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

// Insert writes a single jev_decisions row.
func (r *DecisionRepository) Insert(ctx context.Context, d domain.JevDecision) (domain.JevDecision, error) {
	createdAt := d.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}

	res, err := r.db.ExecContext(ctx, insertDecisionSQL,
		d.InstrumentID, d.Symbol, sqlutil.FormatTime(d.Timestamp), d.DecisionType, d.StateHash, d.StateJSON,
		d.QuestionVersion, d.ResponseJSON, sqlutil.NullableString(d.Direction), sqlutil.NullableFloat64(d.Confidence),
		d.LatencyMs, d.ModelID, sqlutil.NullableFloat64(d.RequestCost), sqlutil.FormatTime(createdAt),
	)
	if err != nil {
		return domain.JevDecision{}, fmt.Errorf(
			"repository: insert jev decision for instrument %d (%s): %w", d.InstrumentID, d.DecisionType, err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return domain.JevDecision{}, fmt.Errorf("repository: read jev decision id for instrument %d: %w", d.InstrumentID, err)
	}

	d.ID = id
	d.CreatedAt = createdAt
	if r.observer != nil {
		r.observer(ctx, d)
	}
	return d, nil
}

// Get returns the jev_decisions row with the given id, or
// ErrDecisionNotFound.
func (r *DecisionRepository) Get(ctx context.Context, id int64) (domain.JevDecision, error) {
	row := r.db.QueryRowContext(ctx, decisionSelectColumns+` FROM jev_decisions WHERE id = ?`, id)
	return scanDecision(row)
}

// ListByInstrument returns up to limit jev_decisions rows for
// instrumentID, most recent first, for the Symbol Detail UI's decision
// history and Calibration's per-instrument analysis.
func (r *DecisionRepository) ListByInstrument(ctx context.Context, instrumentID int64, limit int) ([]domain.JevDecision, error) {
	rows, err := r.db.QueryContext(ctx,
		decisionSelectColumns+` FROM jev_decisions WHERE instrument_id = ? ORDER BY timestamp DESC LIMIT ?`,
		instrumentID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("repository: list jev decisions for instrument %d: %w", instrumentID, err)
	}
	defer func() { _ = rows.Close() }()

	var out []domain.JevDecision
	for rows.Next() {
		d, err := scanDecision(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repository: list jev decisions for instrument %d: %w", instrumentID, err)
	}
	return out, nil
}

// listRecentAllQuery / listRecentByTypeQuery read the newest jev_decisions
// rows straight off jev_decisions_timestamp_idx / jev_decisions_type_timestamp_idx
// (backward scans stop after LIMIT rows). They are two statements rather than
// one optional-type filter (an OR on the bound parameter) because that cannot
// use an index and forces a full scan plus sort of the table (issue #419).
const (
	listRecentAllQuery    = decisionSelectColumns + ` FROM jev_decisions ORDER BY timestamp DESC, id DESC LIMIT ?`
	listRecentByTypeQuery = decisionSelectColumns + ` FROM jev_decisions WHERE decision_type = ? ORDER BY timestamp DESC, id DESC LIMIT ?`
)

// ListRecent returns up to limit jev_decisions rows across every
// instrument, most recent first, optionally restricted to decisionType
// ("" = both Scout and Trader) - System Activity Log's Jev call feed.
func (r *DecisionRepository) ListRecent(ctx context.Context, decisionType string, limit int) ([]domain.JevDecision, error) {
	query, args := listRecentAllQuery, []any{limit}
	if decisionType != "" {
		query, args = listRecentByTypeQuery, []any{decisionType, limit}
	}
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("repository: list recent jev decisions (type=%q): %w", decisionType, err)
	}
	defer func() { _ = rows.Close() }()

	var out []domain.JevDecision
	for rows.Next() {
		d, err := scanDecision(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repository: list recent jev decisions (type=%q): %w", decisionType, err)
	}
	return out, nil
}

// ListByInstrumentRange returns every decisionType jev_decisions row for
// instrumentID timestamped in the half-open range [from, to), oldest
// first - a backtest replay's historical Jev decision feed.
func (r *DecisionRepository) ListByInstrumentRange(ctx context.Context, instrumentID int64, decisionType string, from, to time.Time) ([]domain.JevDecision, error) {
	rows, err := r.db.QueryContext(ctx,
		decisionSelectColumns+` FROM jev_decisions WHERE instrument_id = ? AND decision_type = ? AND timestamp >= ? AND timestamp < ? ORDER BY timestamp ASC`,
		instrumentID, decisionType, sqlutil.FormatTime(from), sqlutil.FormatTime(to),
	)
	if err != nil {
		return nil, fmt.Errorf("repository: list %s jev decisions for instrument %d in [%s, %s): %w", decisionType, instrumentID, from, to, err)
	}
	defer func() { _ = rows.Close() }()

	var out []domain.JevDecision
	for rows.Next() {
		d, err := scanDecision(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repository: list %s jev decisions for instrument %d in [%s, %s): %w", decisionType, instrumentID, from, to, err)
	}
	return out, nil
}

const decisionSelectColumns = `
SELECT id, instrument_id, symbol, timestamp, decision_type, state_hash, state_json,
	question_version, response_json, direction, confidence, latency_ms,
	model_id, request_cost, created_at`

func scanDecision(row sqlutil.RowScanner) (domain.JevDecision, error) {
	var (
		d         domain.JevDecision
		timestamp string
		direction sql.NullString
		createdAt string
	)

	err := row.Scan(
		&d.ID, &d.InstrumentID, &d.Symbol, &timestamp, &d.DecisionType, &d.StateHash, &d.StateJSON,
		&d.QuestionVersion, &d.ResponseJSON, &direction, sqlutil.NullFloat(&d.Confidence), &d.LatencyMs,
		&d.ModelID, sqlutil.NullFloat(&d.RequestCost), &createdAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.JevDecision{}, ErrDecisionNotFound
	}
	if err != nil {
		return domain.JevDecision{}, fmt.Errorf("repository: scan jev decision: %w", err)
	}

	if direction.Valid {
		d.Direction = &direction.String
	}

	d.Timestamp, err = sqlutil.ParseTime(timestamp)
	if err != nil {
		return domain.JevDecision{}, err
	}
	d.CreatedAt, err = sqlutil.ParseTime(createdAt)
	if err != nil {
		return domain.JevDecision{}, err
	}
	return d, nil
}
