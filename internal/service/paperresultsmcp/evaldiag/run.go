package evaldiag

import (
	"cmp"
	"context"
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/barriersettings"
	"github.com/ousiassllc/pitha-trador/internal/config/costsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/calibration"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// legs are the triple-barrier widths of each side with the cost settings of
// that side applied: what the Labeler would measure a LONG and a SHORT with now.
type legs struct{ long, short barriersettings.Barriers }

// loadLegs reads the barrier and cost settings (read-only) and validates both
// sides the way the Labeler does before it writes a label.
func loadLegs(ctx context.Context, q query.Q) (base barriersettings.Barriers, l legs, err error) {
	store := settingsReader{q}
	if base, err = barriersettings.Load(ctx, store); err != nil {
		return base, l, fmt.Errorf("evaldiag: %w", err)
	}
	for direction, dst := range map[string]*barriersettings.Barriers{domain.JevDirectionLong: &l.long, domain.JevDirectionShort: &l.short} {
		if *dst, err = costsettings.LabelBarriers(ctx, store, base, direction); err != nil {
			return base, l, fmt.Errorf("evaldiag: %w", err)
		}
		if err = dst.Validate(); err != nil {
			return base, l, fmt.Errorf("evaldiag: effective triple-barrier settings are invalid: %w", err)
		}
	}
	return base, l, nil
}

// cutoffOf reads the LLM training cutoff. start is the JST start of the cutoff
// date, the first instant a decision counts; applied is false (start is the zero
// time) while the cutoff is the placeholder, when nothing is dropped.
func cutoffOf(ctx context.Context, q query.Q) (Cutoff, time.Time, error) {
	s, err := metalabelsettings.Load(ctx, settingsReader{q})
	if err != nil {
		return Cutoff{}, time.Time{}, fmt.Errorf("evaldiag: %w", err)
	}
	c := Cutoff{Date: s.LLMCutoff, Status: s.CutoffStatus(), Model: s.LLMCutoffModel}
	if c.Status == metalabelsettings.CutoffPlaceholder {
		return c, time.Time{}, nil
	}
	day, err := time.ParseInLocation(metalabelsettings.LLMCutoffLayout, s.LLMCutoff, query.JST)
	if err != nil {
		return Cutoff{}, time.Time{}, fmt.Errorf("evaldiag: %s %q is not a date: %w", metalabelsettings.KeyLLMCutoff, s.LLMCutoff, err)
	}
	c.Applied = true
	return c, day, nil
}

// Run evaluates every trader LONG/SHORT decision of [r.From, r.To) against the
// baselines and returns the data and the number of decisions read. It reads
// only; the clock is not used (the signature matches the other analyses).
func Run(ctx context.Context, q query.Q, r query.Range, _ time.Time, req Request) (any, int, error) {
	base, l, err := loadLegs(ctx, q)
	if err != nil {
		return nil, 0, err
	}
	horizon := base.TimeoutMinutes()
	if req.HorizonMinutes != nil {
		horizon = *req.HorizonMinutes
	}
	cutoff, cutoffStart, err := cutoffOf(ctx, q)
	if err != nil {
		return nil, 0, err
	}
	from, to := r.Bounds()
	decisions, err := listDecisions(ctx, q, from, to, horizon, req)
	if err != nil {
		return nil, 0, err
	}

	bars, err := openBars(ctx, q, from, to, horizon, req)
	if err != nil {
		return nil, 0, err
	}
	defer bars.close()
	accs := map[cohortKey]*cohortAcc{}
	var order []*cohortAcc
	for _, d := range decisions {
		key := cohortKey{d.modelID, d.version}
		acc := accs[key]
		if acc == nil {
			if len(accs) >= MaxCohorts {
				return nil, 0, query.ErrLimitExceeded
			}
			acc = &cohortAcc{key: key, firstAt: d.at, reasons: map[string]int{}}
			accs[key], order = acc, append(order, acc)
		}
		acc.lastAt = d.at
		acc.decisions++
		acc.storedAll.add(d.storedNet)
		window, err := bars.window(d.id) // every decision, in list order, even one that is then left out
		if err != nil {
			return nil, 0, err
		}
		if cutoff.Applied && d.at.Before(cutoffStart) {
			acc.beforeCutoff++
			continue
		}
		row, reason := labelDecision(d, window, horizon, l)
		if reason != "" {
			acc.reasons[reason]++
			continue
		}
		acc.rows = append(acc.rows, row)
		acc.storedEvaluated.add(d.storedNet)
		if d.storedNet != nil {
			acc.recomputedSum += row.net[iLLM]
			acc.recomputedLabels++
			acc.maxAbsDiff = max(acc.maxAbsDiff, math.Abs(row.net[iLLM]-*d.storedNet))
		}
	}
	slices.SortStableFunc(order, func(a, b *cohortAcc) int {
		return cmp.Or(a.firstAt.Compare(b.firstAt), cmp.Compare(a.key.model, b.key.model), cmp.Compare(a.key.version, b.key.version))
	})

	out := Data{
		From: r.From.In(query.JST).Format(time.DateOnly), To: r.To.In(query.JST).Format(time.DateOnly),
		HorizonMinutes: horizon, PromptVersion: req.PromptVersion, ModelID: req.ModelID, Notice: notice,
		Decisions: len(decisions), LLMCutoff: cutoff,
		LabelAssumptions: LabelAssumptions{
			LabelVersion: domain.CalibrationLabelVersionTripleBarrier, HorizonMinutes: horizon,
			TakeProfitBps: base.TakeProfitBps, StopLossBps: base.StopLossBps, BarrierCostBps: base.CostBps,
			LabelCostBps: CostBps{Long: l.long.CostBps, Short: l.short.CostBps}, Settings: settingsText,
		},
		Statistics:  Statistics{Method: statisticsText, Alpha: Alpha, Power: Power, Resamples: Resamples, Seed: Seed, Targets: TargetsBps},
		Cohorts:     []Cohort{},
		Definitions: definitions(),
		Caveats:     caveats(cutoff),
	}
	for _, acc := range order {
		out.Cohorts = append(out.Cohorts, acc.cohort(horizon))
	}
	return out, len(decisions), nil
}

// labelDecision labels one decision on every arm with the Labeler's own
// definition (calibration.LabelWindow) over window, the bars after its response.
// It returns the exclusion reason (and no row) when the decision cannot be
// labeled on the LLM arm and both legs, or has no bar of its own for the momentum
// signal: every arm is then measured on the same decisions.
func labelDecision(d decisionRow, window []domain.Snapshot, horizon int, l legs) (evalRow, string) {
	nets := map[string]float64{}
	for _, side := range []struct {
		direction string
		barriers  barriersettings.Barriers
		prefix    string
	}{
		{d.direction, l.forDirection(d.direction), PrefixLLM},
		{oppositeOf(d.direction), l.forDirection(oppositeOf(d.direction)), PrefixOpposite},
	} {
		outcome, gap := calibration.LabelWindow(window, d.responseAt, side.direction, horizon, side.barriers)
		if gap != "" {
			kind := KindWindowShort
			if idx, _ := calibration.FindEntry(window, side.direction, d.responseAt); idx < 0 {
				kind = KindNoEntry
			}
			return evalRow{}, side.prefix + kind
		}
		nets[side.direction] = *outcome.NetReturnBps
	}
	if !d.hasBar {
		return evalRow{}, ReasonNoBarMomentum
	}

	long, short := nets[domain.JevDirectionLong], nets[domain.JevDirectionShort]
	row := evalRow{
		day: d.at.In(query.JST).Format(time.DateOnly), dir: d.direction, entered: d.entered, stored: d.storedNet,
	}
	row.net[iLLM] = nets[d.direction]
	row.net[iRandom] = (long + short) / 2
	row.net[iNone] = 0
	row.net[iLong], row.net[iShort] = long, short
	if d.return5m != nil {
		switch {
		case *d.return5m > 0:
			row.net[iMomentum], row.net[iContrarian] = long, short
		case *d.return5m < 0:
			row.net[iMomentum], row.net[iContrarian] = short, long
		}
	}
	return row, ""
}

func (l legs) forDirection(direction string) barriersettings.Barriers {
	if direction == domain.JevDirectionShort {
		return l.short
	}
	return l.long
}

func oppositeOf(direction string) string {
	if direction == domain.JevDirectionLong {
		return domain.JevDirectionShort
	}
	return domain.JevDirectionLong
}
