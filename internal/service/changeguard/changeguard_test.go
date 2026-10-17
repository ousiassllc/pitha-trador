package changeguard_test

import (
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/changeguard"
	"github.com/ousiassllc/pitha-trador/internal/service/marketcalendar"
)

func jst(month time.Month, day, hour int) time.Time {
	return time.Date(2026, month, day, hour, 0, 0, 0, marketcalendar.JST)
}

func TestTradingDaysSince(t *testing.T) {
	// 2026-10-13 is a Tuesday; the weekend and the 10-12 holiday are not counted.
	for name, tc := range map[string]struct {
		from, to time.Time
		want     int
	}{
		"same day":                {jst(10, 14, 9), jst(10, 14, 23), 0},
		"next day":                {jst(10, 14, 18), jst(10, 15, 0), 1},
		"across a weekend":        {jst(10, 16, 18), jst(10, 19, 9), 1}, // Fri -> Mon
		"a full trading week":     {jst(10, 14, 18), jst(10, 21, 9), 5}, // Wed -> next Wed
		"skips the 10-12 holiday": {jst(10, 9, 18), jst(10, 13, 9), 1},  // Fri -> Tue (Mon is a holiday)
		"to before from":          {jst(10, 15, 9), jst(10, 14, 9), 0},
	} {
		if got := changeguard.TradingDaysSince(tc.from, tc.to); got != tc.want {
			t.Errorf("%s: TradingDaysSince = %d, want %d", name, got, tc.want)
		}
	}
}

func TestLimits_ModelAdoption(t *testing.T) {
	limits := changeguard.DefaultLimits()
	now := jst(10, 19, 18) // Monday

	if b := limits.CheckModelAdoption(now, changeguard.History{}); b != nil {
		t.Errorf("no history: blocked %+v", b)
	}
	// A swap 2 trading days ago is rate limited; the block names when it ends.
	b := limits.CheckModelAdoption(now, changeguard.History{LastModelAdoption: jst(10, 15, 18)})
	if b == nil || b.Reason != changeguard.ReasonModelSwapRateLimited {
		t.Fatalf("block = %+v, want model_swap_rate_limited", b)
	}
	if want := jst(10, 22, 0); !b.Until.Equal(want) {
		t.Errorf("Until = %v, want %v (5th trading day after the swap)", b.Until, want)
	}
	if b.Detail == "" {
		t.Error("the block has no detail")
	}
	// Five trading days later it is allowed again.
	if b := limits.CheckModelAdoption(jst(10, 22, 18), changeguard.History{LastModelAdoption: jst(10, 15, 18)}); b != nil {
		t.Errorf("after the window: blocked %+v", b)
	}
	// A policy change 1 trading day ago freezes the adoption; 3 days later it is free.
	h := changeguard.History{LastPolicyChange: jst(10, 16, 15)}
	if b := limits.CheckModelAdoption(jst(10, 19, 18), h); b == nil || b.Reason != changeguard.ReasonPolicyChangeFreeze {
		t.Errorf("block = %+v, want policy_change_freeze", b)
	}
	if b := limits.CheckModelAdoption(jst(10, 21, 18), h); b != nil {
		t.Errorf("after the freeze: blocked %+v", b)
	}
}

func TestLimits_PolicyChange(t *testing.T) {
	limits := changeguard.DefaultLimits()
	now := jst(10, 19, 15)

	if b := limits.CheckPolicyChange(now, changeguard.History{LastPolicyChange: jst(10, 16, 15)}); b == nil || b.Reason != changeguard.ReasonPolicyChangeRateLimited {
		t.Errorf("block = %+v, want policy_change_rate_limited", b)
	}
	if b := limits.CheckPolicyChange(now, changeguard.History{LastModelAdoption: jst(10, 16, 18)}); b == nil || b.Reason != changeguard.ReasonModelAdoptionFreeze {
		t.Errorf("block = %+v, want model_adoption_freeze", b)
	}
	// A model adopted 3 trading days ago no longer freezes policy changes, and a
	// change 5 trading days ago no longer rate limits them.
	h := changeguard.History{LastModelAdoption: jst(10, 14, 18), LastPolicyChange: jst(10, 9, 15)}
	if b := limits.CheckPolicyChange(now, h); b != nil {
		t.Errorf("after both windows: blocked %+v", b)
	}
}

func TestLimits_ZeroDaysDisableTheRule(t *testing.T) {
	now := jst(10, 19, 18)
	h := changeguard.History{LastModelAdoption: now, LastPolicyChange: now}
	if b := (changeguard.Limits{}).CheckModelAdoption(now, h); b != nil {
		t.Errorf("zero limits blocked %+v", b)
	}
	if b := (changeguard.Limits{}).CheckPolicyChange(now, h); b != nil {
		t.Errorf("zero limits blocked %+v", b)
	}
}
