package featureengine_test

import (
	"math"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/featureengine"
)

func book(bid, ask, bidQty, askQty float64) featureengine.Reading {
	return featureengine.Reading{Price: 100, Bid: new(bid), Ask: new(ask), BidQty: new(bidQty), AskQty: new(askQty)}
}

func TestCompute_MicropriceDevBps(t *testing.T) {
	// bid 100 x 300, ask 101 x 100: microprice = (100*100 + 101*300)/400 = 100.75,
	// mid = 100.5, deviation = 0.25/100.5 * 10000 bps.
	f := featureengine.Compute(featureengine.Input{Timestamp: t0, Current: book(100, 101, 300, 100)})
	wantValue(t, "MicropriceDevBps", f.MicropriceDevBps, 0.25/100.5*10000)

	// A symmetric book has no deviation.
	f = featureengine.Compute(featureengine.Input{Timestamp: t0, Current: book(100, 101, 200, 200)})
	wantValue(t, "MicropriceDevBps(symmetric)", f.MicropriceDevBps, 0)
}

// Missing quotes, empty sides, a crossed book and a zero price must leave the
// derived values nil - never NaN, Inf or a division by zero.
func TestCompute_MicropriceDevBps_NilOnUnusableBook(t *testing.T) {
	zeroBid := book(0, 0, 10, 10) // mid 0
	cases := map[string]featureengine.Reading{
		"no book":           {Price: 100},
		"missing ask":       {Price: 100, Bid: new(100.0), BidQty: new(10.0), AskQty: new(10.0)},
		"both quantities 0": book(100, 101, 0, 0),
		"crossed":           book(102, 101, 10, 10),
		"zero mid":          zeroBid,
	}
	for name, r := range cases {
		f := featureengine.Compute(featureengine.Input{Timestamp: t0, Current: r})
		if f.MicropriceDevBps != nil {
			t.Errorf("%s: MicropriceDevBps = %v, want nil", name, *f.MicropriceDevBps)
		}
	}
	// One empty side keeps the quantity-weighted mid defined (it equals the
	// other side's quote) and finite.
	f := featureengine.Compute(featureengine.Input{Timestamp: t0, Current: book(100, 101, 0, 50)})
	if f.MicropriceDevBps == nil || math.IsNaN(*f.MicropriceDevBps) || math.IsInf(*f.MicropriceDevBps, 0) {
		t.Errorf("one empty side: MicropriceDevBps = %v, want a finite value", f.MicropriceDevBps)
	}
}

func TestCompute_OrderFlowPassThrough(t *testing.T) {
	flow := featureengine.OrderFlow{
		ShortWindow: 10 * time.Second, MidWindow: 30 * time.Second, LongWindow: time.Minute,
		Short: new(5.0), Mid: new(-3.0), Long: nil,
	}
	f := featureengine.Compute(featureengine.Input{Timestamp: t0, Current: book(100, 101, 10, 10), OrderFlow: flow})
	wantValue(t, "OFIShort", f.OFIShort, 5)
	wantValue(t, "OFIMid", f.OFIMid, -3)
	if f.OFILong != nil {
		t.Errorf("OFILong = %v, want nil (no event in the long window)", *f.OFILong)
	}
	if f.OFIWindowShortSeconds == nil || *f.OFIWindowShortSeconds != 10 ||
		f.OFIWindowMidSeconds == nil || *f.OFIWindowMidSeconds != 30 ||
		f.OFIWindowLongSeconds == nil || *f.OFIWindowLongSeconds != 60 {
		t.Errorf("window seconds = %v/%v/%v, want 10/30/60", f.OFIWindowShortSeconds, f.OFIWindowMidSeconds, f.OFIWindowLongSeconds)
	}
}

func TestCompute_OrderFlow_NotRunningAndNonFinite(t *testing.T) {
	f := featureengine.Compute(featureengine.Input{Timestamp: t0, Current: book(100, 101, 10, 10)})
	if f.OFIShort != nil || f.OFIMid != nil || f.OFILong != nil ||
		f.OFIWindowShortSeconds != nil || f.OFIWindowMidSeconds != nil || f.OFIWindowLongSeconds != nil {
		t.Errorf("zero OrderFlow left OFI fields set: %+v", f)
	}

	f = featureengine.Compute(featureengine.Input{Timestamp: t0, Current: book(100, 101, 10, 10), OrderFlow: featureengine.OrderFlow{
		ShortWindow: time.Second, Short: new(math.NaN()), Mid: new(math.Inf(1)), Long: new(math.Inf(-1)),
	}})
	if f.OFIShort != nil || f.OFIMid != nil || f.OFILong != nil {
		t.Errorf("non-finite OFI was kept: %v/%v/%v", f.OFIShort, f.OFIMid, f.OFILong)
	}
}
