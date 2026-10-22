package shadowtests

import (
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config/jevshadowsettings"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query/shadowquery"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

// jev_shadow_cost shows the Jev API's 429/529 stop of the shadow: whether it is
// active, why (status, reason, whose call), until when, how often today, and the
// fixed cooldown and call interval (issue #883).
func TestShadowCostShowsTheThrottleStop(t *testing.T) {
	srv, w := shadowServer(t)

	var none shadowquery.ShadowCostStatus
	srv.call(t, "jev_shadow_cost", map[string]any{}).data(t, "jev_shadow_cost", &none)
	if none.ThrottleActive || none.LastThrottle != nil || none.ThrottlesToday != 0 {
		t.Errorf("nothing stored: active %v last %+v today %d, want none", none.ThrottleActive, none.LastThrottle, none.ThrottlesToday)
	}
	if none.ThrottleCooldownSeconds != 900 || none.MinCallIntervalSeconds != 2 {
		t.Errorf("cooldown %ds interval %ds, want 900 and 2", none.ThrottleCooldownSeconds, none.MinCallIntervalSeconds)
	}

	// FixedNow is 2026-10-18 12:00 JST (03:00Z): a stop at 11:55 JST lasts to 12:10 JST.
	testkit.Setting(t, w, jevshadowsettings.KeyThrottle, `{"date":"2026-10-18","count":3,"status":529,"reason":"api_overloaded","source":"live","at":"2026-10-18T02:55:00Z","until":"2026-10-18T03:10:00Z"}`)
	var active shadowquery.ShadowCostStatus
	srv.call(t, "jev_shadow_cost", map[string]any{}).data(t, "jev_shadow_cost", &active)
	l := active.LastThrottle
	if !active.ThrottleActive || active.ThrottlesToday != 3 || l == nil || l.Status != 529 || l.Reason != "api_overloaded" || l.Source != "live" || l.Until != "2026-10-18T03:10:00Z" {
		t.Errorf("active stop: active %v today %d last %+v", active.ThrottleActive, active.ThrottlesToday, l)
	}

	// Over (until is in the past) and of an earlier day: not active, not counted for today, still the last one.
	testkit.Setting(t, w, jevshadowsettings.KeyThrottle, `{"date":"2026-10-17","count":1,"status":429,"reason":"api_rate_limited","source":"shadow","at":"2026-10-17T02:00:00Z","until":"2026-10-17T02:15:00Z"}`)
	var past shadowquery.ShadowCostStatus
	srv.call(t, "jev_shadow_cost", map[string]any{}).data(t, "jev_shadow_cost", &past)
	if past.ThrottleActive || past.ThrottlesToday != 0 || past.LastThrottle == nil || past.LastThrottle.Reason != "api_rate_limited" || past.LastThrottle.Source != "shadow" {
		t.Errorf("finished stop: active %v today %d last %+v", past.ThrottleActive, past.ThrottlesToday, past.LastThrottle)
	}
}
