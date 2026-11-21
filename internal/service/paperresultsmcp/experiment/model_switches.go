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

// RoleSwitchName is the Confounder.Name of a recorded change of the live Jev
// role's provider or model on the Settings screen (ai_role_history, issue
// #1080): the AI provider layer's counterpart of ModelSwitchName. Values are the
// "provider / model" labels before and after.
const RoleSwitchName = "AI_ROLE_JEV"

// UnsetModel stands for an unset JEV_MODEL (the default model applies) in the
// Values of a recorded switch.
const UnsetModel = "未設定（既定のモデル）"

// UnsetValue stands for a setting that has no value (an unset key whose default
// is empty) as the Before of a setting experiment, which cannot be empty.
const UnsetValue = "（未設定）"

// modelSwitches lists the switches of the configured Jev model saved in
// [from, to) (SQL text bounds), oldest first, as confounders of the given
// period (PeriodBefore or PeriodAfter; a switch is timestamped, so it is never
// a boundary one): Values are [before, after] and At is when the switch was
// saved. Saving does not apply a model: the new one is used from the next
// application start, so a switch saved inside a period may take effect after
// it. It is counted all the same, because the period cannot be shown to have
// run on one model. A model experiment's own switch - to the model it names as
// its After - is not a confounder in either period, like a setting
// experiment's own change.
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
	roles, err := roleSwitches(ctx, q, period, from, to)
	if err != nil {
		return nil, err
	}
	return append(out, roles...), nil
}

// roleSwitches lists the changes of the live Jev role's provider and model saved
// in [from, to) (ai_role_history, kind 'saved'), oldest first, like modelSwitches.
// Only a switch that changes the provider or the role's own model name is a row
// there; like JEV_MODEL it takes effect at the next start, and is counted when it
// was saved inside the period.
func roleSwitches(ctx context.Context, q query.Q, period, from, to string) ([]Confounder, error) {
	rows, err := q.QueryContext(ctx, `
SELECT at, before_provider, before_model, provider, model FROM ai_role_history
WHERE kind = 'saved' AND role = 'jev' AND at >= ? AND at < ? ORDER BY id`, from, to)
	if err != nil {
		return nil, fmt.Errorf("experiment: ai role switches: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []Confounder{}
	for rows.Next() {
		var at, bp, bm, ap, am string
		if err := rows.Scan(&at, &bp, &bm, &ap, &am); err != nil {
			return nil, fmt.Errorf("experiment: scan ai role switch: %w", err)
		}
		out = append(out, Confounder{Kind: KindModel, Period: period, Name: RoleSwitchName, At: query.FormatOut(at), Values: []string{roleLabel(bp, bm), roleLabel(ap, am)}})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("experiment: ai role switches: %w", err)
	}
	return out, nil
}

// roleLabel is the "provider / model" label of a stored pair; an empty model is
// the provider's default.
func roleLabel(provider, model string) string {
	if model == "" {
		model = UnsetModel
	}
	return provider + " / " + model
}

func modelOrUnset(v *string) string {
	if v == nil {
		return UnsetModel
	}
	return *v
}

// checkModelStart refuses a model experiment whose declared start precedes the
// switch that put its After model in use. The model in effect at started is
// read from the record: the after_value of the latest switch saved at or
// before started, or, when there is none, the before_value of the oldest
// switch (the model in use before the record began). The start is refused only
// when that model is known (not unset or absent from the record), is not the
// After model, and a switch to the After model was saved later: the After model
// cannot have been in use at started. Any other case (the After model was
// already in effect, an unknown model, no switch to it on record - it predates
// the record or was made some other way) takes the declared start as given.
// Comparing only with the latest switch to After would refuse a start in a
// period when After was in use before another model and again after it.
func checkModelStart(ctx context.Context, q query.Q, c Change, started time.Time) error {
	s := sqlutil.FormatTime(started)
	inEffect, err := modelInEffect(ctx, q, s)
	if err != nil {
		return err
	}
	if inEffect == nil || *inEffect == c.After {
		return nil
	}
	var at string
	err = q.QueryRowContext(ctx, `SELECT at FROM jev_model_changes WHERE after_value = ? AND at > ? ORDER BY at, id LIMIT 1`, c.After, s).Scan(&at)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("experiment: read later model switch: %w", err)
	}
	switched, err := sqlutil.ParseTime(at)
	if err != nil {
		return fmt.Errorf("experiment: %w", err)
	}
	return invalid("started_at の時点で使われていたのは %s で、after のモデルへの切り替えは %s（JST）に保存されています。started_at は、それ以降にしてください",
		*inEffect, switched.In(query.JST).Format("2006-01-02 15:04:05"))
}

// modelInEffect returns the model in use at s (SQL text time) according to the
// record, or nil when it is unknown: no record, or the model is unset.
func modelInEffect(ctx context.Context, q query.Q, s string) (*string, error) {
	var model *string
	err := q.QueryRowContext(ctx, `SELECT after_value FROM jev_model_changes WHERE at <= ? ORDER BY at DESC, id DESC LIMIT 1`, s).Scan(&model)
	if errors.Is(err, sql.ErrNoRows) {
		err = q.QueryRowContext(ctx, `SELECT before_value FROM jev_model_changes ORDER BY at, id LIMIT 1`).Scan(&model)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
	}
	if err != nil {
		return nil, fmt.Errorf("experiment: read model in effect: %w", err)
	}
	return model, nil
}
