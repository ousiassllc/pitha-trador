// Package researchlabel records the research-only fixed-horizon labels of
// issue #1074: for every market_snapshots bar, what holding to 30 minutes, 60
// minutes and the close returned before and after the cost the existing
// triple-barrier label assumes (table research_horizon_labels).
//
// The record is read-only research data. It never feeds trading: Policy, Risk,
// order placement, the Paper guard, Live, the Jev prompts, RAG, the meta-model
// training and self-improvement do not read it (isolation_test.go in
// internal/repository/researchlabel pins that). It writes only its own tables
// and only reads market_snapshots.
//
// LabelBars is the pure labeling of one instrument's bars of one JST day;
// Recorder drives it over the days not yet recorded.
package researchlabel

import (
	"sort"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	rlabel "github.com/ousiassllc/pitha-trador/internal/domain/researchlabel"
	"github.com/ousiassllc/pitha-trador/internal/service/calibration"
	"github.com/ousiassllc/pitha-trador/internal/service/marketcalendar"
)

// CloseAcceptWithin is how long before the TSE close (15:30 JST) the last bar
// of the day may fall for the 'close' horizon: the exit must be the close's
// bar, not a bar from mid-afternoon when the data stopped early. The last bar
// at 15:25 or later is accepted.
const CloseAcceptWithin = 5 * time.Minute

// bpsPerUnit converts a fractional price return to basis points.
const bpsPerUnit = 10000.0

// Costs are the costs (bps of the entry quote) the existing label assumes for
// each side at record time: costsettings.LabelBarriers' CostBps.
type Costs struct {
	Long, Short float64
}

// fixedHorizons are the horizons that exit a fixed time after the entry bar.
var fixedHorizons = []struct {
	name string
	d    time.Duration
}{
	{rlabel.Horizon30m, 30 * time.Minute},
	{rlabel.Horizon60m, 60 * time.Minute},
}

// LabelBars returns the research labels of bars, the 1-minute bars of ONE
// instrument within ONE JST day (ascending by timestamp). Each bar is an entry:
// LONG at its ask, SHORT at its bid (a side with no positive quote has no
// result; a bar with neither has no row).
//
// '30m' / '60m' follow the live label's window rule (calibration.Labeler): the
// exit is the last bar at or before entry + horizon, accepted only when it is
// within calibration.HorizonBarTolerance of that target. A window the bars do
// not cover (lunch break, close, data gap) gets no row, so a shortened horizon
// is never recorded as the full one.
//
// 'close' exits at the last bar of the day (at or before 15:30 JST), accepted
// only when it is at or after 15:25 JST, and only for entries before 15:25
// (nothing is left to hold after that).
func LabelBars(bars []domain.Snapshot, costs Costs) []rlabel.Label {
	var out []rlabel.Label
	if len(bars) == 0 {
		return out
	}
	closeLast, closeOK := closeExit(bars)
	var closeFrom time.Time
	if at, ok := marketcalendar.TSE.CloseAt(bars[0].Timestamp); ok {
		closeFrom = at.Add(-CloseAcceptWithin)
	} else {
		closeOK = false
	}
	for i, entry := range bars {
		if !quoted(entry.Ask) && !quoted(entry.Bid) {
			continue
		}
		emit := func(horizon string, exit domain.Snapshot) {
			out = append(out, rlabel.Label{
				SnapshotID: entry.ID, InstrumentID: entry.InstrumentID, Symbol: entry.Symbol, SnapshotAt: entry.Timestamp,
				Horizon: horizon, ExitAt: exit.Timestamp, EntryBid: entry.Bid, EntryAsk: entry.Ask,
				EntryPrice: entry.Price, ExitPrice: exit.Price, SpreadBps: entry.SpreadBps,
				Long:  sideResult(entry.Ask, exit.Price, costs.Long, 1),
				Short: sideResult(entry.Bid, exit.Price, costs.Short, -1),
			})
		}
		for _, h := range fixedHorizons {
			if exit, ok := fixedExit(bars, i, h.d); ok {
				emit(h.name, exit)
			}
		}
		if closeOK && entry.Timestamp.Before(closeFrom) {
			emit(rlabel.HorizonClose, closeLast)
		}
	}
	return out
}

// fixedExit is the exit bar of the entry bars[i] for a horizon d after it.
func fixedExit(bars []domain.Snapshot, i int, d time.Duration) (domain.Snapshot, bool) {
	deadline := bars[i].Timestamp.Add(d)
	// The first bar after the deadline; the one before it is the last bar at
	// or before the deadline.
	j := i + 1 + sort.Search(len(bars)-i-1, func(k int) bool { return bars[i+1+k].Timestamp.After(deadline) })
	if j-1 <= i {
		return domain.Snapshot{}, false
	}
	exit := bars[j-1]
	if exit.Timestamp.Before(deadline.Add(-calibration.HorizonBarTolerance)) {
		return domain.Snapshot{}, false
	}
	return exit, true
}

// closeExit is the day's last bar at or before the TSE close, and whether it
// is the close's own bar (15:25 JST or later).
func closeExit(bars []domain.Snapshot) (domain.Snapshot, bool) {
	closeAt, ok := marketcalendar.TSE.CloseAt(bars[0].Timestamp)
	if !ok {
		return domain.Snapshot{}, false
	}
	for i := len(bars) - 1; i >= 0; i-- {
		if bars[i].Timestamp.After(closeAt) {
			continue
		}
		return bars[i], !bars[i].Timestamp.Before(closeAt.Add(-CloseAcceptWithin))
	}
	return domain.Snapshot{}, false
}

// grossReturnBps is calibration.grossReturnBps: the direction-adjusted return
// from entry to price in bps (factor +1 LONG, -1 SHORT).
func grossReturnBps(entry, price, factor float64) float64 {
	return (price - entry) / entry * bpsPerUnit * factor
}

// quoted reports whether quote is an executable (positive) quote.
func quoted(quote *float64) bool { return quote != nil && *quote > 0 }

// sideResult is one side's result entering at quote and exiting at exitPrice
// (factor +1 LONG, -1 SHORT), or nil when the entry bar has no such quote.
func sideResult(quote *float64, exitPrice, cost, factor float64) *rlabel.Side {
	if !quoted(quote) {
		return nil
	}
	gross := grossReturnBps(*quote, exitPrice, factor)
	return &rlabel.Side{GrossBps: gross, CostBps: cost, NetBps: gross - cost}
}
