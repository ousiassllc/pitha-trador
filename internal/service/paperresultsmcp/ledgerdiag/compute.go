package ledgerdiag

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/stats"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/evaldiag"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// ErrUnsupportedReference is returned when the reference is a live_cohort: another cohort judged
// other decisions, so nothing can be paired with it row by row.
var ErrUnsupportedReference = errors.New("ledgerdiag: a live_cohort cannot be the reference: it judged different decisions")

// Compute lines the candidates up against the reference on the same period and rows. It reads only.
// The result's Candidates carry, besides the response fields, the paired series the promotion
// checklist uses (json "-").
func Compute(ctx context.Context, q query.Q, r query.Range, req Request) (*Ledger, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	ds, err := load(ctx, q, r, req)
	if err != nil {
		return nil, err
	}
	set, err := loadSources(ctx, q, r, ds, req)
	if err != nil {
		return nil, err
	}
	ref := set.get(req.reference(), "", "")
	if kind, key, _ := ParseID(req.reference()); ref.kind == "" {
		ref.kind, ref.key = kind, key
	}
	if ref.kind == KindLiveCohort {
		return nil, ErrUnsupportedReference
	}
	cands := set.candidates(req)
	if len(cands) > MaxCandidates {
		return nil, query.ErrLimitExceeded
	}

	l := &Ledger{
		From: r.From.In(query.JST).Format(time.DateOnly), To: r.To.In(query.JST).Format(time.DateOnly),
		Horizon: req.horizon(), Alignment: req.alignment(), Notice: notice, LLMCutoff: ds.cutoff,
		Exclusions:  ds.excl,
		Statistics:  statistics(),
		Candidates:  []Entry{},
		Definitions: definitions(), Caveats: caveats(ds.cutoff),
	}
	refRows := set.refRows
	l.Reference = referenceOf(req, ds, ref, refRows)

	var common []decRow
	if req.alignment() == AlignCommonAll {
		common = commonRows(refRows, ref, cands)
		n := len(common)
		l.Reference.CommonRows = &n
	}
	for _, s := range cands {
		l.Candidates = append(l.Candidates, entryOf(req, s, ref, refRows, common))
	}
	applyHolm(l, req.FamilyExtra)
	return l, nil
}

// commonRows are the reference rows the reference and every candidate that answered anything answered. A
// candidate with no answer at all is listed (no_answers) but left out of the intersection, so it does not
// empty the table for the others; a live_cohort judged other decisions and has no row to intersect.
func commonRows(refRows []decRow, ref *source, cands []*source) []decRow {
	var out []decRow
rows:
	for _, d := range refRows {
		if _, ok := ref.answered(d); !ok {
			continue
		}
		for _, s := range cands {
			if s.kind == KindLiveCohort || !hasAnswers(s) {
				continue
			}
			if _, ok := s.answered(d); !ok {
				continue rows
			}
		}
		out = append(out, d)
	}
	return out
}

func hasAnswers(s *source) bool { return s.baseline != "" || len(s.answers) > 0 }

// pairRows are the reference rows both the reference and s answered (pairwise alignment).
func pairRows(refRows []decRow, ref, s *source) []decRow {
	var out []decRow
	for _, d := range refRows {
		if _, ok := ref.answered(d); !ok {
			continue
		}
		if _, ok := s.answered(d); ok {
			out = append(out, d)
		}
	}
	return out
}

// seriesOf is the candidate's and the reference's net on rows (time order).
func seriesOf(rows []decRow, ref, s *source) []PairRow {
	out := make([]PairRow, 0, len(rows))
	for _, d := range rows {
		ra, _ := ref.answered(d)
		sa, _ := s.answered(d)
		out = append(out, PairRow{DecisionID: d.ID, At: d.At, Day: d.Day, Net: s.net(d, sa), Ref: ref.net(d, ra)})
	}
	return out
}

func points(rows []PairRow, value func(PairRow) float64) []evaldiag.Point {
	out := make([]evaldiag.Point, len(rows))
	for i, p := range rows {
		out[i] = evaldiag.Point{Day: p.Day, Value: value(p)}
	}
	return out
}

// meanOf is the plain mean of value over rows (nil without rows): the same number Summarize reports as its
// mean, without the bootstrap interval.
func meanOf(rows []PairRow, value func(PairRow) float64) *float64 {
	if len(rows) == 0 {
		return nil
	}
	var sum float64
	for _, p := range rows {
		sum += value(p)
	}
	m := sum / float64(len(rows))
	return &m
}

func daysOf(rows []PairRow) int {
	seen := map[string]bool{}
	for _, p := range rows {
		seen[p.Day] = true
	}
	return len(seen)
}

func answerOfSource(s *source) func(decRow) answer {
	return func(d decRow) answer {
		a, _ := s.answered(d)
		return a
	}
}

// referenceOf is the reference block: its own mean over every row it answered in the period.
func referenceOf(req Request, ds *dataset, ref *source, refRows []decRow) Reference {
	out := Reference{ID: req.reference(), PromptVersion: ds.liveVersion, ModelIDFilter: req.ModelID, ModelIDs: ref.modelIDs()}
	out.MultipleModelID = len(out.ModelIDs) > 1
	var rows []decRow
	var pts []evaldiag.Point
	for _, d := range refRows {
		if a, ok := ref.answered(d); ok {
			rows = append(rows, d)
			pts = append(pts, evaldiag.Point{Day: d.Day, Value: ref.net(d, a)})
		}
	}
	out.Rows = len(refRows)
	if req.MeansOnly {
		days, sum := map[string]bool{}, 0.0
		for _, p := range pts {
			days[p.Day], sum = true, sum+p.Value
		}
		out.Days = len(days)
		if len(pts) > 0 {
			m := sum / float64(len(pts))
			out.MeanNetBps = &m
		}
	} else {
		sum := evaldiag.Summarize(pts, false)
		out.Days, out.MeanNetBps, out.CI = sum.Days, sum.MeanNetBps, sum.CI
	}
	out.AICost = costOf(rows, answerOfSource(ref))
	return out
}

