package replaylayer

import (
	"fmt"
	"math"
	"sort"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/backtest"
	"github.com/ousiassllc/pitha-trador/internal/service/metaablation"
)

// tradeRecords books the closed trades with their kind (momentum or VWAP
// reversion, read from the evaluation that opened them) and their P&L in yen:
// quantity x entry fill x net return, the figure the simulated Risk Engine
// books on close.
func tradeRecords(run cellRun) []TradeRecord {
	opened := map[tradeKey]backtest.EntryEvaluation{}
	for _, ev := range run.evals {
		if ev.Outcome == backtest.OutcomeOpened {
			opened[tradeKey{ev.InstrumentID, ev.BarTime}] = ev
		}
	}
	out := make([]TradeRecord, 0, len(run.trades))
	for _, t := range run.trades {
		ev := opened[tradeKey{t.InstrumentID, t.EntryTimestamp}]
		out = append(out, TradeRecord{
			Day: dayOf(t.ExitTimestamp), InstrumentID: t.InstrumentID, DecisionID: ev.Decision.ID,
			Direction: t.Direction, Reversion: isReversion(ev), Quantity: t.Quantity,
			NetReturnBps: t.NetReturnPct * 100,
			PnLJPY:       float64(t.Quantity) * t.EntryPrice * t.NetReturnPct / 100,
			ExitReason:   t.ExitReason,
		})
	}
	return out
}

// dayPnL is one day of a P&L path, by stream.
type dayPnL struct {
	day                        string
	total, momentum, reversion float64
	trades                     int
}

// dailyPnL sums the trades' P&L by exit day over exactly days: the days common
// to the four cells, a day with no trade being 0.
func dailyPnL(days []string, trades []TradeRecord) ([]dayPnL, error) {
	index := make(map[string]int, len(days))
	out := make([]dayPnL, len(days))
	for i, d := range days {
		index[d] = i
		out[i].day = d
	}
	for _, t := range trades {
		i, ok := index[t.Day]
		if !ok {
			return nil, fmt.Errorf("replaylayer: a trade closed on %s, which is not a day of the segment", t.Day)
		}
		out[i].trades++
		out[i].total += t.PnLJPY
		if t.Reversion {
			out[i].reversion += t.PnLJPY
		} else {
			out[i].momentum += t.PnLJPY
		}
	}
	return out, nil
}

// dailyPoints is the continuous daily path of a segment: daily P&L and the
// equity carried from day to day from the segment's initial capital.
func dailyPoints(pnl []dayPnL, capital float64) []DailyPoint {
	out := make([]DailyPoint, len(pnl))
	equity := capital
	for i, d := range pnl {
		equity += d.total
		out[i] = DailyPoint{
			Day: d.day, PnLJPY: d.total, PnLRatio: d.total / capital,
			MomentumPnLJPY: d.momentum, ReversionPnLJPY: d.reversion, Trades: d.trades, EquityJPY: equity,
		}
	}
	return out
}

// drawdownOf is the maximum drawdown on the daily equity curve (peak-to-trough,
// the initial capital counting as the first peak).
func drawdownOf(points []DailyPoint, capital float64) Drawdown {
	peak, peakDay := capital, "start"
	var dd Drawdown
	for _, p := range points {
		if p.EquityJPY > peak {
			peak, peakDay = p.EquityJPY, p.Day
			continue
		}
		if drop := peak - p.EquityJPY; drop > dd.MaxJPY {
			dd.MaxJPY, dd.MaxPct, dd.PeakDay, dd.TroughDay = drop, drop/peak*100, peakDay, p.Day
		}
	}
	dd.Description = "maximum peak-to-trough drop of this segment's daily equity curve (initial capital as the first peak); " +
		"a description only - the difference between cells is unreliable with few days"
	if dd.TroughDay == "" {
		dd.Description = "the daily equity curve of this segment never fell below a previous peak; " + dd.Description
	}
	return dd
}

// countsOf are the counts of the evaluations kept by keep and the closed trades
// of the same kind (kind: nil keeps all, else true selects reversion).
func countsOf(evals []backtest.EntryEvaluation, trades []TradeRecord, decisionsInWindow int, reversion *bool) Counts {
	keepEval := func(ev backtest.EntryEvaluation) bool { return reversion == nil || isReversion(ev) == *reversion }
	stages := stageCounts(evals, keepEval)
	c := Counts{DecisionsInWindow: decisionsInWindow, Evaluated: stages[0].Reached}
	c.NotEvaluated = c.DecisionsInWindow - c.Evaluated
	c.Eligible = reachedOf(stages, metaablation.StageQuality)
	c.Takes = reachedOf(stages, metaablation.StageRisk)
	for _, ev := range evals {
		if keepEval(ev) && ev.Outcome == backtest.OutcomeOpened {
			c.Entries++
		}
	}
	for _, t := range trades {
		if reversion != nil && t.Reversion != *reversion {
			continue
		}
		c.Trades++
		c.NetPnLJPY += t.PnLJPY
		c.NetBpsSum += t.NetReturnBps
	}
	if c.Trades > 0 {
		c.MeanNetBpsPerEntry = c.NetBpsSum / float64(c.Trades)
	}
	if c.Eligible > 0 {
		c.NetBpsPerEligible = c.NetBpsSum / float64(c.Eligible)
		c.PnLJPYPerEligible = c.NetPnLJPY / float64(c.Eligible)
	}
	return c
}

// cellSegment assembles one cell's results on one segment.
func cellSegment(seg SegmentSpec, run cellRun, decisions []domain.JevDecision) (CellSegment, error) {
	trades := tradeRecords(run)
	sort.SliceStable(trades, func(i, j int) bool { return trades[i].Day < trades[j].Day })
	pnl, err := dailyPnL(seg.Days, trades)
	if err != nil {
		return CellSegment{}, err
	}
	reversion := true
	points := dailyPoints(pnl, seg.InitialCapitalJPY)
	for _, p := range points {
		if math.IsNaN(p.EquityJPY) || math.IsInf(p.EquityJPY, 0) {
			return CellSegment{}, fmt.Errorf("replaylayer: non-finite equity on %s", p.Day)
		}
	}
	return CellSegment{
		Segment: seg.Index, Daily: points, Drawdown: drawdownOf(points, seg.InitialCapitalJPY),
		Counts:          countsOf(run.evals, trades, decisionsIn(decisions, seg, nil), nil),
		Reversion:       countsOf(run.evals, trades, decisionsIn(decisions, seg, &reversion), &reversion),
		Stages:          stageCounts(run.evals, func(backtest.EntryEvaluation) bool { return true }),
		ReversionStages: stageCounts(run.evals, isReversion),
		DropReasons:     dropReasons(run.evals), Trades: trades,
	}, nil
}

// decisionsIn counts the decisions timestamped inside seg (kind: nil counts all,
// else true selects the VWAP reversion decisions).
func decisionsIn(decisions []domain.JevDecision, seg SegmentSpec, reversion *bool) int {
	n := 0
	for _, d := range decisions {
		if d.Timestamp.Before(seg.From) || !d.Timestamp.Before(seg.To) {
			continue
		}
		if reversion == nil || d.MarketRegime.UsesVWAPReversion() == *reversion {
			n++
		}
	}
	return n
}
