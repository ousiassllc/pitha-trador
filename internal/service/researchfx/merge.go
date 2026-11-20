package researchfx

import (
	"slices"

	rfx "github.com/ousiassllc/pitha-trador/internal/domain/researchfx"
	"github.com/ousiassllc/pitha-trador/internal/service/fx/gmo"
)

// Merge joins the BID and the ASK candles of one trading day by their open time,
// oldest first. A minute with a candle on one side only keeps nil for the other
// side. Within a side a repeated minute keeps its last candle.
func Merge(symbol, day string, bid, ask []gmo.Candle) []rfx.Candle {
	byMinute := map[int64]*rfx.Candle{}
	var out []*rfx.Candle
	put := func(cs []gmo.Candle, set func(*rfx.Candle, *rfx.OHLC)) {
		for _, c := range cs {
			k := c.OpenTime.UnixNano()
			m := byMinute[k]
			if m == nil {
				m = &rfx.Candle{Symbol: symbol, OpenTime: c.OpenTime.UTC(), TradingDay: day}
				byMinute[k], out = m, append(out, m)
			}
			set(m, &rfx.OHLC{Open: c.Open, High: c.High, Low: c.Low, Close: c.Close})
		}
	}
	put(bid, func(c *rfx.Candle, o *rfx.OHLC) { c.Bid = o })
	put(ask, func(c *rfx.Candle, o *rfx.OHLC) { c.Ask = o })
	slices.SortFunc(out, func(a, b *rfx.Candle) int { return a.OpenTime.Compare(b.OpenTime) })
	merged := make([]rfx.Candle, len(out))
	for i, c := range out {
		merged[i] = *c
	}
	return merged
}
