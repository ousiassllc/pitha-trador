package metalabel

import (
	"context"
	"fmt"
)

// ListActiveModels returns every active model, oldest TrainedAt first (the
// lower ID first on a tie): the history a backtest replay walks forward through
// to know which model was in force at each decision time (issue #775). Rejected
// models are never in force, so they are not returned. The rows are read as
// stored; nothing is derived or added.
func (r *Repository) ListActiveModels(ctx context.Context) ([]Model, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+modelColumns+` FROM metalabel_models
		WHERE status = ? ORDER BY trained_at ASC, id ASC`, ModelActive)
	if err != nil {
		return nil, fmt.Errorf("repository: list active meta-label models: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []Model
	for rows.Next() {
		m, err := scanModel(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repository: list active meta-label models: %w", err)
	}
	return out, nil
}
