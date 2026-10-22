package experiment

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/mcpwritesettings"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// Period is one side of the comparison: the same metrics as
// paper_performance_summary and risk_rejections give for [From, To).
type Period struct {
	From           string                `json:"from"`
	To             string                `json:"to"`
	Performance    query.Performance     `json:"performance"`
	RiskRejections *query.RiskRejections `json:"risk_rejections,omitempty"`
}

// Diff is after minus before, over the two criteria of the verdict.
type Diff struct {
	TotalPnlJPY    float64 `json:"total_pnl_jpy"`
	MaxDrawdownJPY float64 `json:"max_drawdown_jpy"`
}

// Periods a Confounder was found in.
const (
	PeriodBefore   = "before"
	PeriodAfter    = "after"
	PeriodBoundary = "boundary"
)

// Confounder is something other than the experiment's variable that changed
// in the period before or after the experiment's start, or across the two.
type Confounder struct {
	// Kind is setting, model or prompt.
	Kind string `json:"kind"`
	// Period is before (the same length just before the start), after (the
	// experiment's start to its end) or boundary (Jev model / prompt values
	// that differ only when the two periods are put together: A before, B
	// after): where the change was seen.
	Period string `json:"period"`
	// Name is the setting key, or the Jev decision type (scout / trader) whose
	// model / prompt version changed.
	Name string `json:"name"`
	// AuditID and At are the mcp_settings_audit row of a setting change.
	AuditID *int64 `json:"audit_id,omitempty"`
	At      string `json:"at,omitempty"`
	// Values are the model ids / prompt versions seen in the period.
	Values []string `json:"values,omitempty"`
}

// Comparison is experiment_compare's data.
type Comparison struct {
	Experiment Experiment `json:"experiment"`
	// MinTrades is the closed-trade count each period needs (never below
	// MinTradesFloor); MinTradesSource says whether it is the provisional floor
	// (MinTradesProvisionalText) or a value the caller raised.
	MinTrades       int    `json:"min_trades"`
	MinTradesSource string `json:"min_trades_source"`
	Before          Period `json:"before"`
	After           Period `json:"after"`
	Diff            Diff   `json:"diff"`
	// PerTradePnl is the test the verdict rests on (welch.go): the n, mean and
	// standard deviation of the net pnl per trade of each period, the
	// difference of the means, its 95% interval, t and the degrees of freedom.
	PerTradePnl Test         `json:"per_trade_pnl"`
	Confounded  bool         `json:"confounded"`
	Confounders []Confounder `json:"confounders"`
	Verdict     string       `json:"verdict"`
	// Judged is true when the test gave an answer (improved, worsened, mixed,
	// inconclusive) and false when judgement is withheld (insufficient_trades,
	// not_computable, confounded, aborted).
	Judged bool `json:"judged"`
}

// Compare compares experiment exp: the positions closed from its start to its
// end (to now while it runs) against the same length of time just before the
// start. capitalJPY is the Paper capital (see query.PerformanceSummary); a
// minTrades of 0 means MinTradesFloor, one below it is ErrMinTradesBelowFloor.
// withRejections adds each period's risk_rejections.
func Compare(ctx context.Context, q query.Q, exp Experiment, now time.Time, minTrades int, capitalJPY float64, withRejections bool) (Comparison, error) {
	if minTrades != 0 {
		if err := CheckMinTrades(minTrades); err != nil {
			return Comparison{}, err
		}
	}
	c := Comparison{Experiment: exp, MinTrades: minTrades, MinTradesSource: MinTradesSourceRaised, Confounders: []Confounder{}}
	if c.MinTrades <= MinTradesFloor {
		c.MinTrades, c.MinTradesSource = MinTradesFloor, MinTradesSourceFloor
	}
	start, end := exp.window(now)
	before := query.Range{From: start.Add(-end.Sub(start)), To: start}
	after := query.Range{From: start, To: end}
	var beforePnls, afterPnls []float64
	var err error
	if c.Before, beforePnls, err = period(ctx, q, before, capitalJPY, withRejections); err != nil {
		return Comparison{}, err
	}
	if c.After, afterPnls, err = period(ctx, q, after, capitalJPY, withRejections); err != nil {
		return Comparison{}, err
	}
	var jev [2]jevValues
	for i, p := range []struct {
		name string
		r    query.Range
	}{{PeriodBefore, before}, {PeriodAfter, after}} {
		found, seen, err := confounders(ctx, q, exp, p.name, p.r)
		if err != nil {
			return Comparison{}, err
		}
		c.Confounders = append(c.Confounders, found...)
		jev[i] = seen
	}
	c.Confounders = append(c.Confounders, boundaryConfounders(exp, c.Confounders, jev[0], jev[1])...)
	c.Confounded = len(c.Confounders) > 0
	c.Diff = Diff{
		TotalPnlJPY:    c.After.Performance.TotalPnlJPY - c.Before.Performance.TotalPnlJPY,
		MaxDrawdownJPY: c.After.Performance.MaxDrawdownJPY - c.Before.Performance.MaxDrawdownJPY,
	}
	c.PerTradePnl = Welch(beforePnls, afterPnls)
	c.Verdict = verdict(c)
	switch c.Verdict {
	case VerdictImproved, VerdictWorsened, VerdictMixed, VerdictInconclusive:
		c.Judged = true
	}
	return c, nil
}

// period reads one side: its Performance (paper_performance_summary's own
// calculation) and the net pnl of each of the very trades it counted.
func period(ctx context.Context, q query.Q, r query.Range, capitalJPY float64, withRejections bool) (Period, []float64, error) {
	p := Period{From: r.From.UTC().Format(time.RFC3339), To: r.To.UTC().Format(time.RFC3339)}
	perf, pnls, err := query.PerformanceSummary(ctx, q, r, capitalJPY)
	if err != nil {
		return Period{}, nil, err
	}
	p.Performance = perf
	if withRejections {
		rr, err := query.Rejections(ctx, q, r)
		if err != nil {
			return Period{}, nil, err
		}
		p.RiskRejections = &rr
	}
	return p, pnls, nil
}

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
//     a different value does);
//   - more than one Jev model_id, or question_version, was recorded in
//     jev_decisions for one decision type (scout / trader). For a model or
//     prompt experiment that variable is allowed two values in a period (the
//     old and the new one, switching near the start) and no more.
func confounders(ctx context.Context, q query.Q, exp Experiment, period string, r query.Range) ([]Confounder, jevValues, error) {
	out := []Confounder{}
	seen := jevValues{models: map[string][]string{}, prompts: map[string][]string{}}
	from, to := sqlutil.FormatTime(r.From), sqlutil.FormatTime(r.To)

	rows, err := q.QueryContext(ctx, `
SELECT id, at, setting_key, after_value FROM mcp_settings_audit
WHERE at >= ? AND at < ? AND action IN ('set', 'reset') ORDER BY id`, from, to)
	if err != nil {
		return nil, seen, fmt.Errorf("experiment: changed settings: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var (
			id         int64
			at, key    string
			afterValue *string
		)
		if err := rows.Scan(&id, &at, &key, &afterValue); err != nil {
			return nil, seen, fmt.Errorf("experiment: scan changed setting: %w", err)
		}
		if !mcpwritesettings.IsAllowed(key) || (exp.Variable.AuditID != nil && *exp.Variable.AuditID == id) {
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
