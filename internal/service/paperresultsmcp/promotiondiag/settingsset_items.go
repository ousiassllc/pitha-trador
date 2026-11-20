package promotiondiag

import (
	"math"
	"slices"

	"gonum.org/v1/gonum/stat/distuv"

	"github.com/ousiassllc/pitha-trador/internal/config/promotionsettings"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/experiment"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// itemOrder is the fixed order of the eleven items.
var itemOrder = []string{
	ItemSameRows, ItemCommonRows, ItemCommonDays, ItemDiffCILower, ItemMultipleCompare, ItemPositiveDays,
	ItemNotOneDayWins, ItemMaxDrawdown, ItemAfterAICost, ItemAfterCutoff, ItemModelVersionStable,
}

// notApplicable is an item that cannot be judged for an experiment: unknown, flagged, never a fail.
func notApplicable(name, op string, required any, detail string) Item {
	return Item{Name: name, Status: StatusUnknown, Comparator: op, Required: required, Detail: detail, NotApplicable: true}
}

// settingsSetItems runs the eleven items over an experiment's before / after comparison (docs:
// paper-results-mcp-promotion.md §6.3). before and after are the per-JST-day pnl of the two periods' trades,
// family the size of the multiple-comparison family. The criteria are the ones in force, as for any candidate.
//
// The items that read the experiment's interval or performance (4 to 8) are unknown when experiment_compare
// withheld its judgement (aborted, confounded, switch_unconfirmed, insufficient_trades, not_computable): its
// numbers are then returned but not judged, as the experiment's own tool does.
func settingsSetItems(c experiment.Comparison, before, after []query.Day, family int, crit promotionsettings.Criteria) ([]Item, *ExperimentNumbers) {
	t := c.PerTradePnl
	n := &ExperimentNumbers{
		BeforeTrades: c.Before.Performance.ClosedTradeCount, AfterTrades: c.After.Performance.ClosedTradeCount,
		BeforeDays: len(before), AfterDays: len(after),
		BeforeMeanJPY: t.Before.MeanJPY, AfterMeanJPY: t.After.MeanJPY, DiffMeanJPY: t.Diff, DiffCILowerJPY: t.CILow, DiffCIUpperJPY: t.CIHigh,
		FamilySize:           family,
		BeforeMaxDrawdownJPY: c.Before.Performance.MaxDrawdownJPY, AfterMaxDrawdownJPY: c.After.Performance.MaxDrawdownJPY,
	}
	if t.T != nil && t.DF != nil {
		p := 2 * distuv.StudentsT{Mu: 0, Sigma: 1, Nu: *t.DF}.CDF(-math.Abs(*t.T))
		adj := min(1, p*float64(family)) // Holm: the experiment's p is the smallest of the family, the others count as 1
		n.PValue, n.HolmAdjustedP = &p, &adj
	}
	withheld := ""
	if !c.Judged {
		withheld = detailExperimentPrefix + c.Verdict
	}
	// judged turns an item that reads the experiment's judgement into unknown when it was withheld.
	judged := func(it Item) Item {
		if withheld != "" {
			it.Status, it.Detail = StatusUnknown, withheld
		}
		return it
	}

	items := make([]Item, 0, len(itemOrder))
	// 1 same rows and period: before and after are different trades.
	items = append(items, notApplicable(ItemSameRows, ">=", MinCoverage, DetailUnpairedPeriods))
	// 2, 3 enough trades and days in the thinner of the two periods.
	items = append(items,
		compare(ItemCommonRows, ">=", f(float64(min(n.BeforeTrades, n.AfterTrades))), float64(crit.MinCommonRows), ""),
		compare(ItemCommonDays, ">=", f(float64(min(n.BeforeDays, n.AfterDays))), float64(crit.MinDays), ""))
	// 4 the lower end of the experiment's 95% interval of the mean pnl per trade (yen), above 0.
	diff := judged(compare(ItemDiffCILower, ">", t.CILow, 0, DetailCIUndefined))
	if withheld == "" && crit.MinDiffCILowerBps != 0 {
		diff.Status, diff.Detail, diff.Required = StatusUnknown, DetailCriterionUnit, crit.MinDiffCILowerBps
	}
	items = append(items, diff)
	// 5 the Holm-adjusted p.
	items = append(items, judged(compare(ItemMultipleCompare, "<=", n.HolmAdjustedP, crit.MaxAdjustedP, DetailPUndefined)))
	// 6, 7 the days of the after period against the before period.
	pos, rest := dayItems(n, before, after, crit)
	items = append(items, judged(pos), judged(rest))
	// 8 the after period's maximum drawdown is no worse than the before period's.
	dd := compare(ItemMaxDrawdown, "<=", f(n.AfterMaxDrawdownJPY), n.BeforeMaxDrawdownJPY, "")
	if dd.Status == StatusFail {
		dd.Detail = DetailWorseDrawdown
	}
	items = append(items, judged(dd))
	// 9, 10 an experiment's trades carry no AI cost and are not selected by an LLM cutoff.
	items = append(items, notApplicable(ItemAfterAICost, ">", 0.0, DetailNoAICost), notApplicable(ItemAfterCutoff, "==", "applied", DetailNoCutoffFilter))
	// 11 no other Jev model or prompt version changed over the two periods (the experiment's own variable is exempt).
	items = append(items, modelOrPromptItem(c))
	return items, n
}

// dayItems are positive_days and not_one_day_wins, from the days with a closed trade.
func dayItems(n *ExperimentNumbers, before, after []query.Day, crit promotionsettings.Criteria) (positive, rest Item) {
	positive = compare(ItemPositiveDays, ">=", nil, crit.MinPositiveDayPct, DetailNoTradeDays)
	rest = compare(ItemNotOneDayWins, ">", nil, 0, DetailNoTradeDays)
	if len(before) == 0 || len(after) == 0 {
		return positive, rest
	}
	beforeTotal, afterTotal, best := 0.0, 0.0, math.Inf(-1)
	for _, d := range before {
		beforeTotal += d.PnlJPY
	}
	for _, d := range after {
		afterTotal += d.PnlJPY
		best = max(best, d.PnlJPY)
	}
	mean := beforeTotal / float64(len(before))
	wins := len(slices.DeleteFunc(slices.Clone(after), func(d query.Day) bool { return d.PnlJPY <= mean }))
	share := 100 * float64(wins) / float64(len(after))
	remaining := afterTotal - best - beforeTotal
	n.BeforeMeanDayJPY, n.PositiveDayPct, n.RestWithoutBestDay = &mean, &share, &remaining
	positive = compare(ItemPositiveDays, ">=", &share, crit.MinPositiveDayPct, DetailNoTradeDays)
	rest = compare(ItemNotOneDayWins, ">", &remaining, 0, DetailNoTradeDays)
	if rest.Status == StatusFail {
		rest.Detail = DetailOneDayCarries
	}
	return positive, rest
}

// modelOrPromptItem fails when experiment_compare found a Jev model or prompt version that changed besides
// the experiment's own variable (its confounders of kind model / prompt).
func modelOrPromptItem(c experiment.Comparison) Item {
	changed := 0
	for _, f := range c.Confounders {
		if f.Kind == experiment.KindModel || f.Kind == experiment.KindPrompt {
			changed++
		}
	}
	it := Item{Name: ItemModelVersionStable, Status: StatusPass, Comparator: "==", Actual: float64(changed), Required: 0.0, Detail: DetailOK}
	if changed > 0 {
		it.Status, it.Detail = StatusFail, DetailModelOrPromptChanged
	}
	return it
}