// entryOf is one candidate's column.
func entryOf(req Request, s, ref *source, refRows []decRow, common []decRow) Entry {
	e := Entry{ID: s.id, Kind: s.kind, DistinctModelIDs: s.modelIDs(), AICost: Cost{BySource: []CostSource{}, Notes: []string{}}, RefCost: Cost{BySource: []CostSource{}, Notes: []string{}}}
	if s.kind == KindLiveCohort {
		return cohortEntry(req, e, s)
	}
	e.NoAnswers = !hasAnswers(s)
	e.NAnswered = len(s.answers)
	if s.baseline != "" {
		e.NAnswered = len(refRows)
	}
	rows := common
	if req.alignment() == AlignPairwise {
		rows = pairRows(refRows, ref, s)
	}
	if e.NoAnswers {
		rows = nil
	}
	n := len(rows)
	e.NCommon = &n
	for _, d := range refRows {
		if _, ok := s.answered(d); ok {
			continue
		}
		if s.failed[d.ID] {
			e.Missing.Failed++
		} else {
			e.Missing.Absent++
		}
	}
	if len(refRows) > 0 {
		e.Missing.OutsideCommon = len(refRows) - n - e.Missing.Absent - e.Missing.Failed
		rate := 1 - float64(n)/float64(len(refRows))
		e.MissingRate = &rate
	}

	e.Series = seriesOf(rows, ref, s)
	if req.MeansOnly {
		e.Days, e.MeanNetBps, e.LiveMeanOnCommonRows = daysOf(e.Series), meanOf(e.Series, func(p PairRow) float64 { return p.Net }), meanOf(e.Series, func(p PairRow) float64 { return p.Ref })
	} else {
		net := evaldiag.Summarize(points(e.Series, func(p PairRow) float64 { return p.Net }), false)
		e.Days, e.MeanNetBps, e.CI = net.Days, net.MeanNetBps, net.CI
		e.LiveMeanOnCommonRows = meanOf(e.Series, func(p PairRow) float64 { return p.Ref })
	}
	e.DiffSummary = evaldiag.Summarize(points(e.Series, func(p PairRow) float64 { return p.Net - p.Ref }), false)
	e.DiffVsLive = &Diff{
		N: e.DiffSummary.N, Days: e.DiffSummary.Days, MeanBps: e.DiffSummary.MeanNetBps, CI: e.DiffSummary.CI,
		DetectableMinimumBps: e.DiffSummary.DetectableMinimumEdgeBps, PValue: e.DiffSummary.P,
	}
	e.AICost = costOf(rows, answerOfSource(s))
	e.RefCost = costOf(rows, answerOfSource(ref))
	e.Combined = combine(req, e.MeanNetBps, e.DiffSummary.MeanNetBps, e.AICost, e.RefCost)
	return e
}

// cohortEntry is a live_cohort: its own statistics on its own rows. Its decisions are not the
// reference's, so there is no common row, no missing rate and no difference.
func cohortEntry(req Request, e Entry, s *source) Entry {
	e.NAnswered = len(s.cohort)
	e.DiffReason = "different_decisions"
	pts := make([]evaldiag.Point, len(s.cohort))
	for i, d := range s.cohort {
		pts[i] = evaldiag.Point{Day: d.Day, Value: d.own()}
	}
	sum := evaldiag.Summarize(pts, false)
	e.Days, e.MeanNetBps, e.CI = sum.Days, sum.MeanNetBps, sum.CI
	e.AICost = costOf(s.cohort, func(d decRow) answer { return liveAnswer(d) })
	e.Combined = combine(req, e.MeanNetBps, nil, e.AICost, Cost{})
	if e.Combined.Reason == "" {
		e.Combined.Reason = "reference_cost_not_applicable"
	}
	return e
}

// applyHolm sets every difference's Holm-adjusted p across the candidates that have a p. The
// family is those candidates plus extra comparisons made elsewhere, which are taken as having
// p = 1 (the least favourable: they widen the correction for every real p and never narrow it).
func applyHolm(l *Ledger, extra int) {
	var idx []int
	var ps []float64
	untested := 0 // a comparison whose p is undefined (fewer than two days) still counts, as p = 1
	for i, e := range l.Candidates {
		switch {
		case e.DiffVsLive != nil && e.DiffVsLive.PValue != nil:
			idx, ps = append(idx, i), append(ps, *e.DiffVsLive.PValue)
		case e.DiffVsLive != nil:
			untested++
		}
	}
	family := len(ps) + untested + extra
	adjusted := stats.Holm(append(slices.Clone(ps), slices.Repeat([]float64{1}, untested+extra)...))
	for k, i := range idx {
		a := adjusted[k]
		l.Candidates[i].DiffVsLive.HolmAdjustedP = &a
	}
	for i := range l.Candidates {
		if d := l.Candidates[i].DiffVsLive; d != nil {
			d.FamilySize = family
		}
	}
	l.Statistics.FamilySize = family
}
