// Package researchlabel holds the value types of the research-only
// fixed-horizon labels (issue #1074, table research_horizon_labels): what
// holding from a market snapshot's bar to a horizon's exit bar returned, per
// side, before and after the cost the triple-barrier label assumes. The record
// is never a trading input (see internal/repository/researchlabel).
package researchlabel

import "time"

// Horizon identifiers (research_horizon_labels.horizon).
const (
	Horizon30m   = "30m"
	Horizon60m   = "60m"
	HorizonClose = "close"
)

// LabelVersion is research_horizon_labels.label_version: 1 is the fixed-horizon
// exit without take-profit/stop-loss barriers.
const LabelVersion = 1

// Horizons are the recorded horizons in output order.
var Horizons = []string{Horizon30m, Horizon60m, HorizonClose}

// Side is the result of one side (LONG or SHORT) of a label, in bps of the entry
// quote.
type Side struct {
	// GrossBps is the direction-adjusted return before cost.
	GrossBps float64
	// CostBps is the cost the triple-barrier label assumes for the side.
	CostBps float64
	// NetBps is GrossBps - CostBps.
	NetBps float64
}

// Label is one research_horizon_labels row. Long or Short is nil when the entry
// bar carries no executable quote for that side (LONG enters at the ask, SHORT
// at the bid).
type Label struct {
	SnapshotID   int64
	InstrumentID int64
	Symbol       string
	SnapshotAt   time.Time
	Horizon      string
	ExitAt       time.Time
	EntryBid     *float64
	EntryAsk     *float64
	EntryPrice   float64
	ExitPrice    float64
	SpreadBps    *float64
	Long         *Side
	Short        *Side
}
