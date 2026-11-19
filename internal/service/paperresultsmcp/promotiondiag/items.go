package promotiondiag

import (
	"slices"

	"github.com/ousiassllc/pitha-trador/internal/config/promotionsettings"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/ledgerdiag"
)

// Detail codes of an item.
const (
	DetailOK                 = "ok"
	DetailBelowRequired      = "below_required"
	DetailAboveRequired      = "above_required"
	DetailNoReferenceRows    = "no_reference_rows"
	DetailNoCommonRows       = "no_common_rows"
	DetailDifferentDecisions = "different_decisions"
	DetailCIUndefined        = "ci_undefined_fewer_than_two_days"
	DetailPUndefined         = "p_undefined"
	DetailOneDayCarries      = "total_depends_on_the_best_day"
	DetailWorseDrawdown      = "candidate_drawdown_worse_than_current"
	DetailCutoffPlaceholder  = "llm_cutoff_is_placeholder"
	DetailModelChanged       = "more_than_one_model_id"
	DetailNoModelRows        = "no_model_ids"
)

func f(v float64) *float64 { return &v }

// compare builds a threshold item: pass when got stands to want as op says.
func compare(name, op string, got *float64, want float64, undefined string) Item {
	it := Item{Name: name, Comparator: op, Required: want, Detail: DetailOK}
	if got == nil {
		it.Status, it.Detail = StatusUnknown, undefined
		return it
	}
	it.Actual = *got
	ok := false
	switch op {
	case ">=":
		ok = *got >= want
	case ">":
		ok = *got > want
	case "<=":
		ok = *got <= want
	}
	if ok {
		it.Status = StatusPass
		return it
	}
	it.Status, it.Detail = StatusFail, DetailBelowRequired
	if op == "<=" {
		it.Detail = DetailAboveRequired
	}
	return it
}

// evaluate runs the eleven items in their fixed order over the candidate's entry of the ledger.
func evaluate(l *ledgerdiag.Ledger, e *ledgerdiag.Entry, c promotionsettings.Criteria, req Request) ([]Item, KeyNumbers) {
	kn := KeyNumbers{ReferenceRows: l.Reference.Rows, FamilySize: l.Statistics.FamilySize}
	different := e.DiffVsLive == nil // a live_cohort judged other decisions
	dayDiffs := dayDiffSums(e.Series)
	n, days := 0, 0
	if e.NCommon != nil {
		n = *e.NCommon
	}
	if e.DiffVsLive != nil {
		days = e.DiffVsLive.Days
		kn.DiffMeanBps, kn.DiffCI, kn.PValue, kn.HolmAdjustedP = e.DiffVsLive.MeanBps, e.DiffVsLive.CI, e.DiffVsLive.PValue, e.DiffVsLive.HolmAdjustedP
	}
	kn.CommonRows, kn.Days, kn.MeanNetBps, kn.CurrentMeanBps = n, days, e.MeanNetBps, e.LiveMeanOnCommonRows

	items := make([]Item, 0, 11)
	add := func(it Item) {
		if different && it.Status != StatusPass && it.Detail != DetailOK && it.Name != ItemAfterCutoff && it.Name != ItemModelVersionStable {
			it.Status, it.Detail = StatusUnknown, DetailDifferentDecisions
		}
		items = append(items, it)
	}

	// 1 same rows and period: the common rows cover MinCoverage of the reference rows of the period.
	cov := compare(ItemSameRows, ">=", nil, MinCoverage, DetailNoReferenceRows)
	if l.Reference.Rows > 0 && e.NCommon != nil {
		kn.Coverage = f(float64(n) / float64(l.Reference.Rows))
		cov = compare(ItemSameRows, ">=", kn.Coverage, MinCoverage, DetailNoReferenceRows)
	}
	if l.Reference.Rows == 0 {
		cov.Status = StatusFail
	}
	add(cov)
	// 2, 3 enough rows and days.
	rows := compare(ItemCommonRows, ">=", nil, float64(c.MinCommonRows), DetailDifferentDecisions)
	if e.NCommon != nil {
		rows = compare(ItemCommonRows, ">=", f(float64(n)), float64(c.MinCommonRows), "")
	}
	add(rows)
	daysItem := compare(ItemCommonDays, ">=", nil, float64(c.MinDays), DetailDifferentDecisions)
	if e.DiffVsLive != nil {
		daysItem = compare(ItemCommonDays, ">=", f(float64(days)), float64(c.MinDays), "")
	}
	add(daysItem)
	// 4 the lower end of the day-cluster interval of the difference.
	var lower *float64
	if kn.DiffCI != nil {
		lower = &kn.DiffCI.Lower
	}
	add(compare(ItemDiffCILower, ">", lower, c.MinDiffCILowerBps, DetailCIUndefined))
	// 5 the Holm-adjusted p over the real family (widened by family_size_extra).
	add(compare(ItemMultipleCompare, "<=", kn.HolmAdjustedP, c.MaxAdjustedP, DetailPUndefined))
	// 6 the share of days the candidate beat the current one.
	var share *float64
	if len(dayDiffs) > 0 {
		pos := 0
		for _, d := range dayDiffs {
			if d > 0 {
				pos++
			}
		}
		share = f(100 * float64(pos) / float64(len(dayDiffs)))
		kn.PositiveDayPct = share
	}
	add(compare(ItemPositiveDays, ">=", share, c.MinPositiveDayPct, DetailNoCommonRows))
	// 7 the total difference without its single best day stays positive.
	var rest *float64
	if len(dayDiffs) > 0 {
		total := 0.0
		for _, d := range dayDiffs {
			total += d
		}
		rest = f(total - slices.Max(dayDiffs))
	}
	one := compare(ItemNotOneDayWins, ">", rest, 0, DetailNoCommonRows)
	if one.Status == StatusFail {
		one.Detail = DetailOneDayCarries
	}
	add(one)
	// 8 the candidate's maximum drawdown is no worse than the current's.
	add(drawdownItem(e))
	// 9 the mean difference after the AI cost difference.
	after := compare(ItemAfterAICost, ">", e.Combined.DiffAfterAICostBps, 0, e.Combined.Reason)
	if e.Combined.DiffAfterAICostBps == nil && e.Combined.Reason == "" {
		after.Detail = "no_common_rows"
	}
	add(after)
	// 10 the LLM training cutoff is confirmed (not the placeholder).
	cut := Item{Name: ItemAfterCutoff, Comparator: "==", Actual: l.LLMCutoff.Status, Required: "applied", Status: StatusPass, Detail: DetailOK}
	if !l.LLMCutoff.Applied {
		cut.Status, cut.Detail = StatusUnknown, DetailCutoffPlaceholder
	}
	add(cut)
	// 11 one model id on each side over the period.
	add(modelItem(l, e))
	return items, kn
}

