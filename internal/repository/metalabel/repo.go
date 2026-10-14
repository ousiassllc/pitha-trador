package metalabel

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

// Repository persists metalabel_models and metalabel_predictions
// (docs/architecture/er/tables-metalabel.md).
type Repository struct {
	db *sql.DB
}

// NewRepository returns a Repository backed by db.
func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

const modelColumns = `id, version, algorithm, label_version, status, reject_reason, trained_at,
	train_start, train_end, oos_start, oos_end, train_samples, oos_samples,
	feature_names_json, params_json, oos_auc, oos_expected_value_bps, oos_calibration_json, created_at`

// InsertModel stores m and returns it with ID and CreatedAt set. A Version
// that is already stored is an error (the column is UNIQUE).
func (r *Repository) InsertModel(ctx context.Context, m Model) (Model, error) {
	names, err := json.Marshal(nonNil(m.FeatureNames))
	if err != nil {
		return Model{}, fmt.Errorf("repository: encode feature names of meta-label model %q: %w", m.Version, err)
	}
	curve, err := json.Marshal(nonNil(m.OOSCalibration))
	if err != nil {
		return Model{}, fmt.Errorf("repository: encode calibration curve of meta-label model %q: %w", m.Version, err)
	}
	createdAt := time.Now().UTC()
	res, err := r.db.ExecContext(ctx, `INSERT INTO metalabel_models (
		version, algorithm, label_version, status, reject_reason, trained_at,
		train_start, train_end, oos_start, oos_end, train_samples, oos_samples,
		feature_names_json, params_json, oos_auc, oos_expected_value_bps, oos_calibration_json, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		m.Version, m.Algorithm, m.LabelVersion, m.Status, m.RejectReason, sqlutil.FormatTime(m.TrainedAt),
		sqlutil.FormatTime(m.TrainStart), sqlutil.FormatTime(m.TrainEnd), sqlutil.FormatTime(m.OOSStart), sqlutil.FormatTime(m.OOSEnd),
		m.TrainSamples, m.OOSSamples, string(names), m.ParamsJSON,
		sqlutil.NullableFloat64(m.OOSAUC), sqlutil.NullableFloat64(m.OOSExpectedValueBps), string(curve), sqlutil.FormatTime(createdAt))
	if err != nil {
		return Model{}, fmt.Errorf("repository: insert meta-label model %q: %w", m.Version, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Model{}, fmt.Errorf("repository: meta-label model %q id: %w", m.Version, err)
	}
	m.ID = id
	m.CreatedAt = createdAt
	return m, nil
}

// LatestActiveModel returns the active model with the greatest TrainedAt (the
// newest ID breaking a tie), or (zero, false, nil) when there is none.
func (r *Repository) LatestActiveModel(ctx context.Context) (Model, bool, error) {
	m, err := scanModel(r.db.QueryRowContext(ctx, `SELECT `+modelColumns+` FROM metalabel_models
		WHERE status = ? ORDER BY trained_at DESC, id DESC LIMIT 1`, ModelActive))
	if errors.Is(err, sql.ErrNoRows) {
		return Model{}, false, nil
	}
	if err != nil {
		return Model{}, false, err
	}
	return m, true, nil
}

func scanModel(row sqlutil.RowScanner) (Model, error) {
	var (
		m                                                 Model
		trainedAt, trainStart, trainEnd, oosStart, oosEnd string
		names, curve, createdAt                           string
		auc, ev                                           sql.NullFloat64
	)
	if err := row.Scan(&m.ID, &m.Version, &m.Algorithm, &m.LabelVersion, &m.Status, &m.RejectReason, &trainedAt,
		&trainStart, &trainEnd, &oosStart, &oosEnd, &m.TrainSamples, &m.OOSSamples,
		&names, &m.ParamsJSON, &auc, &ev, &curve, &createdAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Model{}, err
		}
		return Model{}, fmt.Errorf("repository: scan meta-label model: %w", err)
	}
	for _, f := range []struct {
		dst *time.Time
		src string
	}{{&m.TrainedAt, trainedAt}, {&m.TrainStart, trainStart}, {&m.TrainEnd, trainEnd},
		{&m.OOSStart, oosStart}, {&m.OOSEnd, oosEnd}, {&m.CreatedAt, createdAt}} {
		t, err := sqlutil.ParseTime(f.src)
		if err != nil {
			return Model{}, err
		}
		*f.dst = t
	}
	if err := json.Unmarshal([]byte(names), &m.FeatureNames); err != nil {
		return Model{}, fmt.Errorf("repository: decode feature names of meta-label model %q: %w", m.Version, err)
	}
	if err := json.Unmarshal([]byte(curve), &m.OOSCalibration); err != nil {
		return Model{}, fmt.Errorf("repository: decode calibration curve of meta-label model %q: %w", m.Version, err)
	}
	if auc.Valid {
		m.OOSAUC = &auc.Float64
	}
	if ev.Valid {
		m.OOSExpectedValueBps = &ev.Float64
	}
	return m, nil
}

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}
