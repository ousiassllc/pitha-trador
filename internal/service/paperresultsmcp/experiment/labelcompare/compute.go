package labelcompare

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/experiment"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// Input is what Compute needs of the trade-based comparison it sits next to:
// the very windows of experiment_compare (experiment.Experiment.Windows), the
// experiment-level verdict that withholds judgement (Withheld: "", or the
// experiment's aborted / confounded / switch_unconfirmed state, taken from the
// same Comparison so the confounder and switch checks are not run twice) and
// the two windows' risk rejections that Compare already read (nil when not).
type Input struct {
	Before, After query.Range
	Withheld      string
	Rejections    [2]*query.RiskRejections // before, after
}

// InputOf builds the Input of a Comparison made over the windows before / after.
func InputOf(c experiment.Comparison, before, after query.Range) Input {
	in := Input{Before: before, After: after, Rejections: [2]*query.RiskRejections{c.Before.RiskRejections, c.After.RiskRejections}}
	switch {
	case c.Experiment.Status == experiment.StatusAborted:
		in.Withheld = VerdictAborted
	case c.Confounded:
		in.Withheld = VerdictConfounded
	case !c.Switch.Confirmed():
		in.Withheld = VerdictSwitchUnconfirmed
	}
	return in
}

// Compute is the label-based comparison of in. Only a cancelled or expired
// ctx is returned as an error; any other failure of the reads is the Result's
// Error (limit_exceeded or failed), so the trade-based answer it accompanies is
// never lost to it.
func Compute(ctx context.Context, q query.Q, in Input) (Result, error) {
	res := Result{Caution: Caution, Before: newWindow(in.Before), After: newWindow(in.After)}
	res.Before.Skips, res.After.Skips = skipsOf(in.Rejections[0]), skipsOf(in.Rejections[1])
	if err := res.read(ctx, q, in); err != nil {
		if ctx.Err() != nil || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return Result{}, err
		}
		res.Error = ErrorFailed
		if errors.Is(err, query.ErrLimitExceeded) {
			res.Error = ErrorLimitExceeded
		}
		res.Before.Decisions, res.After.Decisions, res.Before.Days, res.After.Days = Decisions{}, Decisions{}, 0, 0
		res.DecisionLabels, res.ResearchLabels, res.DaysNeeded = nil, nil, nil
		res.LabelVerdict, res.LabelJudged = VerdictNotComputable, false
	}
	return res, nil
}

func newWindow(r query.Range) Window {
	return Window{From: r.From.UTC().Format(time.RFC3339), To: r.To.UTC().Format(time.RFC3339)}
}

// read fills in the figures and the verdict.
func (res *Result) read(ctx context.Context, q query.Q, in Input) error {
	for _, r := range []query.Range{in.Before, in.After} {
		if r.To.Sub(r.From) > MaxWindowDays*24*time.Hour {
			return query.ErrLimitExceeded
		}
	}
	var (
		labels [2]storedLabel
		err    error
	)
	for i, r := range []query.Range{in.Before, in.After} {
		w := &res.Before
		if i == 1 {
			w = &res.After
		}
		if w.Decisions, err = decisionCounts(ctx, q, r); err != nil {
			return err
		}
		if labels[i], err = storedLabels(ctx, q, r); err != nil {
			return err
		}
	}
	horizon, ok := dominantHorizon(labels)
	dl := &DecisionLabels{HorizonMinutes: horizon}
	var side [2]map[string]series
	for i := range labels {
		side[i] = labels[i][horizon]
		for h := range labels[i] {
			if h != horizon {
				dl.IgnoredRows += labels[i].rows(h)
			}
		}
	}
	all := [2]series{{}, {}}
	for i := range side {
		for _, s := range side[i] {
			all[i].merge(s)
		}
	}
	dl.Long = compare(side[0][domain.JevDirectionLong], side[1][domain.JevDirectionLong])
	dl.Short = compare(side[0][domain.JevDirectionShort], side[1][domain.JevDirectionShort])
	dl.All = compare(all[0], all[1])
	if !ok {
		dl.HorizonMinutes = 0 // no stored label in either window
	}
	res.DecisionLabels = dl
	res.Before.Days, res.After.Days = dl.All.Before.Days, dl.All.After.Days

	if res.ResearchLabels, err = researchCompare(ctx, q, in); err != nil {
		return err
	}
	res.DaysNeeded = needed(all, dl.All)
	res.LabelVerdict = labelVerdict(in.Withheld, dl.All, res.DaysNeeded)
	res.LabelJudged = judged(res.LabelVerdict)
	return nil
}

// dominantHorizon is the stored label horizon with the most labeled decisions
// over both windows (the smaller one on a tie); ok is false with none.
func dominantHorizon(labels [2]storedLabel) (horizon int, ok bool) {
	counts := map[int]int{}
	for _, l := range labels {
		for h := range l {
			counts[h] += l.rows(h)
		}
	}
	for h, n := range counts {
		if !ok || n > counts[horizon] || (n == counts[horizon] && h < horizon) {
			horizon, ok = h, true
		}
	}
	return horizon, ok
}

// researchCompare is the research labels of both windows per horizon and side.
func researchCompare(ctx context.Context, q query.Q, in Input) ([]ResearchHorizon, error) {
	before, err := researchLabels(ctx, q, in.Before)
	if err != nil {
		return nil, err
	}
	after, err := researchLabels(ctx, q, in.After)
	if err != nil {
		return nil, err
	}
	out := make([]ResearchHorizon, 0, len(horizons()))
	for _, h := range horizons() {
		out = append(out, ResearchHorizon{
			Horizon: h,
			Long:    compare(before[h][domain.JevDirectionLong], after[h][domain.JevDirectionLong]),
			Short:   compare(before[h][domain.JevDirectionShort], after[h][domain.JevDirectionShort]),
		})
	}
	return out, nil
}

// skipsOf summarises one window's rejections: the Policy Engine's skips and
// the Risk Engine's rejections of the window's trade signals (the figures
// risk_rejections gives, orders proposals' signals included) and the most
// frequent fixed reason codes. other_reject_breakdown holds the jev_none rows
// twice (by regime and by confidence band); only the regime view is used, so
// the codes add up to the skip count once.
func skipsOf(r *query.RiskRejections) *Skips {
	if r == nil {
		return nil
	}
	s := &Skips{RiskPassedCount: r.RiskPassedCount, SkipCount: r.OtherRejectCount, TopReasons: []query.RejectBucket{}}
	var reasons []query.RejectBucket
	for _, b := range r.Buckets {
		s.SkipCount += b.Count
		reasons = append(reasons, query.RejectBucket{Reason: "risk_engine_rejected:" + b.Reason, Count: b.Count})
	}
	for _, b := range r.OtherRejectBreakdown {
		if !strings.HasPrefix(b.Reason, "jev_none:conf=") {
			reasons = append(reasons, b)
		}
	}
	slices.SortFunc(reasons, func(x, y query.RejectBucket) int { return cmp.Or(y.Count-x.Count, strings.Compare(x.Reason, y.Reason)) })
	s.TopReasons = append(s.TopReasons, reasons[:min(len(reasons), TopReasons)]...)
	if total := s.SkipCount + s.RiskPassedCount; total > 0 {
		rate := float64(s.SkipCount) / float64(total)
		s.SkipRate = &rate
	}
	return s
}
