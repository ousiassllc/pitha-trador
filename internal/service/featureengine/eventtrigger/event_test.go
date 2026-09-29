package eventtrigger_test

import (
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/featureengine/eventtrigger"
)

func ptr(v float64) *float64 { return &v }

func quietThresholds() eventtrigger.Thresholds {
	return eventtrigger.Thresholds{
		Return1mChange:           0.01,
		VolumeRatioChange:        2.0,
		SpreadChangeBps:          10,
		OrderbookImbalanceChange: 0.5,
	}
}

// TestDetectEvent_QuietCycleSuppresses covers FR-SCAN-2: every continuous
// signal under its threshold and no boolean event fired means Triggered
// is false (the Jev call for this cycle should be skipped).
func TestDetectEvent_QuietCycleSuppresses(t *testing.T) {
	now := time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)
	prev := domain.Snapshot{
		Timestamp: now.Add(-time.Minute),
		Price:     2000,
		SpreadBps: ptr(5),
		Feature: domain.Feature{
			Return1m:           ptr(0.001),
			VolumeRatio5m:      ptr(1.0),
			OrderbookImbalance: ptr(0.1),
			PriceVsVWAPBps:     10,
		},
	}
	curr := domain.Snapshot{
		Timestamp: now,
		Price:     2001,
		SpreadBps: ptr(6),
		Feature: domain.Feature{
			Return1m:           ptr(0.0005),
			VolumeRatio5m:      ptr(1.1),
			OrderbookImbalance: ptr(0.12),
			PriceVsVWAPBps:     11,
		},
	}
	history := []domain.Snapshot{{Price: 1990}, {Price: 2010}}

	sig := eventtrigger.Detect(prev, curr, history, quietThresholds(), false, false)

	if sig.Triggered() {
		t.Fatalf("Triggered() = true, want false (sig = %+v)", sig)
	}
}

// TestDetectEvent_Return1mChangeExceededTriggers covers FR-SCAN-1's "1分
// リターン急変" condition.
func TestDetectEvent_Return1mChangeExceededTriggers(t *testing.T) {
	now := time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)
	prev := domain.Snapshot{Timestamp: now.Add(-time.Minute), Price: 2000, Feature: domain.Feature{Return1m: ptr(0.001), PriceVsVWAPBps: 10}}
	curr := domain.Snapshot{Timestamp: now, Price: 2050, Feature: domain.Feature{Return1m: ptr(0.02), PriceVsVWAPBps: 10}}

	sig := eventtrigger.Detect(prev, curr, nil, quietThresholds(), false, false)

	if !sig.Return1mChangeExceeded {
		t.Errorf("Return1mChangeExceeded = false, want true")
	}
	if !sig.Triggered() {
		t.Errorf("Triggered() = false, want true")
	}
}

// TestDetectEvent_VolumeRatioChangeExceededTriggers covers FR-SCAN-1's
// "出来高急増" condition.
func TestDetectEvent_VolumeRatioChangeExceededTriggers(t *testing.T) {
	now := time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)
	prev := domain.Snapshot{Timestamp: now.Add(-time.Minute), Price: 2000, Feature: domain.Feature{VolumeRatio5m: ptr(1.0), PriceVsVWAPBps: 10}}
	curr := domain.Snapshot{Timestamp: now, Price: 2000, Feature: domain.Feature{VolumeRatio5m: ptr(3.5), PriceVsVWAPBps: 10}}

	sig := eventtrigger.Detect(prev, curr, nil, quietThresholds(), false, false)

	if !sig.VolumeRatioChangeExceeded || !sig.Triggered() {
		t.Errorf("sig = %+v, want VolumeRatioChangeExceeded/Triggered true", sig)
	}
}

// TestDetectEvent_SpreadChangeExceededTriggers covers FR-SCAN-1's
// "スプレッド急拡大" condition.
func TestDetectEvent_SpreadChangeExceededTriggers(t *testing.T) {
	now := time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)
	prev := domain.Snapshot{Timestamp: now.Add(-time.Minute), Price: 2000, SpreadBps: ptr(5), Feature: domain.Feature{PriceVsVWAPBps: 10}}
	curr := domain.Snapshot{Timestamp: now, Price: 2000, SpreadBps: ptr(40), Feature: domain.Feature{PriceVsVWAPBps: 10}}

	sig := eventtrigger.Detect(prev, curr, nil, quietThresholds(), false, false)

	if !sig.SpreadChangeExceeded || !sig.Triggered() {
		t.Errorf("sig = %+v, want SpreadChangeExceeded/Triggered true", sig)
	}
}

