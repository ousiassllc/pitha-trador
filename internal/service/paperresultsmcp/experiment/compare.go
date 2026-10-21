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

// DefaultMinTrades is how many closed Paper trades EACH of the two periods
// needs before an experiment is judged. Fewer is verdict insufficient_trades:
// the figures are still returned, the win / lose call is not.
//
// PROVISIONAL PROPOSAL, NOT APPROVED by あいり (issue #871: the implementer
// proposes, あいり approves, and only then is the value final). It is kept in
// this one place; experiments_list / experiment_compare accept min_trades to
// override it per call.
//
// Basis (docs/architecture/paper-results-mcp-experiments.md §5): the two-sample
// comparison of the mean net pnl per trade, two-sided alpha 0.05, power 0.8,
// to detect a difference of half a standard deviation of the per-trade pnl
// (d = 0.5): n = 2 * (1.96 + 0.84)^2 / d^2 = 62.8 -> 63 per period, +1 for the
// t distribution = 64. The real per-trade standard deviation is NOT measured
// yet (no Paper database was available): it must be checked against the
// production database before approval.
const DefaultMinTrades = 64

// MaxMinTrades is the largest min_trades a caller may ask for.
const MaxMinTrades = 10000

// Where the min_trades of a response came from.
const (
	// MinTradesProvisional: DefaultMinTrades, which is not approved yet.
	MinTradesProvisional = "default_provisional_unapproved"
	// MinTradesArgument: the caller's own min_trades.
	MinTradesArgument = "argument"
)

// Verdicts. Judged ones (improved, worsened, mixed) compare only the net pnl
// (after trading costs) and the maximum drawdown; the win rate and the rest
// are shown for reference and never decide.
const (
	// VerdictInsufficientTrades: a period has fewer than min_trades closed trades.
	VerdictInsufficientTrades = "insufficient_trades"
	// VerdictConfounded: something other than the variable changed during the
	// experiment (Confounders lists it), so the comparison says nothing.
	VerdictConfounded = "confounded"
	// VerdictAborted: the experiment was given up; it is not judged.
	VerdictAborted = "aborted"
	// VerdictImproved: the net pnl is higher and the drawdown no larger.
	VerdictImproved = "improved"
	// VerdictWorsened: the net pnl is lower and the drawdown no smaller.
	VerdictWorsened = "worsened"
	// VerdictMixed: the net pnl and the drawdown do not point the same way
	// (or are equal), so there is no clear win or loss.
	VerdictMixed = "mixed"
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
	// MinTrades is the closed-trade count each period needs; MinTradesSource
	// says whether it is the unapproved default or the caller's value.
	MinTrades       int          `json:"min_trades"`
	MinTradesSource string       `json:"min_trades_source"`
	Before          Period       `json:"before"`
	After           Period       `json:"after"`
	Diff            Diff         `json:"diff"`
	Confounded      bool         `json:"confounded"`
	Confounders     []Confounder `json:"confounders"`
	Verdict         string       `json:"verdict"`
	// Judged is true when Verdict is a win / lose call (improved, worsened,
	// mixed) and false when none is given (insufficient_trades, confounded,
	// aborted).
	Judged bool `json:"judged"`
}

// Compare compares experiment exp: the positions closed from its start to its
// end (to now while it runs) against the same length of time just before the
// start. capitalJPY is the Paper capital (see query.PerformanceSummary); a
// minTrades of 0 means DefaultMinTrades. withRejections adds each period's
// risk_rejections.
func Compare(ctx context.Context, q query.Q, exp Experiment, now time.Time, minTrades int, capitalJPY float64, withRejections bool) (Comparison, error) {
	c := Comparison{Experiment: exp, MinTrades: minTrades, MinTradesSource: MinTradesArgument, Confounders: []Confounder{}}
	if c.MinTrades == 0 {
		c.MinTrades, c.MinTradesSource = DefaultMinTrades, MinTradesProvisional
	}
	start, end := exp.window(now)
	before := query.Range{From: start.Add(-end.Sub(start)), To: start}
	after := query.Range{From: start, To: end}
	var err error
	if c.Before, err = period(ctx, q, before, capitalJPY, withRejections); err != nil {
		return Comparison{}, err
	}
	if c.After, err = period(ctx, q, after, capitalJPY, withRejections); err != nil {
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
	c.Verdict = verdict(c)
	c.Judged = c.Verdict == VerdictImproved || c.Verdict == VerdictWorsened || c.Verdict == VerdictMixed
	return c, nil
}

func period(ctx context.Context, q query.Q, r query.Range, capitalJPY float64, withRejections bool) (Period, error) {
	p := Period{From: r.From.UTC().Format(time.RFC3339), To: r.To.UTC().Format(time.RFC3339)}
	perf, _, err := query.PerformanceSummary(ctx, q, r, capitalJPY)
	if err != nil {
		return Period{}, err
	}
	p.Performance = perf
	if withRejections {
		rr, err := query.Rejections(ctx, q, r)
		if err != nil {
			return Period{}, err
		}
		p.RiskRejections = &rr
	}
	return p, nil
}

// verdict applies the rules in order: aborted and confounded experiments and
// thin periods are not judged; otherwise only net pnl and drawdown decide.
func verdict(c Comparison) string {
	switch {
	case c.Experiment.Status == StatusAborted:
		return VerdictAborted
	case c.Confounded:
		return VerdictConfounded
	case c.Before.Performance.ClosedTradeCount < c.MinTrades || c.After.Performance.ClosedTradeCount < c.MinTrades:
		return VerdictInsufficientTrades
	case c.Diff.TotalPnlJPY > 0 && c.Diff.MaxDrawdownJPY <= 0:
		return VerdictImproved
	case c.Diff.TotalPnlJPY < 0 && c.Diff.MaxDrawdownJPY >= 0:
		return VerdictWorsened
	}
	return VerdictMixed
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
