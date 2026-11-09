package replaylayer

import (
	"context"

	"github.com/ousiassllc/pitha-trador/internal/service/backtest"
	"github.com/ousiassllc/pitha-trador/internal/service/metaablation"
)

// ReferenceLabel marks the reference experiment wherever it is shown.
const ReferenceLabel = "reference: daily independent reset - every day is replayed from the initial capital with no " +
	"carried state; NOT the continuous equity curve or drawdown, and not a basis of a verdict"

// reference replays every included day of every segment independently from the
// initial state, for the four cells over the same days. It reports daily P&L
// only: no equity curve, no drawdown.
func (in Input) reference(ctx context.Context, p plan, decisions backtest.DecisionSource) (*Reference, error) {
	cells := metaablation.Cells()
	out := &Reference{Label: ReferenceLabel, Cells: make([]ReferenceCell, len(cells))}
	paths := make([][]pnlPath, len(cells))
	for i, c := range cells {
		out.Cells[i] = ReferenceCell{Cell: c, Name: c.Name()}
	}
	for _, seg := range p.segments {
		perCell := make([][]dayPnL, len(cells))
		for _, day := range seg.Days {
			start, err := parseDay(day)
			if err != nil {
				return nil, err
			}
			from, to := maxTime(start, seg.From), minTime(start.AddDate(0, 0, 1), seg.To)
			dayDef := SegmentSpec{Index: seg.Index, From: from, To: to, Days: []string{day}, InitialCapitalJPY: seg.InitialCapitalJPY}
			runs, err := in.runCells(ctx, dayDef, decisions)
			if err != nil {
				return nil, err
			}
			for i := range cells {
				pnl, err := dailyPnL(dayDef.Days, tradeRecords(runs[i]))
				if err != nil {
					return nil, err
				}
				perCell[i] = append(perCell[i], pnl[0])
				out.Cells[i].Days = append(out.Cells[i].Days, ReferenceDay{
					Segment: seg.Index, Day: day, PnLJPY: pnl[0].total, PnLRatio: pnl[0].total / seg.InitialCapitalJPY,
					MomentumPnLJPY: pnl[0].momentum, ReversionPnLJPY: pnl[0].reversion,
				})
			}
		}
		for i := range cells {
			paths[i] = append(paths[i], pnlPath{segment: seg.Index, capital: seg.InitialCapitalJPY, days: perCell[i]})
		}
	}
	var err error
	out.Comparisons, err = compare(ExperimentDailyReset, paths)
	return out, err
}