// dayDiffSums are the candidate-minus-current sums per JST day of the common rows (days ascending).
func dayDiffSums(series []ledgerdiag.PairRow) []float64 {
	var days []string
	sums := map[string]float64{}
	for _, p := range series {
		if _, ok := sums[p.Day]; !ok {
			days = append(days, p.Day)
		}
		sums[p.Day] += p.Net - p.Ref
	}
	slices.Sort(days)
	out := make([]float64, len(days))
	for i, d := range days {
		out[i] = sums[d]
	}
	return out
}

// maxDrawdown is the largest fall (bps, >= 0) of the running sum of values from its running peak
// (the start at 0 counts as a peak) in the order given.
func maxDrawdown(values []float64) float64 {
	cum, peak, worst := 0.0, 0.0, 0.0
	for _, v := range values {
		cum += v
		peak = max(peak, cum)
		worst = max(worst, peak-cum)
	}
	return worst
}

func drawdownItem(e *ledgerdiag.Entry) Item {
	it := Item{Name: ItemMaxDrawdown, Comparator: "<=", Detail: DetailOK}
	if len(e.Series) == 0 {
		it.Status, it.Detail = StatusUnknown, DetailNoCommonRows
		return it
	}
	cand, cur := make([]float64, len(e.Series)), make([]float64, len(e.Series))
	for i, p := range e.Series { // time order (ledgerdiag keeps the decisions' order)
		cand[i], cur[i] = p.Net, p.Ref
	}
	it.Actual, it.Required = maxDrawdown(cand), maxDrawdown(cur)
	it.Status = StatusPass
	if it.Actual.(float64) > it.Required.(float64) {
		it.Status, it.Detail = StatusFail, DetailWorseDrawdown
	}
	return it
}

func modelItem(l *ledgerdiag.Ledger, e *ledgerdiag.Entry) Item {
	cand, cur := len(e.DistinctModelIDs), len(l.Reference.ModelIDs)
	it := Item{
		Name: ItemModelVersionStable, Comparator: "==", Detail: DetailOK,
		Actual:   map[string]int{"candidate_model_ids": cand, "current_model_ids": cur},
		Required: map[string]int{"candidate_model_ids": 1, "current_model_ids": 1},
	}
	switch {
	case cand == 0 || cur == 0:
		it.Status, it.Detail = StatusUnknown, DetailNoModelRows
	case cand > 1 || cur > 1:
		it.Status, it.Detail = StatusFail, DetailModelChanged
	default:
		it.Status = StatusPass
	}
	return it
}
