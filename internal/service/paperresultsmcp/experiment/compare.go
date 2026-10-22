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

// Confounder is something other than the experiment's variable that changed
// while the experiment ran.
type Confounder struct {
	// Kind is setting, model or prompt.
	Kind string `json:"kind"`
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
	if c.Confounders, err = confounders(ctx, q, exp, after); err != nil {
		return Comparison{}, err
	}
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

// confounders lists what changed during the experiment (the period r, the
// experiment's start to its end) besides its variable:
//
//   - a whitelisted setting changed or reset (mcp_settings_audit; the
//     experiment's own change - its audit_id, or a later save of the target
//     setting to the same value - does not count, but a later save of the target
//     to a different value does);
//   - more than one Jev model_id, or question_version, was recorded in
//     jev_decisions for one decision type (scout / trader). For a model or
//     prompt experiment that variable is allowed two values (before and after)
//     and no more.
//
// The baseline period before the start is not inspected: the issue defines the
// check over the experiment's own period.
func confounders(ctx context.Context, q query.Q, exp Experiment, r query.Range) ([]Confounder, error) {
	out := []Confounder{}
	from, to := sqlutil.FormatTime(r.From), sqlutil.FormatTime(r.To)

	rows, err := q.QueryContext(ctx, `
SELECT id, at, setting_key, after_value FROM mcp_settings_audit
WHERE at >= ? AND at < ? AND action IN ('set', 'reset') ORDER BY id`, from, to)
	if err != nil {
		return nil, fmt.Errorf("experiment: changed settings: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var (
			id         int64
			at, key    string
			afterValue *string
		)
		if err := rows.Scan(&id, &at, &key, &afterValue); err != nil {
			return nil, fmt.Errorf("experiment: scan changed setting: %w", err)
		}
		if !mcpwritesettings.IsAllowed(key) || (exp.Variable.AuditID != nil && *exp.Variable.AuditID == id) {
			continue
		}
		if exp.Variable.Kind == KindSetting && key == exp.Variable.Name && afterValue != nil && sameValue(*afterValue, exp.Variable.After) {
			continue
		}
		out = append(out, Confounder{Kind: KindSetting, Name: key, AuditID: &id, At: query.FormatOut(at)})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("experiment: changed settings: %w", err)
	}

	jev, err := q.QueryContext(ctx, `
SELECT decision_type, model_id, question_version FROM jev_decisions
WHERE timestamp >= ? AND timestamp < ?
GROUP BY decision_type, model_id, question_version ORDER BY decision_type, model_id, question_version`, from, to)
	if err != nil {
		return nil, fmt.Errorf("experiment: jev models and prompts: %w", err)
	}
	defer func() { _ = jev.Close() }()
	models, prompts := map[string][]string{}, map[string][]string{}
	for jev.Next() {
		var typ, model, prompt string
		if err := jev.Scan(&typ, &model, &prompt); err != nil {
			return nil, fmt.Errorf("experiment: scan jev decision: %w", err)
		}
		if !slices.Contains(models[typ], model) {
			models[typ] = append(models[typ], model)
		}
		if !slices.Contains(prompts[typ], prompt) {
			prompts[typ] = append(prompts[typ], prompt)
		}
	}
	if err := jev.Err(); err != nil {
		return nil, fmt.Errorf("experiment: jev models and prompts: %w", err)
	}
	out = append(out, changedValues(KindModel, exp.Variable.Kind, models)...)
	out = append(out, changedValues(KindPrompt, exp.Variable.Kind, prompts)...)
	return out, nil
}

// changedValues reports, for each decision type, the values of one axis (kind)
// when there are more than allowed: one normally, two when kind is the
// experiment's own variable.
func changedValues(kind, target string, seen map[string][]string) []Confounder {
	allowed := 1
	if kind == target {
		allowed = 2
	}
	var out []Confounder
	for _, typ := range slices.Sorted(maps.Keys(seen)) {
		if values := seen[typ]; len(values) > allowed {
			out = append(out, Confounder{Kind: kind, Name: typ, Values: values})
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