// TestDetectEvent_OrderbookImbalanceChangedTriggers covers FR-SCAN-1's
// "板インバランス急変" condition.
func TestDetectEvent_OrderbookImbalanceChangedTriggers(t *testing.T) {
	now := time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)
	prev := domain.Snapshot{Timestamp: now.Add(-time.Minute), Price: 2000, Feature: domain.Feature{OrderbookImbalance: ptr(0.05), PriceVsVWAPBps: 10}}
	curr := domain.Snapshot{Timestamp: now, Price: 2000, Feature: domain.Feature{OrderbookImbalance: ptr(0.8), PriceVsVWAPBps: 10}}

	sig := eventtrigger.Detect(prev, curr, nil, quietThresholds(), false, false)

	if !sig.OrderbookImbalanceChanged || !sig.Triggered() {
		t.Errorf("sig = %+v, want OrderbookImbalanceChanged/Triggered true", sig)
	}
}

// TestDetectEvent_VWAPCrossedTriggers covers FR-SCAN-1's "VWAPクロス"
// condition.
func TestDetectEvent_VWAPCrossedTriggers(t *testing.T) {
	now := time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)
	prev := domain.Snapshot{Timestamp: now.Add(-time.Minute), Price: 1990, Feature: domain.Feature{PriceVsVWAPBps: -5}}
	curr := domain.Snapshot{Timestamp: now, Price: 2010, Feature: domain.Feature{PriceVsVWAPBps: 5}}

	sig := eventtrigger.Detect(prev, curr, nil, quietThresholds(), false, false)

	if !sig.VWAPCrossed || !sig.Triggered() {
		t.Errorf("sig = %+v, want VWAPCrossed/Triggered true", sig)
	}
}

// TestDetectEvent_HighLowBreakTriggers covers FR-SCAN-1's "高値/安値ブレ
// イク" condition.
func TestDetectEvent_HighLowBreakTriggers(t *testing.T) {
	now := time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)
	prev := domain.Snapshot{Timestamp: now.Add(-time.Minute), Price: 2010, Feature: domain.Feature{PriceVsVWAPBps: 10}}
	curr := domain.Snapshot{Timestamp: now, Price: 2100, Feature: domain.Feature{PriceVsVWAPBps: 10}}
	history := []domain.Snapshot{{Price: 1990}, {Price: 2010}, {Price: 2005}}

	sig := eventtrigger.Detect(prev, curr, history, quietThresholds(), false, false)

	if !sig.HighLowBreak || !sig.Triggered() {
		t.Errorf("sig = %+v, want HighLowBreak/Triggered true", sig)
	}
}

// TestDetectEvent_OrderFlowChangeAndNewsFlagPassThrough covers FR-SCAN-1's
// "約定フロー急変"/"ニュースフラグ発生" conditions: caller-supplied since
// this codebase has no tick-level flow classification or news feed yet.
func TestDetectEvent_OrderFlowChangeAndNewsFlagPassThrough(t *testing.T) {
	now := time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)
	prev := domain.Snapshot{Timestamp: now.Add(-time.Minute), Price: 2000, Feature: domain.Feature{PriceVsVWAPBps: 10}}
	curr := domain.Snapshot{Timestamp: now, Price: 2000, Feature: domain.Feature{PriceVsVWAPBps: 10}}

	orderFlow := eventtrigger.Detect(prev, curr, nil, quietThresholds(), true, false)
	if !orderFlow.OrderFlowChange || !orderFlow.Triggered() {
		t.Errorf("orderFlow = %+v, want OrderFlowChange/Triggered true", orderFlow)
	}

	news := eventtrigger.Detect(prev, curr, nil, quietThresholds(), false, true)
	if !news.NewsFlag || !news.Triggered() {
		t.Errorf("news = %+v, want NewsFlag/Triggered true", news)
	}
}

// TestDetectEvent_NilFeatureValuesTreatedAsNoSignal covers FR-FE-2: a
// missing Return1m/VolumeRatio5m/SpreadBps/OrderbookImbalance value must
// not be misread as an exceedance.
func TestDetectEvent_NilFeatureValuesTreatedAsNoSignal(t *testing.T) {
	now := time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)
	prev := domain.Snapshot{Timestamp: now.Add(-time.Minute), Price: 2000, Feature: domain.Feature{PriceVsVWAPBps: 10}}
	curr := domain.Snapshot{Timestamp: now, Price: 2000, Feature: domain.Feature{PriceVsVWAPBps: 10}}

	sig := eventtrigger.Detect(prev, curr, nil, quietThresholds(), false, false)

	if sig.Triggered() {
		t.Fatalf("Triggered() = true, want false (sig = %+v)", sig)
	}
}
