// Package vwapcross implements FR-EXIT-1's VWAP逆クロス Exit condition: price
// crossing VWAP against an open position (as opposed to merely sitting on
// the adverse side). It lives in its own directory, next to
// internal/service/execution, to keep that directory within the
// per-directory line budget.
package vwapcross

import "github.com/ousiassllc/pitha-trador/internal/domain"

// Adverse reports whether price sits on the side of vwap that works against
// positionSide (a LONG below VWAP, a SHORT above it). Price exactly on VWAP
// is not adverse.
func Adverse(positionSide string, price, vwap float64) bool {
	if positionSide == domain.PositionSideLong {
		return price < vwap
	}
	return price > vwap
}

// Crossed reports whether price crossed VWAP against positionSide: it is
// adverse now (price vs vwap) but was not at the previous observation
// (prevPrice vs prevVWAP). Staying on the adverse side is not a cross, so a
// position entered on the adverse side is not exited by this condition until
// it has moved to the favorable side and back.
func Crossed(positionSide string, prevPrice, prevVWAP, price, vwap float64) bool {
	return !Adverse(positionSide, prevPrice, prevVWAP) && Adverse(positionSide, price, vwap)
}

// Observation is the price/VWAP pair seen at a position's previous
// evaluation, the baseline Crossed compares the next evaluation to.
type Observation struct {
	PositionID int64
	Price      float64
	VWAP       float64
}

// Tracker remembers each instrument's latest Observation. The zero value is
// ready to use; it is not safe for concurrent use (Engine.OnSnapshot calls it
// under snapshotMu).
type Tracker struct {
	last map[int64]Observation
}

// Previous returns the last Observation recorded for positionID on
// instrumentID, or false when it has not been evaluated with a VWAP yet
// (e.g. its first evaluation, or after a restart).
func (t *Tracker) Previous(instrumentID, positionID int64) (Observation, bool) {
	obs, ok := t.last[instrumentID]
	return obs, ok && obs.PositionID == positionID
}

// Record stores obs as instrumentID's baseline for the next evaluation. An
// instrument has at most one open position, so a closed position's entry is
// simply replaced by the next position's.
func (t *Tracker) Record(instrumentID int64, obs Observation) {
	if t.last == nil {
		t.last = make(map[int64]Observation)
	}
	t.last[instrumentID] = obs
}
