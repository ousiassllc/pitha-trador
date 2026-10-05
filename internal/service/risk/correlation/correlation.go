// Package correlation is functional.md §4.7 FR-RISK-1's "同じ方向に重ねない"
// gates: a cap on how many open positions may point the same way, and a
// block on adding to a direction while the whole market is moving against
// it. They are pure functions of the risk limits and the readings Check
// already has, split out of internal/service/risk only to keep that
// directory within linterly's size limit (like sizing).
package correlation

import (
	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// Rejection reasons (they equal the same-named internal/service/risk.Reason*
// values, which alias them).
const (
	ReasonMaxSameDirectionPositions = "max_same_direction_positions"
	ReasonMarketAdverse             = "market_adverse_to_direction"
)

// SameDirectionExceeded reports whether sameDirectionCount open positions
// in the candidate's direction already reach
// limits.MaxSameDirectionPositions. Momentum, volume-surge and breakout
// candidates cluster on the same names in the same tape, so a per-symbol
// cap alone does not stop the book from piling onto one direction.
func SameDirectionExceeded(limits config.RiskLimits, sameDirectionCount int) bool {
	return sameDirectionCount >= limits.MaxSameDirectionPositions
}

// MarketAdverse reports whether a new entry in direction would add to a
// direction the market is moving against: positions are already held that
// way (sameDirectionCount > 0) and the tracked market indexes' 5-minute
// return (Feature.MarketReturn5m, a decimal ratio) has moved at least
// limits.MarketAdverseReturn5mPct percent against it (down for LONG, up
// for SHORT). It returns that return in percent for the rejection detail.
//
// With no market return (nil: no index tracked or its bars are stale,
// FR-FE-2) or an unknown direction there is nothing to judge, so it
// reports false; so does a book that holds nothing in that direction,
// since the gate is about piling onto losses already correlated with the
// market, not about the market view of a lone position.
func MarketAdverse(limits config.RiskLimits, direction string, sameDirectionCount int, marketReturn5m *float64) (float64, bool) {
	if marketReturn5m == nil || sameDirectionCount == 0 {
		return 0, false
	}
	pct := domain.RatioToPercent(*marketReturn5m)
	switch direction {
	case domain.JevDirectionLong:
		return pct, pct <= -limits.MarketAdverseReturn5mPct
	case domain.JevDirectionShort:
		return pct, pct >= limits.MarketAdverseReturn5mPct
	}
	return pct, false
}
