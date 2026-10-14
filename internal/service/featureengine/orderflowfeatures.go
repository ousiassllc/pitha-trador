package featureengine

import (
	"math"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// OrderFlow carries the Order Flow Imbalance readings the caller took from
// internal/service/orderflow for one bar (issue #750), passed through to the
// Feature unchanged like the market context. The zero value means "OFI
// source not running": every OFI field and window width is then nil.
type OrderFlow struct {
	// ShortWindow, MidWindow and LongWindow are the trailing widths the
	// readings below were summed over (Settings, default 10s/30s/60s).
	ShortWindow, MidWindow, LongWindow time.Duration
	// Short, Mid and Long are the OFI over each window; nil when no
	// best-quote event fell inside it.
	Short, Mid, Long *float64
}

// apply copies o into f. A window width is only recorded when positive, and
// an OFI value only when finite.
func (o OrderFlow) apply(f *domain.Feature) {
	f.OFIShort, f.OFIWindowShortSeconds = finiteOrNil(o.Short), windowSeconds(o.ShortWindow)
	f.OFIMid, f.OFIWindowMidSeconds = finiteOrNil(o.Mid), windowSeconds(o.MidWindow)
	f.OFILong, f.OFIWindowLongSeconds = finiteOrNil(o.Long), windowSeconds(o.LongWindow)
}

func windowSeconds(d time.Duration) *int64 {
	if d <= 0 {
		return nil
	}
	return new(int64(d / time.Second))
}

func finiteOrNil(v *float64) *float64 {
	if v == nil || math.IsNaN(*v) || math.IsInf(*v, 0) {
		return nil
	}
	return v
}

// micropriceDevBps is the microprice's deviation from the mid in bps,
// (microprice - mid)/mid * 10000: positive when the quantity-weighted fair
// price leans above the mid (more resting buy quantity). nil whenever
// microprice is nil (missing quote/quantity, crossed book, zero quantities)
// or the mid is not positive.
func micropriceDevBps(r Reading) *float64 {
	mp := microprice(r)
	if mp == nil {
		return nil
	}
	mid := (*r.Bid + *r.Ask) / 2
	if !(mid > 0) {
		return nil
	}
	v := (*mp - mid) / mid * 10000
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return nil
	}
	return &v
}
