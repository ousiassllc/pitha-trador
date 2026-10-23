package jevshadowsettings_test

import (
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/jevshadowsettings"
)

func TestThrottleRoundTripAndActivity(t *testing.T) {
	in := jevshadowsettings.Throttle{
		Date: "2026-10-22", Count: 2, Status: 429, Reason: jevshadowsettings.ThrottleReasonRateLimited, Source: jevshadowsettings.ThrottleSourceShadow,
		At: "2026-10-22T03:00:00Z", Until: "2026-10-22T03:15:00Z",
	}
	value, err := jevshadowsettings.EncodeThrottle(in)
	if err != nil {
		t.Fatal(err)
	}
	out, err := jevshadowsettings.ParseThrottle(value)
	if err != nil || out != in {
		t.Fatalf("round trip = %+v, %v", out, err)
	}
	at := time.Date(2026, 10, 22, 3, 14, 59, 0, time.UTC)
	if !out.ActiveAt(at) || out.ActiveAt(at.Add(time.Second)) {
		t.Error("the stop must last until Until, exclusive")
	}
	var none *jevshadowsettings.Throttle
	if none.ActiveAt(at) || none.CountOn("2026-10-22") != 0 || out.CountOn("2026-10-21") != 0 || out.CountOn("2026-10-22") != 2 {
		t.Error("nil is never active; the count is of its own day only")
	}
}

func TestParseThrottleRejectsDamagedRecords(t *testing.T) {
	for name, value := range map[string]string{
		"not json":       `x`,
		"bad date":       `{"date":"x","count":1,"status":429,"reason":"api_rate_limited","source":"live","at":"2026-10-22T03:00:00Z","until":"2026-10-22T03:15:00Z"}`,
		"zero count":     `{"date":"2026-10-22","count":0,"status":429,"reason":"api_rate_limited","source":"live","at":"2026-10-22T03:00:00Z","until":"2026-10-22T03:15:00Z"}`,
		"status 500":     `{"date":"2026-10-22","count":1,"status":500,"reason":"api_rate_limited","source":"live","at":"2026-10-22T03:00:00Z","until":"2026-10-22T03:15:00Z"}`,
		"reason differs": `{"date":"2026-10-22","count":1,"status":429,"reason":"api_overloaded","source":"live","at":"2026-10-22T03:00:00Z","until":"2026-10-22T03:15:00Z"}`,
		"unknown source": `{"date":"2026-10-22","count":1,"status":429,"reason":"api_rate_limited","source":"x","at":"2026-10-22T03:00:00Z","until":"2026-10-22T03:15:00Z"}`,
		"bad until":      `{"date":"2026-10-22","count":1,"status":429,"reason":"api_rate_limited","source":"live","at":"2026-10-22T03:00:00Z","until":"soon"}`,
	} {
		if _, err := jevshadowsettings.ParseThrottle(value); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestThrottleReason(t *testing.T) {
	if jevshadowsettings.ThrottleReason(429) != "api_rate_limited" || jevshadowsettings.ThrottleReason(529) != "api_overloaded" {
		t.Error("429 and 529 have their codes")
	}
	for _, status := range []int{200, 401, 422, 500, 503} {
		if r := jevshadowsettings.ThrottleReason(status); r != "" {
			t.Errorf("status %d has a throttle reason %q", status, r)
		}
	}
}
