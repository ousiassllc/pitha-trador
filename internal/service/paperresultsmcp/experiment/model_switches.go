package experiment

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// The Settings screen's record of switching the Jev model (JEV_MODEL,
// jev_model_changes, issue #885) is the second source of model changes next to
// jev_decisions.model_id. A decision carries its model only once it has run, so
// a switch in a period with no decision is visible in the record alone. The
// record starts with migration 000048: a switch before it left no row, and no
// row means "no record", never "no switch".

// ModelSwitchName is the Confounder.Name of a recorded switch of the
// configured Jev model (the Settings key); the Name of a confounder found in
// jev_decisions is the decision type (scout / trader).
const ModelSwitchName = "JEV_MODEL"

// UnsetModel stands for an unset JEV_MODEL (the default model applies) in the
// Values of a recorded switch.
const UnsetModel = "未設定（既定のモデル）"

// modelSwitches lists the switches of the configured Jev model saved in
// [from, to) (SQL text bounds), oldest first, as confounders: Values are
// [before, after] and At is when the switch was saved. Saving does not apply a
// model: the new one is used from the next application start, so a switch saved
// inside a period may take effect after it. It is counted all the same, because
// the period cannot be shown to have run on one model. A model experiment's own
// switch - to the model it names as its After - is not a confounder, like a
// setting experiment's own change.
func modelSwitches(ctx context.Context, q query.Q, exp Experiment, period, from, to string) ([]Confounder, error) {
	rows, err := q.QueryContext(ctx, `
SELECT at, before_value, after_value FROM jev_model_changes WHERE at >= ? AND at < ? ORDER BY id`, from, to)
	if err != nil {
		return nil, fmt.Errorf("experiment: model switches: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []Confounder{}
	for rows.Next() {
		var at string
		var before, after *string
		if err := rows.Scan(&at, &before, &after); err != nil {
			return nil, fmt.Errorf("experiment: scan model switch: %w", err)
		}
		if exp.Variable.Kind == KindModel && after != nil && *after == exp.Variable.After {
			continue
		}
		out = append(out, Confounder{Kind: KindModel, Period: period, Name: ModelSwitchName, At: query.FormatOut(at), Values: []string{modelOrUnset(before), modelOrUnset(after)}})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("experiment: model switches: %w", err)
	}
	return out, nil
}

func modelOrUnset(v *string) string {
	if v == nil {
		return UnsetModel
	}
	return *v
}

// checkModelStart refuses a model experiment whose declared start is earlier
// than the recorded switch to the model it names as its After: the model could
// not have been in use before it was saved. With no such record (the switch
// predates the record, or was made some other way) the declared start is taken
// as given.
func checkModelStart(ctx context.Context, q query.Q, c Change, started time.Time) error {
	var at string
	err := q.QueryRowContext(ctx, `SELECT at FROM jev_model_changes WHERE after_value = ? ORDER BY id DESC LIMIT 1`, c.After).Scan(&at)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("experiment: read latest model switch: %w", err)
	}
	switched, err := sqlutil.ParseTime(at)
	if err != nil {
		return fmt.Errorf("experiment: %w", err)
	}
	if started.Before(switched) {
		return invalid("started_at は、after のモデルへの切り替えを保存した時刻（%s）以降にしてください", switched.UTC().Format(time.RFC3339))
	}
	return nil
}
