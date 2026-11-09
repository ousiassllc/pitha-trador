package labellayer

import (
	"context"
	"errors"
	"fmt"
	"sort"

	repo "github.com/ousiassllc/pitha-trador/internal/repository/metalabel"
	"github.com/ousiassllc/pitha-trador/internal/service/marketcalendar"
	"github.com/ousiassllc/pitha-trador/internal/service/metaablation"
	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/eligibility"
	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/independence"
	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/manifest"
	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/stats"
	"github.com/ousiassllc/pitha-trador/internal/service/metalabel"
)

// ErrNotRescorable is returned when the stored model cannot score (no model, an
// unusable one, an invalid configuration): the run is not executable and the
// reason says why.
var ErrNotRescorable = errors.New("labellayer: the model cannot be re-scored")

// Input is everything table 1 needs.
type Input struct {
	// Manifest is the frozen pre-registration.
	Manifest manifest.Manifest
	// Model is the stored model as it is, scored never trained; ParamsSHA256
	// is the SHA-256 of its stored params_json (manifest.ParamsHash), checked
	// with the model version against the manifest.
	Model        *metalabel.Model
	ParamsSHA256 string
	// Cohort is the cohort / Jev version of the evaluation; "" disables it.
	Cohort string
	Rows   []Row
}

// scored is one independent row with its reconstruction and model verdict.
type scored struct {
	row   Row
	res   eligibility.Result
	day   string
	model bool // the model takes it (valid when it reached the M stage)
}

// Run computes table 1 and the population (i) reference.
func Run(ctx context.Context, in Input) (Result, error) {
	m := in.Manifest
	if in.Model == nil {
		return Result{}, fmt.Errorf("%w: no model", ErrNotRescorable)
	}
	if err := m.CheckModel(in.Model.Version, in.ParamsSHA256); err != nil {
		return Result{}, err
	}
	plan, err := stats.PlanFrom(m, manifest.LayerLabel)
	if err != nil {
		return Result{}, err
	}
	if err := plan.Validate(); err != nil {
		return Result{}, err
	}
	ev, err := eligibility.New(m.Rules, true)
	if err != nil {
		return Result{}, err
	}
	cfg := metalabel.DecideConfig{MinEVBps: m.Rules.MinEVBps, KellyFraction: metalabel.DefaultKellyFraction}
	ic := independence.Config{Windows: m.Windows, Cohort: in.Cohort}

	var out Result
	out.Boundary = ic.Describe()
	out.Independence.ByReason = map[string]int{}
	var rows []scored
	var reference []Row
	for _, r := range in.Rows {
		c := ic.Classify(independence.Row{ID: r.ID, DecidedAt: r.DecidedAt, Cohort: r.Cohort})
		out.Independence.ByReason[c.Reason]++
		switch c.Class {
		case independence.Independent:
			out.Independence.Independent++
			res := ev.Evaluate(ctx, r.Eligibility)
			res.ID = r.ID
			rows = append(rows, scored{row: r, res: res, day: r.DecidedAt.In(marketcalendar.JST).Format("2006-01-02")})
		case independence.Reference:
			out.Independence.Reference++
			reference = append(reference, r)
		default:
			out.Independence.Excluded++
		}
	}

	for i := range rows {
		s := &rows[i]
		if s.res.Unknown != "" || s.res.QOff.Stage != 0 {
			continue
		}
		v := metalabel.Decide(in.Model, s.res.Side, s.row.Eligibility.Features, cfg)
		if rescoreFailed(v.Reason) {
			return Result{}, fmt.Errorf("%w: %s", ErrNotRescorable, v.Reason)
		}
		s.model = v.Action == repo.ActionTake
	}
	out.Reference, err = rescoreReference(in.Model, cfg, reference)
	if err != nil {
		return Result{}, err
	}
	out.build(rows, plan)
	return out, nil
}

func rescoreFailed(reason string) bool {
	return reason == metalabel.ReasonNoModel || reason == metalabel.ReasonModelUnusable || reason == metalabel.ReasonInvalidConfig
}

// takes reports whether cell c takes the candidate: it passed every stage before
// M with c's Q setting and, with M on, the model takes it.
func takes(c metaablation.Cell, s scored) bool {
	return s.res.Unknown == "" && !s.res.Reversion && s.res.Outcome(c.QOn).Stage == 0 && (!c.MOn || s.model)
}

// value is the row's net bps in cell c: the label when taken, 0 when skipped.
func value(c metaablation.Cell, s scored) float64 {
	if takes(c, s) {
		return s.row.NetReturnBps
	}
	return 0
}

// build fills the cells, the comparisons and the reversion counts.
func (out *Result) build(rows []scored, plan stats.Plan) {
	days := map[string]bool{}
	var eligible []scored
	for _, s := range rows {
		if s.res.Eligible() && !s.res.Reversion {
			eligible = append(eligible, s)
			days[s.day] = true
		}
		if s.res.Reversion && s.res.Unknown == "" {
			out.Reversion.Rows++
			if s.res.QOff.Stage == 0 {
				out.Reversion.ReachedMeta++
				if s.model {
					out.Reversion.ModelTakes++
				} else {
					out.Reversion.ModelSkips++
				}
			}
		}
	}
	for d := range days {
		out.Days = append(out.Days, d)
	}
	sort.Strings(out.Days)
	for _, c := range metaablation.Cells() {
		out.Cells = append(out.Cells, cellResult(c, rows, eligible, out.Days))
	}
	for _, cmp := range metaablation.Comparisons() {
		cr := compare(plan, cmp, eligible, out.Days)
		out.Comparisons = append(out.Comparisons, cr)
		if cmp.Primary {
			out.PrimaryComparisons++
		}
	}
	if unknown := out.Cells[0].Funnel.Unknown; unknown > 0 {
		out.PopulationLimit = fmt.Sprintf("%d independent rows could not be recomputed (reasons: %v) and are excluded from all four cells; results describe the recomputable rows only", unknown, out.Cells[0].Funnel.UnknownReasonList())
	}
}

func errNotRescorable(reason string) error { return fmt.Errorf("%w: %s", ErrNotRescorable, reason) }
