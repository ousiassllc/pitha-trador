package fillmodel

import "math"

// tickBand is one row of the TSE tick table: prices up to and including
// UpTo move in steps of Tick yen.
type tickBand struct{ UpTo, Tick float64 }

// tickTable is the standard 呼値の単位 table (TOPIX100構成銘柄・ETF等の
// 特例表は扱わない: instruments does not record that membership).
var tickTable = []tickBand{
	{3_000, 1},
	{5_000, 5},
	{30_000, 10},
	{50_000, 50},
	{300_000, 100},
	{500_000, 500},
	{3_000_000, 1_000},
	{5_000_000, 5_000},
	{30_000_000, 10_000},
	{50_000_000, 50_000},
}

// topTick is the tick above the last table band.
const topTick = 100_000

// TickSize returns the 呼値 for a price in yen.
func TickSize(price float64) float64 {
	for _, b := range tickTable {
		if price <= b.UpTo {
			return b.Tick
		}
	}
	return topTick
}

// tickEpsilon absorbs float noise so a price already on the grid is never
// pushed a whole tick away.
const tickEpsilon = 1e-9

// RoundUp is the lowest tick-grid price at or above price.
func RoundUp(price float64) float64 {
	tick := TickSize(price)
	return math.Ceil(price/tick-tickEpsilon) * tick
}

// RoundDown is the highest tick-grid price at or below price.
func RoundDown(price float64) float64 {
	tick := TickSize(price)
	return math.Floor(price/tick+tickEpsilon) * tick
}
