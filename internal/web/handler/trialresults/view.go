package trialresults

import (
	"github.com/ousiassllc/pitha-trador/internal/domain/jevtrial"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query/trialquery"
	"github.com/ousiassllc/pitha-trador/internal/web/labels"
	"github.com/ousiassllc/pitha-trador/internal/web/organisms/trialresults"
)

// totalLabel names the all-arms row of the cost table.
const totalLabel = "合計"

// fill maps the trialquery results onto the panel's plain props. Nothing is
// recomputed: a rate is the aggregation's own (nil stays "no value"), an
// unknown cost stays a call count, and there is no rank, winner or vote.
func fill(props *trialresults.Props, c trialquery.Comparison, cost trialquery.CostReport) {
	props.CohortSize = c.Cohort.Size
	props.Arms, props.SkipReasons = armRows(c)
	props.Cost = costRows(cost)
	props.Proxy = proxyRows(c.CounterfactualProxy)
	props.Empty = len(props.Arms) == 0 && cost.Total.Calls == 0
}

// armRows is every compared arm (period rows: the rows the missing rate counts)
// and then the arms that only have skips, with the skips of each and their
// reasons.
func armRows(c trialquery.Comparison) ([]trialresults.Arm, []trialresults.SkipRow) {
	skipped := map[string]trialquery.ArmSkips{}
	skipArms := append(append([]trialquery.ArmSkips{}, c.Fairness.Skipped.PerArm...), c.Fairness.Skipped.ArmsWithoutRows...)
	for _, s := range skipArms {
		skipped[s.ArmID] = s
	}
	var arms []trialresults.Arm
	for _, a := range c.PerArm {
		rows := a.PeriodRows
		arms = append(arms, trialresults.Arm{
			ID: a.ArmID, Calls: rows.Total, Missing: rows.Failed + rows.Refused + rows.Partial,
			MissingRate: rows.MissingRate, Skipped: skipped[a.ArmID].Skipped,
		})
	}
	for _, s := range c.Fairness.Skipped.ArmsWithoutRows {
		arms = append(arms, trialresults.Arm{ID: s.ArmID, Skipped: s.Skipped})
	}
	var reasons []trialresults.SkipRow
	for _, s := range skipArms {
		for _, reason := range jevtrial.SkipReasons {
			if n := s.ByReason[reason]; n > 0 {
				reasons = append(reasons, trialresults.SkipRow{ArmID: s.ArmID, Reason: labels.TrialSkipReason(reason), Count: n})
			}
		}
	}
	return arms, reasons
}

// costRows is each arm's cost by source, then the total row.
func costRows(cost trialquery.CostReport) []trialresults.CostRow {
	if cost.Total.Calls == 0 {
		return nil
	}
	rows := make([]trialresults.CostRow, 0, len(cost.PerArm)+1)
	for _, a := range cost.PerArm {
		rows = append(rows, costRow(a.ArmID, false, a.CostBlock))
	}
	return append(rows, costRow(totalLabel, true, cost.Total))
}

func costRow(label string, total bool, b trialquery.CostBlock) trialresults.CostRow {
	return trialresults.CostRow{
		Label: label, Total: total,
		Reported:     trialresults.SourceCost{Calls: b.Reported.Calls, USD: b.Reported.USD},
		Estimate:     trialresults.SourceCost{Calls: b.ListPriceEstimate.Calls, USD: b.ListPriceEstimate.USD},
		UnknownCalls: b.Unknown.Calls,
	}
}

// proxyRows is the counterfactual_proxy block (always proxy: no realized outcome).
func proxyRows(p trialquery.Proxy) []trialresults.ProxyRow {
	rows := make([]trialresults.ProxyRow, len(p.Arms))
	for i, a := range p.Arms {
		row := trialresults.ProxyRow{ArmID: a.ArmID, NoneAnswers: a.NoneAnswers, NoAnswer: a.NoAnswer}
		for _, h := range a.Horizons {
			row.Horizons = append(row.Horizons, trialresults.ProxyHorizon{
				Minutes: h.HorizonMinutes, Labelled: h.Labelled, Coverage: h.Coverage,
				TP: h.BarrierHit["tp"], SL: h.BarrierHit["sl"], Timeout: h.BarrierHit["timeout"], AvgNetBps: h.AvgNetReturnBps,
			})
		}
		rows[i] = row
	}
	return rows
}
