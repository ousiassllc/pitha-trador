package replaylayer

import (
	"context"

	"github.com/ousiassllc/pitha-trador/internal/service/backtest"
	"github.com/ousiassllc/pitha-trador/internal/service/metaablation"
)

// Run replays the four cells over the window and returns the replay layer's
// result. It reads nothing but in, calls no model training or calibration and
// writes nothing. The same input gives the same result.
func Run(ctx context.Context, in Input) (Result, error) {
	if err := in.validate(); err != nil {
		return Result{}, err
	}
	cells := metaablation.Cells()
	p := newPlan(in)
	decisions := backtest.NewSliceDecisionSource(in.Decisions)

	res := Result{
		Window: Window{From: in.From, To: in.To}, Config: in.configRecord(), Population: PopulationDescription,
		Unavailable: p.unavailable, Segments: p.segments, Cells: make([]CellResult, len(cells)),
	}
	paths := make([][]pnlPath, len(cells))
	for i, c := range cells {
		res.Cells[i] = CellResult{Cell: c, Name: c.Name(), Config: cellConfig(c)}
	}
	for _, seg := range p.segments {
		runs, err := in.runCells(ctx, seg, decisions)
		if err != nil {
			return Result{}, err
		}
		for i := range cells {
			cs, err := cellSegment(seg, runs[i], in.Decisions)
			if err != nil {
				return Result{}, err
			}
			res.Cells[i].Segments = append(res.Cells[i].Segments, cs)
			res.Cells[i].Totals = addCounts(res.Cells[i].Totals, cs.Counts)
			res.Cells[i].ReversionTotals = addCounts(res.Cells[i].ReversionTotals, cs.Reversion)
			pnl, err := dailyPnL(seg.Days, cs.Trades)
			if err != nil {
				return Result{}, err
			}
			paths[i] = append(paths[i], pnlPath{segment: seg.Index, capital: seg.InitialCapitalJPY, days: pnl})
		}
	}
	var err error
	if res.Comparisons, err = compare(ExperimentContinuous, paths); err != nil {
		return Result{}, err
	}
	if in.ReferenceDailyReset {
		if res.ReferenceDailyReset, err = in.reference(ctx, p, decisions); err != nil {
			return Result{}, err
		}
	}
	return res, nil
}

// addCounts sums two segments' counts (counts and yen only: no series).
func addCounts(a, b Counts) Counts {
	a.DecisionsInWindow += b.DecisionsInWindow
	a.Evaluated += b.Evaluated
	a.NotEvaluated += b.NotEvaluated
	a.Eligible += b.Eligible
	a.Takes += b.Takes
	a.Entries += b.Entries
	a.Trades += b.Trades
	a.NetPnLJPY += b.NetPnLJPY
	a.NetBpsSum += b.NetBpsSum
	a.MeanNetBpsPerEntry, a.NetBpsPerEligible, a.PnLJPYPerEligible = 0, 0, 0
	if a.Trades > 0 {
		a.MeanNetBpsPerEntry = a.NetBpsSum / float64(a.Trades)
	}
	if a.Eligible > 0 {
		a.NetBpsPerEligible = a.NetBpsSum / float64(a.Eligible)
		a.PnLJPYPerEligible = a.NetPnLJPY / float64(a.Eligible)
	}
	return a
}
