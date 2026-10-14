package marketdatajob

import (
	"context"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/orderflowsettings"
	"github.com/ousiassllc/pitha-trador/internal/service/broker"
	"github.com/ousiassllc/pitha-trador/internal/service/orderflow"
)

type mapSettings map[string]string

func (m mapSettings) Get(_ context.Context, key string) (string, bool, error) {
	v, ok := m[key]
	return v, ok, nil
}

func flowQuote(bidQty float64) broker.Quote {
	return broker.Quote{Bid: new(100.0), BidQty: new(bidQty), Ask: new(101.0), AskQty: new(20.0)}
}

// Issue #750: the persisted bar carries OFI over the Settings windows and the
// microprice deviation; they are stored per bar for later analysis.
func TestHandleMarketData_PersistsOrderFlowOverConfiguredWindows(t *testing.T) {
	env := newTestEnv(t)
	tracker := orderflow.NewTracker()
	env.OrderFlow = tracker
	env.OrderFlowSettings = mapSettings{orderflowsettings.KeyShort: "5", orderflowsettings.KeyMid: "20", orderflowsettings.KeyLong: "90"}

	now := barClock()
	tracker.Observe("7203", flowQuote(10), now.Add(-70*time.Second))
	tracker.Observe("7203", flowQuote(14), now.Add(-60*time.Second)) // +4: only in the 90s window
	tracker.Observe("7203", flowQuote(19), now.Add(-10*time.Second)) // +5: in the 20s and 90s windows

	q := flowQuote(19)
	q.Symbol, q.Price, q.VWAP = "7203", 100.5, 100.5
	env.Fake.quote = q
	inst := mustCreateInstrument(t, env, "7203")
	if err := env.HandleMarketData(context.Background(), marketDataJob(t, inst)); err != nil {
		t.Fatalf("HandleMarketData: %v", err)
	}

	snaps, err := env.Snapshots.ListByInstrument(context.Background(), inst.ID, 10)
	if err != nil || len(snaps) != 1 {
		t.Fatalf("ListByInstrument = %v, %v", snaps, err)
	}
	f := snaps[0].Feature
	if f.OFIShort != nil {
		t.Errorf("OFIShort = %v, want nil (no event in the last 5s)", *f.OFIShort)
	}
	if f.OFIMid == nil || *f.OFIMid != 5 || f.OFILong == nil || *f.OFILong != 9 {
		t.Errorf("OFIMid/OFILong = %v/%v, want 5/9", f.OFIMid, f.OFILong)
	}
	if f.OFIWindowShortSeconds == nil || *f.OFIWindowShortSeconds != 5 ||
		f.OFIWindowMidSeconds == nil || *f.OFIWindowMidSeconds != 20 ||
		f.OFIWindowLongSeconds == nil || *f.OFIWindowLongSeconds != 90 {
		t.Errorf("window seconds = %v/%v/%v, want 5/20/90", f.OFIWindowShortSeconds, f.OFIWindowMidSeconds, f.OFIWindowLongSeconds)
	}
	if f.MicropriceDevBps == nil {
		t.Error("MicropriceDevBps = nil, want a value for a two-sided book")
	}
}

func TestHandleMarketData_OrderFlowDefaultsAndDisabled(t *testing.T) {
	ctx := context.Background()

	// No window settings: the 10s/30s/60s defaults.
	env := newTestEnv(t)
	env.OrderFlow = orderflow.NewTracker()
	q := flowQuote(10)
	q.Symbol, q.Price, q.VWAP = "7203", 100.5, 100.5
	env.Fake.quote = q
	inst := mustCreateInstrument(t, env, "7203")
	if err := env.HandleMarketData(ctx, marketDataJob(t, inst)); err != nil {
		t.Fatalf("HandleMarketData: %v", err)
	}
	snaps, _ := env.Snapshots.ListByInstrument(ctx, inst.ID, 1)
	f := snaps[0].Feature
	if f.OFIShort != nil || f.OFIMid != nil || f.OFILong != nil {
		t.Errorf("OFI without any PUSH event = %v/%v/%v, want nil (unknown, not 0)", f.OFIShort, f.OFIMid, f.OFILong)
	}
	if f.OFIWindowShortSeconds == nil || *f.OFIWindowShortSeconds != 10 || *f.OFIWindowMidSeconds != 30 || *f.OFIWindowLongSeconds != 60 {
		t.Errorf("default windows = %v/%v/%v, want 10/30/60", f.OFIWindowShortSeconds, f.OFIWindowMidSeconds, f.OFIWindowLongSeconds)
	}

	// No tracker (立花 / OFI source off): everything stays NULL, bar persisted.
	env = newTestEnv(t)
	env.Fake.quote = q
	inst = mustCreateInstrument(t, env, "7203")
	if err := env.HandleMarketData(ctx, marketDataJob(t, inst)); err != nil {
		t.Fatalf("HandleMarketData: %v", err)
	}
	snaps, _ = env.Snapshots.ListByInstrument(ctx, inst.ID, 1)
	if f := snaps[0].Feature; f.OFIWindowShortSeconds != nil || f.OFIMid != nil {
		t.Errorf("without a tracker OFI fields were set: %+v", f.OrderFlowFeatures)
	}
}

// A damaged window setting must not stop the bar from being persisted.
func TestHandleMarketData_InvalidWindowSettingsFallBackToDefaults(t *testing.T) {
	env := newTestEnv(t)
	env.OrderFlow = orderflow.NewTracker()
	env.OrderFlowSettings = mapSettings{orderflowsettings.KeyShort: "999"}
	q := flowQuote(10)
	q.Symbol, q.Price, q.VWAP = "7203", 100.5, 100.5
	env.Fake.quote = q
	inst := mustCreateInstrument(t, env, "7203")
	if err := env.HandleMarketData(context.Background(), marketDataJob(t, inst)); err != nil {
		t.Fatalf("HandleMarketData: %v", err)
	}
	snaps, _ := env.Snapshots.ListByInstrument(context.Background(), inst.ID, 1)
	if got := snaps[0].Feature.OFIWindowShortSeconds; got == nil || *got != 10 {
		t.Errorf("OFIWindowShortSeconds = %v, want the default 10", got)
	}
}
