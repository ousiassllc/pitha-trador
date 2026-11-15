package experiment

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/ousiassllc/pitha-trador/internal/config/mcpwritesettings"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// What changed besides the experiment's variable: the confounders of one
// period, of the boundary between the two, and the checks they share.

// jevValues are the Jev model ids and prompt versions (question_version) seen
// in one period, by decision type (scout / trader).
type jevValues struct {
	models, prompts map[string][]string
}

// confounders lists what changed in one period r of the comparison (period is
// PeriodBefore or PeriodAfter) besides the experiment's variable, and returns
// the Jev values seen in r for boundaryConfounders. It is run on both periods:
// a change in the before period makes the two periods differ in more than the
// variable just as one in the after period does.
//
//   - a whitelisted setting changed or reset (mcp_settings_audit; the
//     experiment's own change - its audit_id, or a later save of the target
//     setting to the same value - does not count, but a save of the target to
//     a different value does). A save that left the value as it was (the same
//     value stored again; a reset of a key that had no stored value) changed
//     nothing, so it is no confounder either (noChange);
//   - more than one Jev model_id, or question_version, was recorded in
//     jev_decisions for one decision type (scout / trader). For a model or
//     prompt experiment that variable is allowed two values in a period (the
//     old and the new one, switching near the start) and no more;
//   - a Self-Improvement policy proposal applied or rolled back
//     (policyChanges), which moves the Policy Engine's thresholds.
func confounders(ctx context.Context, q query.Q, exp Experiment, period string, r query.Range) ([]Confounder, jevValues, error) {
	out := []Confounder{}
	seen := jevValues{models: map[string][]string{}, prompts: map[string][]string{}}
	from, to := sqlutil.FormatTime(r.From), sqlutil.FormatTime(r.To)

	rows, err := q.QueryContext(ctx, `
SELECT id, at, setting_key, before_value, after_value FROM mcp_settings_audit
WHERE at >= ? AND at < ? AND action IN ('set', 'reset') ORDER BY id`, from, to)
	if err != nil {
		return nil, seen, fmt.Errorf("experiment: changed settings: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var (
			id                      int64
			at, key                 string
			beforeValue, afterValue *string
		)
		if err := rows.Scan(&id, &at, &key, &beforeValue, &afterValue); err != nil {
			return nil, seen, fmt.Errorf("experiment: scan changed setting: %w", err)
		}
		if !mcpwritesettings.IsAllowed(key) || (exp.Variable.AuditID != nil && *exp.Variable.AuditID == id) || noChange(beforeValue, afterValue) {
			continue
		}
		if exp.Variable.Kind == KindSetting && key == exp.Variable.Name && afterValue != nil && sameValue(*afterValue, exp.Variable.After) {
			continue
		}
		out = append(out, Confounder{Kind: KindSetting, Period: period, Name: key, AuditID: &id, At: query.FormatOut(at)})
	}
	if err := rows.Err(); err != nil {
		return nil, seen, fmt.Errorf("experiment: changed settings: %w", err)
	}

	switches, err := modelSwitches(ctx, q, exp, period, from, to)
	if err != nil {
		return nil, seen, err
	}
	out = append(out, switches...)

	policy, err := policyChanges(ctx, q, period, from, to)
	if err != nil {
		return nil, seen, err
	}
	out = append(out, policy...)

	jev, err := q.QueryContext(ctx, `
SELECT decision_type, model_id, question_version FROM jev_decisions
WHERE timestamp >= ? AND timestamp < ?
GROUP BY decision_type, model_id, question_version ORDER BY decision_type, model_id, question_version`, from, to)
	if err != nil {
		return nil, seen, fmt.Errorf("experiment: jev models and prompts: %w", err)
	}
	defer func() { _ = jev.Close() }()
	for jev.Next() {
		var typ, model, prompt string
		if err := jev.Scan(&typ, &model, &prompt); err != nil {
			return nil, seen, fmt.Errorf("experiment: scan jev decision: %w", err)
		}
		if !slices.Contains(seen.models[typ], model) {
			seen.models[typ] = append(seen.models[typ], model)
		}
		if !slices.Contains(seen.prompts[typ], prompt) {
			seen.prompts[typ] = append(seen.prompts[typ], prompt)
		}
	}
	if err := jev.Err(); err != nil {
		return nil, seen, fmt.Errorf("experiment: jev models and prompts: %w", err)
	}
	out = append(out, changedValues(KindModel, period, exp.Variable.Kind, seen.models)...)
	out = append(out, changedValues(KindPrompt, period, exp.Variable.Kind, seen.prompts)...)
	return out, seen, nil
}

// boundaryConfounders catches the Jev model / prompt switch that falls between
// the two periods: before has only A and after only B, so no period alone sees
// two values. For each axis other than the experiment's own variable (that one
// is expected to switch), the values of both periods are taken as one set and
// more than one value per decision type is a confounder with Period
// PeriodBoundary. A type and axis already in found (reported per period) is
// not repeated.
func boundaryConfounders(exp Experiment, found []Confounder, before, after jevValues) []Confounder {
	var out []Confounder
	for _, axis := range []struct {
		kind          string
		before, after map[string][]string
	}{{KindModel, before.models, after.models}, {KindPrompt, before.prompts, after.prompts}} {
		if axis.kind == exp.Variable.Kind {
			continue
		}
		union := map[string][]string{}
		for _, seen := range []map[string][]string{axis.before, axis.after} {
			for typ, values := range seen {
				for _, v := range values {
					if !slices.Contains(union[typ], v) {
						union[typ] = append(union[typ], v)
					}
				}
			}
		}
		for _, typ := range slices.Sorted(maps.Keys(union)) {
			reported := slices.ContainsFunc(found, func(f Confounder) bool { return f.Kind == axis.kind && f.Name == typ })
			if values := union[typ]; len(values) > 1 && !reported {
				out = append(out, Confounder{Kind: axis.kind, Period: PeriodBoundary, Name: typ, Values: values})
			}
		}
	}
	return out
}

// changedValues reports, for each decision type, the values of one axis (kind)
// when there are more than allowed: one normally, two when kind is the
// experiment's own variable.
func changedValues(kind, period, target string, seen map[string][]string) []Confounder {
	allowed := 1
	if kind == target {
		allowed = 2
	}
	var out []Confounder
	for _, typ := range slices.Sorted(maps.Keys(seen)) {
		if values := seen[typ]; len(values) > allowed {
			out = append(out, Confounder{Kind: kind, Period: period, Name: typ, Values: values})
		}
	}
	return out
}

// sameValue compares two stored setting values: equal as text, or as numbers
// ("0.50" and "0.5").
func sameValue(a, b string) bool {
	a, b = strings.TrimSpace(a), strings.TrimSpace(b)
	if a == b {
		return true
	}
	fa, ea := strconv.ParseFloat(a, 64)
	fb, eb := strconv.ParseFloat(b, 64)
	return ea == nil && eb == nil && fa == fb
}
