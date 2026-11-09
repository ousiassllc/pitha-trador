package loader

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	repo "github.com/ousiassllc/pitha-trador/internal/repository/metalabel"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
	"github.com/ousiassllc/pitha-trador/internal/service/metalabel"
)

// modelSelect lists the stored columns the comparison needs; the model's
// parameters are read as one opaque string (params_json) that is hashed and
// parsed, never printed.
const modelSelect = `SELECT id, version, algorithm, status, trained_at, train_start, train_end, oos_start, oos_end,
	feature_names_json, params_json, adoption_json FROM metalabel_models`

// LatestRejected returns the rejected model with the greatest trained_at (the
// newest id breaking a tie), or false when none is stored. It is what `init`
// pins; `run` never calls it.
func (s *Source) LatestRejected(ctx context.Context) (repo.Model, bool, error) {
	return s.queryModel(ctx, modelSelect+` WHERE status = ? ORDER BY trained_at DESC, id DESC LIMIT 1`, repo.ModelRejected)
}

// ModelByVersion returns the stored model with that version, or false when
// there is none: `run` reads the model the manifest froze, by version.
func (s *Source) ModelByVersion(ctx context.Context, version string) (repo.Model, bool, error) {
	return s.queryModel(ctx, modelSelect+` WHERE version = ?`, version)
}

func (s *Source) queryModel(ctx context.Context, query string, args ...any) (repo.Model, bool, error) {
	var (
		m                                                 repo.Model
		trainedAt, trainStart, trainEnd, oosStart, oosEnd string
		names, adoption                                   string
	)
	err := s.db.QueryRowContext(ctx, query, args...).Scan(&m.ID, &m.Version, &m.Algorithm, &m.Status,
		&trainedAt, &trainStart, &trainEnd, &oosStart, &oosEnd, &names, &m.ParamsJSON, &adoption)
	if errors.Is(err, sql.ErrNoRows) {
		return repo.Model{}, false, nil
	}
	if err != nil {
		return repo.Model{}, false, fmt.Errorf("loader: read metalabel_models: %w", err)
	}
	return parseTimes(m, trainedAt, trainStart, trainEnd, oosStart, oosEnd, names, adoption)
}

func parseTimes(m repo.Model, trainedAt, trainStart, trainEnd, oosStart, oosEnd, names, adoption string) (repo.Model, bool, error) {
	var err error
	if m.TrainedAt, err = sqlutil.ParseTime(trainedAt); err != nil {
		return repo.Model{}, false, err
	}
	if m.TrainStart, err = sqlutil.ParseTime(trainStart); err != nil {
		return repo.Model{}, false, err
	}
	if m.TrainEnd, err = sqlutil.ParseTime(trainEnd); err != nil {
		return repo.Model{}, false, err
	}
	if m.OOSStart, err = sqlutil.ParseTime(oosStart); err != nil {
		return repo.Model{}, false, err
	}
	if m.OOSEnd, err = sqlutil.ParseTime(oosEnd); err != nil {
		return repo.Model{}, false, err
	}
	if err := json.Unmarshal([]byte(names), &m.FeatureNames); err != nil {
		return repo.Model{}, false, fmt.Errorf("loader: decode feature names of model %q: %w", m.Version, err)
	}
	if err := json.Unmarshal([]byte(adoption), &m.Adoption); err != nil {
		return repo.Model{}, false, fmt.Errorf("loader: decode adoption record of model %q: %w", m.Version, err)
	}
	return m, true, nil
}

// ParseStored rebuilds the in-memory *metalabel.Model of a stored row whatever
// its status: metalabel.ParseModel refuses a row that is not active, so a copy
// of the row with the status switched is parsed. The database is not written
// (the row is a value read from a read-only handle); the status of the stored
// model stays "rejected". An error means the model cannot be re-scored.
func ParseStored(m repo.Model) (*metalabel.Model, error) {
	m.Status = repo.ModelActive
	model, err := metalabel.ParseModel(m)
	if err != nil {
		return nil, err
	}
	return &model, nil
}
