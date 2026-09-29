// Package sizing is functional.md §4.8 FR-ENTRY-3's position sizing: a
// pure function of the risk limits, split out of internal/service/risk
// only to keep that directory within linterly's size limit.
package sizing

import (
	"math"

	"github.com/ousiassllc/pitha-trador/internal/config"
)

// TradingLotSize is the TSE 単元株数 every order quantity is a multiple of.
const TradingLotSize = 100

// DefaultStopLossPct is FR-EXIT-2's initial stop_loss_pct, used for
// position sizing when Config.StopLossPct is zero.
const DefaultStopLossPct = 0.6

// Rejection reasons Quantity returns with a zero quantity: the FR-RISK-1
// limit that leaves no room for one lot (they equal the same-named
// internal/service/risk.Reason* values, which alias them), or
// ReasonInvalidInput when no size can be computed at all (no initial
// capital, non-positive price): fail closed rather than size against a
// made-up baseline.
const (
	ReasonMaxTradeLossPct         = "max_trade_loss_pct"
	ReasonMaxPositionPerSymbolPct = "max_position_per_symbol_pct"
	ReasonMaxTotalExposurePct     = "max_total_exposure_pct"
	ReasonInvalidInput            = "invalid_sizing_input"
)

// Quantity implements functional.md §4.8's position sizing
// (FR-RISK-1): the largest whole number of TradingLotSize lots such that
//
//   - the loss if the stop is hit stays within max_trade_loss_pct:
//     equity × max_trade_loss_pct / (price × stop_loss_pct),
//   - the position stays within max_position_per_symbol_pct:
//     equity × max_position_per_symbol_pct / price, and
//   - the combined exposure stays within max_total_exposure_pct:
//     equity × (max_total_exposure_pct - totalExposurePct) / price,
//
// where equity is limits.InitialCapital and stopLossPct/every *_pct are
// percentages (0.6 means 0.6%). When not even one lot fits it returns
// quantity 0 and the Reason* constant of the binding limit, so Check can
// reject with the limit that actually prevented the trade.
func Quantity(limits config.RiskLimits, stopLossPct, price, totalExposurePct float64) (int64, string) {
	if limits.InitialCapital <= 0 || price <= 0 || math.IsNaN(price) || math.IsInf(price, 0) {
		return 0, ReasonInvalidInput
	}

	type bound struct {
		shares float64
		reason string
	}
	bounds := []bound{
		{sharesForTradeLoss(limits, stopLossPct, price), ReasonMaxTradeLossPct},
		{limits.InitialCapital * limits.MaxPositionPerSymbolPct / 100 / price, ReasonMaxPositionPerSymbolPct},
		{limits.InitialCapital * math.Max(0, limits.MaxTotalExposurePct-totalExposurePct) / 100 / price, ReasonMaxTotalExposurePct},
	}
	binding := bounds[0]
	for _, b := range bounds[1:] {
		if b.shares < binding.shares {
			binding = b
		}
	}

	lots := math.Floor(binding.shares / TradingLotSize)
	if lots < 1 {
		return 0, binding.reason
	}
	return int64(lots) * TradingLotSize, ""
}

// sharesForTradeLoss is the max_trade_loss_pct bound of Quantity. A
// non-positive stopLossPct means no stop bounds the loss, so no size can
// satisfy max_trade_loss_pct.
func sharesForTradeLoss(limits config.RiskLimits, stopLossPct, price float64) float64 {
	if stopLossPct <= 0 {
		return 0
	}
	return limits.InitialCapital * limits.MaxTradeLossPct / 100 / (price * stopLossPct / 100)
}
