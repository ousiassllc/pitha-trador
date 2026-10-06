package eventtrigger_test

import (
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/featureengine/eventtrigger"
	"github.com/ousiassllc/pitha-trador/internal/service/marketcalendar"
)

func jst(day, h, m, s int) time.Time {
	return time.Date(2026, 10, day, h, m, s, 0, marketcalendar.JST)
}

// jumpy returns a prev/curr pair where every prev-comparing signal would
// fire if the two bars were consecutive (issue #644).
func jumpy(prevAt, currAt time.Time) (domain.Snapshot, domain.Snapshot) {
	prev := domain.Snapshot{Timestamp: prevAt, Price: 1990, SpreadBps: ptr(2), Feature: domain.Feature{
		VWAP: 2000, PriceVsVWAPBps: -50, OrderbookImbalance: ptr(-0.5), TradeFlowImbalance: ptr(-0.5),
	}}
	curr := domain.Snapshot{Timestamp: currAt, Price: 2010, SpreadBps: ptr(40), Feature: domain.Feature{
		VWAP: 2000, PriceVsVWAPBps: 50, OrderbookImbalance: ptr(0.5), TradeFlowImbalance: ptr(0.5),
	}}
	return prev, curr
}

func TestDetectEvent_ConsecutiveBarsOfOneSessionStillFire(t *testing.T) {
	prev, curr := jumpy(jst(6, 10, 0, 0), jst(6, 10, 1, 0))
	sig := eventtrigger.Detect(prev, curr, nil, quietThresholds(), false)
	if !sig.SpreadChangeExceeded || !sig.OrderbookImbalanceChanged || !sig.OrderFlowChange || !sig.VWAPCrossed {
		t.Errorf("sig = %+v, want every prev-comparing signal true", sig)
	}
}

func TestDetectEvent_PrevGapSuppressesPrevComparisons(t *testing.T) {
	tests := []struct {
		name         string
		prevAt, curr time.Time
	}{
		{"lunch break 11:29 to 12:30", jst(6, 11, 29, 0), jst(6, 12, 30, 0)},
		{"previous business day close to the open", jst(5, 15, 29, 0), jst(6, 9, 0, 0)},
		{"10 minute missing stretch", jst(6, 10, 0, 0), jst(6, 10, 10, 0)},
		{"just over 90 seconds", jst(6, 10, 0, 0), jst(6, 10, 1, 31)},
		{"prev newer than curr", jst(6, 10, 1, 0), jst(6, 10, 0, 0)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prev, curr := jumpy(tt.prevAt, tt.curr)
			sig := eventtrigger.Detect(prev, curr, nil, quietThresholds(), false)
			if sig.SpreadChangeExceeded || sig.OrderbookImbalanceChanged || sig.OrderFlowChange || sig.VWAPCrossed {
				t.Errorf("sig = %+v, want every prev-comparing signal false", sig)
			}
		})
	}
}

func TestDetectEvent_PrevGapAtToleranceStillFires(t *testing.T) {
	prev, curr := jumpy(jst(6, 10, 0, 0), jst(6, 10, 1, 30))
	if sig := eventtrigger.Detect(prev, curr, nil, quietThresholds(), false); !sig.VWAPCrossed {
		t.Errorf("sig = %+v, want VWAPCrossed at exactly MaxPrevGap", sig)
	}
}

func TestDetectEvent_MissingVWAPIsNotACross(t *testing.T) {
	for name, mutate := range map[string]func(prev, curr *domain.Snapshot){
		"curr without VWAP": func(_, c *domain.Snapshot) { c.Feature.VWAP, c.Feature.PriceVsVWAPBps = 0, 0 },
		"prev without VWAP": func(p, _ *domain.Snapshot) { p.Feature.VWAP, p.Feature.PriceVsVWAPBps = 0, 0 },
	} {
		t.Run(name, func(t *testing.T) {
			prev, curr := jumpy(jst(6, 10, 0, 0), jst(6, 10, 1, 0))
			prev.Feature.PriceVsVWAPBps = -50
			curr.Feature.PriceVsVWAPBps = -50
			mutate(&prev, &curr)
			if sig := eventtrigger.Detect(prev, curr, nil, quietThresholds(), false); sig.VWAPCrossed {
				t.Errorf("sig = %+v, want VWAPCrossed false with a missing VWAP", sig)
			}
		})
	}
}

func TestDetectEvent_HighLowBreakUsesOnlyCurrentSession(t *testing.T) {
	bar := func(at time.Time, price float64) domain.Snapshot { return domain.Snapshot{Timestamp: at, Price: price} }
	// Yesterday's and the morning session's range (1000-3000) would swallow
	// 2100; the afternoon session so far only spans 2050-2080.
	history := []domain.Snapshot{
		bar(jst(6, 12, 31, 0), 2080),
		bar(jst(6, 12, 30, 0), 2050),
		bar(jst(6, 11, 29, 0), 3000),
		bar(jst(5, 15, 29, 0), 1000),
	}
	prev := bar(jst(6, 12, 31, 0), 2080)
	curr := bar(jst(6, 12, 32, 0), 2100)

	if sig := eventtrigger.Detect(prev, curr, history, quietThresholds(), false); !sig.HighLowBreak {
		t.Errorf("sig = %+v, want HighLowBreak above the afternoon session's high", sig)
	}
	curr.Price = 2060
	if sig := eventtrigger.Detect(prev, curr, history, quietThresholds(), false); sig.HighLowBreak {
		t.Errorf("sig = %+v, want no HighLowBreak inside the afternoon range", sig)
	}
}

func TestDetectEvent_FirstBarOfSessionIsNoBreakout(t *testing.T) {
	history := []domain.Snapshot{{Timestamp: jst(5, 15, 29, 0), Price: 1000}}
	prev := domain.Snapshot{Timestamp: jst(5, 15, 29, 0), Price: 1000}
	curr := domain.Snapshot{Timestamp: jst(6, 9, 0, 0), Price: 2000}
	if sig := eventtrigger.Detect(prev, curr, history, quietThresholds(), false); sig.HighLowBreak {
		t.Errorf("sig = %+v, want no HighLowBreak without same-session history", sig)
	}
}
