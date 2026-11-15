package evaldiag

import (
	"cmp"
	"context"
	"slices"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// Attrs are the attributes of a decision the slicing tools (issue #1089) cut
// the evaluation by. A missing value is nil (or "" for the text ones): the
// decision's own columns, and the market_snapshots bar of its timestamp and the
// instrument's sector.
type Attrs struct {
	// Regime is jev_decisions.market_regime ("" when not recorded).
	Regime string
	// VolatilityBps is jev_decisions.regime_volatility_bps.
	VolatilityBps *float64
	// Confidence is jev_decisions.confidence.
	Confidence *float64
	// SpreadBps and Turnover5m are the decision's own bar's spread_bps and turnover_5m.
	SpreadBps, Turnover5m *float64
	// Sector is instruments.sector ("" when not recorded).
	Sector string
}

// Row is one evaluated decision as the other research tools (the slicing, the
// candidate ledger, the promotion checklist) use it: the decision's identity, the
// LLM's own net return and the net return (bps, after the label's cost) a LONG
// and a SHORT of the same decision would have had, the baselines' returns, and
// the decision's attributes. Every Row passed the same exclusions as the
// jev_evaluation comparison set: LLM leg, opposite leg and the decision's own bar
// all exist, so every field of it is defined.
type Row struct {
	DecisionID      int64
	At              time.Time
	Day             string // JST date of At
	ModelID         string
	QuestionVersion string
	// Direction is the decision's own direction (LONG or SHORT).
	Direction string
	// Entered: a trade signal of the decision passed Risk.
	Entered bool
	// LLM, Long and Short are the net bps of the decision's own direction, of a LONG and of a SHORT.
	LLM, Long, Short float64
	// Random, Momentum and Contrarian are the baseline arms' net bps (jev_evaluation's definitions; the none arm is 0).
	Random, Momentum, Contrarian float64
	Attrs                        Attrs
}

// Meta describes what Rows evaluated.
type Meta struct {
	HorizonMinutes int
	// Decisions is the number of LONG/SHORT trader decisions read (before exclusions).
	Decisions int
	// CutoffApplied: decisions before metalabel.llm_cutoff were dropped.
	CutoffApplied bool
}

// Rows evaluates the decisions of [r.From, r.To) like Run and returns the
// evaluated rows (oldest first, all cohorts) instead of the aggregate. The
// horizon is req.HorizonMinutes or the barrier timeout in force. It reads only
// and is bounded by the same limits as Run (query.ErrLimitExceeded).
func Rows(ctx context.Context, q query.Q, r query.Range, req Request) ([]Row, Meta, error) {
	c, err := collect(ctx, q, r, req)
	if err != nil {
		return nil, Meta{}, err
	}
	var out []Row
	for _, acc := range c.order {
		for _, e := range acc.rows {
			out = append(out, Row{
				DecisionID: e.id, At: e.at, Day: e.day, ModelID: acc.key.model, QuestionVersion: acc.key.version,
				Direction: e.dir, Entered: e.entered,
				LLM: e.net[iLLM], Long: e.net[iLong], Short: e.net[iShort],
				Random: e.net[iRandom], Momentum: e.net[iMomentum], Contrarian: e.net[iContrarian], Attrs: e.attrs,
			})
		}
	}
	sortRows(out)
	return out, Meta{HorizonMinutes: c.horizon, Decisions: c.decisions, CutoffApplied: c.cutoff.Applied}, nil
}

// sortRows orders rows by time then decision id (cohorts interleave).
func sortRows(rows []Row) {
	slices.SortStableFunc(rows, func(a, b Row) int {
		return cmp.Or(a.At.Compare(b.At), cmp.Compare(a.DecisionID, b.DecisionID))
	})
}
