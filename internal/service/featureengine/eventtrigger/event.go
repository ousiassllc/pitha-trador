package eventtrigger

import (
	"math"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// Thresholds are FR-SCAN-1/FR-SCAN-2's `threshold` cutoffs for
// Detect's continuous signals (functional.md §4.3): a symbol whose
// abs(Return1mChange), abs(VolumeRatioChange), abs(SpreadChangeBps) and
// abs(OrderbookImbalanceChange) all fall under their respective
// threshold, and has none of Signal's remaining boolean events set
// either, is the "no_event" quiet case FR-SCAN-2 skips the Jev call for.
// functional.md §4.3 fixes no numeric default for these - unlike Fast
// Screener/Risk's thresholds - so callers source them from
// config/strategy.yaml's scan.event_trigger (internal/config.
// EventTriggerConfig), this package's own initial tuning pass.
type Thresholds struct {
	Return1mChange           float64
	VolumeRatioChange        float64
	SpreadChangeBps          float64
	OrderbookImbalanceChange float64
	TradeFlowImbalanceChange float64
}

// Signal is one instrument's FR-SCAN-1 event-driven re-evaluation
// signal, as Detect derives it by comparing curr against prev and
// th. Every field independently satisfying FR-SCAN-1 means an immediate
// re-evaluation is warranted (Triggered reports the combined result);
// when none do, FR-SCAN-2's suppression applies instead.
type Signal struct {
	// Return1mChangeExceeded is abs(curr.Feature.Return1m) >=
	// th.Return1mChange (curr.Feature.Return1m == nil, i.e. insufficient
	// history - FR-FE-2 - is treated as no signal, not an exceedance).
	Return1mChangeExceeded bool
	// VolumeRatioChangeExceeded is abs(curr.Feature.VolumeRatio5m) >=
	// th.VolumeRatioChange (nil treated as no signal).
	VolumeRatioChangeExceeded bool
	// SpreadChangeExceeded is abs(curr.SpreadBps - prev.SpreadBps) >=
	// th.SpreadChangeBps (either nil, i.e. board data unavailable for
	// that bar - FR-FE-2 - treated as no signal).
	SpreadChangeExceeded bool
	// OrderbookImbalanceChanged is
	// abs(curr.Feature.OrderbookImbalance - prev.Feature.
	// OrderbookImbalance) >= th.OrderbookImbalanceChange (either nil,
	// i.e. board data unavailable - FR-FE-2 - treated as no signal).
	OrderbookImbalanceChanged bool
	// VWAPCrossed is true when Price crossed from one side of VWAP to
	// the other between prev and curr (Feature.PriceVsVWAPBps sign
	// flip).
	VWAPCrossed bool
	// HighLowBreak is true when curr.Price exceeds every price in
	// history (a fresh session high) or falls under every price in
	// history (a fresh session low).
	HighLowBreak bool
	// OrderFlowChange is
	// abs(curr.Feature.TradeFlowImbalance - prev.Feature.
	// TradeFlowImbalance) >= th.TradeFlowImbalanceChange (either nil,
	// i.e. no tick data for that bar - FR-FE-2 - treated as no signal).
	OrderFlowChange bool
	// NewsFlag is a pass-through caller input (Detect's newsFlag
	// parameter): News Ingest's per-symbol flag, which this package
	// cannot derive from domain.Snapshot alone.
	NewsFlag bool
}

// Triggered reports whether sig warrants an immediate re-evaluation per
// FR-SCAN-1 (true), or whether FR-SCAN-2's quiet suppression condition
// holds instead (false: every continuous signal stayed under its
// threshold and no boolean event fired, so the Jev call for this cycle
// should be skipped).
func (sig Signal) Triggered() bool {
	return sig.Return1mChangeExceeded ||
		sig.VolumeRatioChangeExceeded ||
		sig.SpreadChangeExceeded ||
		sig.OrderbookImbalanceChanged ||
		sig.VWAPCrossed ||
		sig.HighLowBreak ||
		sig.OrderFlowChange ||
		sig.NewsFlag
}

// Detect computes prev→curr's Signal for one instrument against
// th (functional.md §4.3 FR-SCAN-1's eight event conditions, minus
// NewsFlag which newsFlag supplies directly - see Signal's doc). history is this instrument's prior bars (any
// bars at or before prev's Timestamp; curr must not be included), used
// for the high/low breakout check - the same look-ahead-safe convention
// Input.History uses in Compute (FR-FE-1).
func Detect(prev, curr domain.Snapshot, history []domain.Snapshot, th Thresholds, newsFlag bool) Signal {
	sig := Signal{NewsFlag: newsFlag}

	if curr.Feature.Return1m != nil {
		sig.Return1mChangeExceeded = math.Abs(*curr.Feature.Return1m) >= th.Return1mChange
	}
	if curr.Feature.VolumeRatio5m != nil {
		sig.VolumeRatioChangeExceeded = math.Abs(*curr.Feature.VolumeRatio5m) >= th.VolumeRatioChange
	}
	if curr.SpreadBps != nil && prev.SpreadBps != nil {
		sig.SpreadChangeExceeded = math.Abs(*curr.SpreadBps-*prev.SpreadBps) >= th.SpreadChangeBps
	}
	if curr.Feature.OrderbookImbalance != nil && prev.Feature.OrderbookImbalance != nil {
		sig.OrderbookImbalanceChanged = math.Abs(*curr.Feature.OrderbookImbalance-*prev.Feature.OrderbookImbalance) >= th.OrderbookImbalanceChange
	}
	if curr.Feature.TradeFlowImbalance != nil && prev.Feature.TradeFlowImbalance != nil {
		sig.OrderFlowChange = math.Abs(*curr.Feature.TradeFlowImbalance-*prev.Feature.TradeFlowImbalance) >= th.TradeFlowImbalanceChange
	}

	prevSign := prev.Feature.PriceVsVWAPBps >= 0
	currSign := curr.Feature.PriceVsVWAPBps >= 0
	sig.VWAPCrossed = prevSign != currSign

	if len(history) > 0 {
		high, low := history[0].Price, history[0].Price
		for _, h := range history[1:] {
			if h.Price > high {
				high = h.Price
			}
			if h.Price < low {
				low = h.Price
			}
		}
		sig.HighLowBreak = curr.Price > high || curr.Price < low
	}

	return sig
}
