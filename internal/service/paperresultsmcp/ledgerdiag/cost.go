package ledgerdiag

import (
	"slices"
	"strings"
)

// costOf is the AI cost of a candidate over rows, by where each row's cost comes from. A row
// whose cost is unknown carries no amount (it is not counted as 0): Complete is then false and no
// per-row cost or bps conversion is derived from it.
func costOf(rows []decRow, answerOf func(decRow) answer) Cost {
	c := Cost{Rows: len(rows), BySource: []CostSource{}, Notes: []string{}}
	bySource := map[string]*CostSource{}
	var total float64
	var any bool
	worstCase, estimates := 0, 0
	for _, d := range rows {
		a := answerOf(d)
		s := bySource[a.cost]
		if s == nil {
			s = &CostSource{Source: a.cost}
			bySource[a.cost] = s
		}
		s.Rows++
		switch {
		case a.hasUSD:
			total += a.usd
			any = true
			sum := a.usd
			if s.USD != nil {
				sum += *s.USD
			}
			s.USD = &sum
			switch a.cost {
			case CostUnknown:
				worstCase++
			case CostListPrice, CostUsageEstimate:
				estimates++
			}
		default:
			c.RowsWithoutAmount++
		}
	}
	for _, s := range bySource {
		c.BySource = append(c.BySource, *s)
	}
	slices.SortFunc(c.BySource, func(a, b CostSource) int { return strings.Compare(a.Source, b.Source) })
	if any {
		c.USDTotal = &total
	}
	c.Complete = c.Rows > 0 && c.RowsWithoutAmount == 0
	if c.Complete {
		perRow := total / float64(c.Rows)
		c.USDPerRow = &perRow
	}
	if worstCase > 0 {
		c.Notes = append(c.Notes, "cost_source=unknown の行は、実費ではなく費用上限の判定に数えた最悪値側の額（counted_cost_usd）で足している。0 円ではない")
	}
	if c.RowsWithoutAmount > 0 {
		c.Notes = append(c.Notes, "費用が不明で金額が無い行は 0 円にせず合計に入れていない（complete=false。1 行あたりの費用・bps 換算は出さない）")
	}
	if estimates > 0 {
		c.Notes = append(c.Notes, "推定の費用（定価×トークン、または入力サイズから推定したトークン）の行を含む。請求額ではない")
	}
	return c
}

// combine converts the AI cost to bps with usd_jpy and notional_jpy and subtracts it from the
// candidate's mean net (and, for the difference, the reference's cost too). The conversion is
// cost_bps = usd_per_row × usd_jpy / notional_jpy × 10000. It is null unless both are given.
func combine(req Request, mean, diffMean *float64, cand, ref Cost) Combined {
	usdJPY, notional, ok := req.costConversion()
	switch {
	case !ok:
		return Combined{Reason: "usd_jpy_and_notional_jpy_required"}
	case cand.Rows == 0 || mean == nil:
		return Combined{Reason: "no_common_rows"}
	case !cand.Complete:
		return Combined{Reason: "cost_unknown"}
	}
	toBps := func(c Cost) float64 { return *c.USDPerRow * usdJPY / notional * 10000 }
	bps := toBps(cand)
	after := *mean - bps
	out := Combined{CostBpsPerRow: &bps, MeanNetAfterAICostBps: &after}
	if !ref.Complete {
		out.Reason = "reference_cost_unknown"
		return out
	}
	live := toBps(ref)
	out.LiveCostBpsPerRow = &live
	if diffMean != nil {
		d := *diffMean - (bps - live)
		out.DiffAfterAICostBps = &d
	}
	return out
}
