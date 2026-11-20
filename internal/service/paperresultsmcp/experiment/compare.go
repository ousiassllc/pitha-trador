package experiment

import (
	"context"
	"time"

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

// Kinds of Confounder besides the three KindSetting / KindModel / KindPrompt
// (which are also the kinds of an experiment's variable): ConfounderPolicy is a
// Self-Improvement proposal applied or rolled back (policy_proposals), which
// changes the Policy Engine's thresholds and so the policy_version of the
// signals. An experiment never varies it, so it is a confounder or nothing.
const ConfounderPolicy = "policy"

// What happened to the policy proposal of a ConfounderPolicy (its Values).
const (
	PolicyApplied    = "applied"
	PolicyRolledBack = "rolled_back"
)

// Confounder is something other than the experiment's variable that changed
// in the period before or after the experiment's start, or across the two.
type Confounder struct {
	// Kind is setting, model, prompt or policy.
	Kind string `json:"kind"`
	// Period is before (the same length just before the start), after (the
	// experiment's start to its end) or boundary (Jev model / prompt values
	// that differ only when the two periods are put together: A before, B
	// after): where the change was seen.
	Period string `json:"period"`
	// Name is the setting key, or the Jev decision type (scout / trader) whose
	// model / prompt version changed, or the applied_policy_version of the
	// policy proposal (a ConfounderPolicy; "" when the row has none).
	Name string `json:"name"`
	// AuditID and At are the mcp_settings_audit row of a setting change; At is
	// also when a model switch was saved or a policy proposal applied / rolled
	// back.
	AuditID *int64 `json:"audit_id,omitempty"`
	At      string `json:"at,omitempty"`
	// Values are the model ids / prompt versions seen in the period, or the
	// one PolicyApplied / PolicyRolledBack of a policy proposal.
	Values []string `json:"values,omitempty"`
}

// Windows are the two periods Compare reads for exp at now (before is the same
// length of time just before the start): the exact instants, for the readers that
// must cover the very same windows: the label-based comparison (labelcompare) and
// the promotion checklist's per-day figures.
func (e Experiment) Windows(now time.Time) (before, after query.Range) { return e.windows(now) }

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
	// Switch is whether the variable really went from its declared before to
	// its declared after (switch.go). Anything but SwitchConfirmed withholds
	// the verdict (VerdictSwitchUnconfirmed).
	Switch  SwitchCheck `json:"switch_check"`
	Verdict string      `json:"verdict"`
	// Judged is true when the test gave an answer (improved, worsened, mixed,
	// inconclusive) and false when judgement is withheld (insufficient_trades,
	// switch_unconfirmed, not_computable, confounded, aborted).
	Judged bool `json:"judged"`
}

// Compare compares experiment exp: the positions closed from its start to its
// end (to now while it runs) against the same length of time just before the
// start. capitalJPY is the Paper capital (see query.PerformanceSummary); a
// minTrades of 0 means MinTradesFloor, one below it is ErrMinTradesBelowFloor.
// withRejections adds each period's risk_rejections.
func Compare(ctx context.Context, q query.Q, exp Experiment, now time.Time, minTrades int, capitalJPY float64, withRejections bool) (Comparison, error) {
	c, err := newComparison(exp, minTrades)
	if err != nil {
		return Comparison{}, err
	}
	before, after := exp.windows(now)
	var beforePnls, afterPnls []float64
	if c.Before, beforePnls, err = period(ctx, q, before, capitalJPY, withRejections); err != nil {
		return Comparison{}, err
	}
	if c.After, afterPnls, err = period(ctx, q, after, capitalJPY, withRejections); err != nil {
		return Comparison{}, err
	}
	if err := c.examine(ctx, q, before, after); err != nil {
		return Comparison{}, err
	}
	c.finish(beforePnls, afterPnls)
	return c, nil
}

// newComparison starts a Comparison of exp with the minimum number of trades
// resolved (see Compare).
func newComparison(exp Experiment, minTrades int) (Comparison, error) {
	if minTrades != 0 {
		if err := CheckMinTrades(minTrades); err != nil {
			return Comparison{}, err
		}
	}
	c := Comparison{Experiment: exp, MinTrades: minTrades, MinTradesSource: MinTradesSourceRaised, Confounders: []Confounder{}}
	if c.MinTrades <= MinTradesFloor {
		c.MinTrades, c.MinTradesSource = MinTradesFloor, MinTradesSourceFloor
	}
	return c, nil
}

// examine fills in what decides whether the experiment may be judged at all,
// without reading a single trade: the confounders of the two periods and of
// their boundary, and whether the variable really switched (switch.go).
func (c *Comparison) examine(ctx context.Context, q query.Q, before, after query.Range) error {
	var jev [2]jevValues
	for i, p := range []struct {
		name string
		r    query.Range
	}{{PeriodBefore, before}, {PeriodAfter, after}} {
		found, seen, err := confounders(ctx, q, c.Experiment, p.name, p.r)
		if err != nil {
			return err
		}
		c.Confounders = append(c.Confounders, found...)
		jev[i] = seen
	}
	c.Confounders = append(c.Confounders, boundaryConfounders(c.Experiment, c.Confounders, jev[0], jev[1])...)
	c.Confounded = len(c.Confounders) > 0
	var err error
	c.Switch, err = checkSwitch(ctx, q, c.Experiment, before, after, jev)
	return err
}

// finish computes the difference, the test and the verdict from the per-trade
// pnl of the two periods (the periods' Performance is already in c).
func (c *Comparison) finish(beforePnls, afterPnls []float64) {
	c.Diff = Diff{
		TotalPnlJPY:    c.After.Performance.TotalPnlJPY - c.Before.Performance.TotalPnlJPY,
		MaxDrawdownJPY: c.After.Performance.MaxDrawdownJPY - c.Before.Performance.MaxDrawdownJPY,
	}
	c.PerTradePnl = Welch(beforePnls, afterPnls)
	c.Verdict = verdict(*c)
	c.Judged = judgedVerdict(c.Verdict)
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
