package jevshadowsettings

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// ThrottleCooldown is how long the shadow stays stopped after the Jev API
// answered one of its calls with 429 (rate limited) or 529 (overloaded). It is a
// constant: there is no setting for it (issue #883).
//
// Why a stop and not a wait: the API's limits are per organization, in requests
// and tokens per second (docs.typesafe.ai/models, "Rate limits"), so the live
// Scout/Trader and the shadow draw on the same budget. A 429 on a shadow call
// means the budget is exhausted; the shadow is the only caller that can give way
// without changing a live decision, so it stops sending and does not retry.
// 15 minutes is the implementer's choice (issue #883): long enough that a
// saturated or overloaded API is left alone for the live calls, short enough
// that a one-off 429 does not cost the day's comparisons. After it the shadow
// sends one call again; a second 429 stops it for another cooldown.
const ThrottleCooldown = 15 * time.Minute

// MinCallInterval is the least time between two shadow calls to the Jev API
// (a constant: there is no setting for it). A comparison that would start
// sooner is dropped, never delayed, so the shadow adds at most one request per
// interval (0.5 requests a second) to the live traffic on the shared key. 2
// seconds is the implementer's choice (issue #883).
const MinCallInterval = 2 * time.Second

// ThrottleSourceShadow and ThrottleSourceLive are the Throttle.Source codes: the
// 429/529 answered a shadow call, or a live (non-shadow) Jev call that the
// shadow's next comparison saw.
const (
	ThrottleSourceShadow = "shadow"
	ThrottleSourceLive   = "live"
)

// ThrottleReasonRateLimited and ThrottleReasonOverloaded are the Throttle.Reason
// codes of a stop by 429 and by 529.
const (
	ThrottleReasonRateLimited = "api_rate_limited"
	ThrottleReasonOverloaded  = "api_overloaded"
)

// statusOverloaded is the API's 529 (net/http has no constant for it).
const statusOverloaded = 529

// ThrottleReason returns the Throttle.Reason code of an HTTP status, "" for a
// status that is neither 429 nor 529.
func ThrottleReason(status int) string {
	switch status {
	case http.StatusTooManyRequests:
		return ThrottleReasonRateLimited
	case statusOverloaded:
		return ThrottleReasonOverloaded
	}
	return ""
}

// Throttle is the stored record of the last time the Jev API answered a shadow
// call with 429 or 529. The shadow sends nothing until Until.
type Throttle struct {
	// Date is the JST day ("YYYY-MM-DD") of the last event and Count the events
	// of that day (the number of times the shadow was stopped by the API).
	Date  string `json:"date"`
	Count int    `json:"count"`
	// Status is the HTTP status of the last event (429 or 529) and Reason its
	// fixed code (ThrottleReason*).
	Status int    `json:"status"`
	Reason string `json:"reason"`
	// Source says whose call got the 429/529 (ThrottleSource*).
	Source string `json:"source"`
	// At is when the last event happened and Until when the shadow may send
	// again (RFC 3339, UTC).
	At    string `json:"at"`
	Until string `json:"until"`
}

// ActiveAt reports whether the shadow is stopped by this record at now.
func (t *Throttle) ActiveAt(now time.Time) bool {
	if t == nil {
		return false
	}
	until, err := time.Parse(time.RFC3339, t.Until)
	return err == nil && now.Before(until)
}

// CountOn returns the events counted for the JST day day (0 for none or
// another day).
func (t *Throttle) CountOn(day string) int {
	if t == nil || t.Date != day {
		return 0
	}
	return t.Count
}

// EncodeThrottle renders t as the runtime_settings value of KeyThrottle.
func EncodeThrottle(t Throttle) (string, error) {
	b, err := json.Marshal(t)
	if err != nil {
		return "", fmt.Errorf("jevshadowsettings: encode %s: %w", KeyThrottle, err)
	}
	return string(b), nil
}

// ParseThrottle decodes the runtime_settings value of KeyThrottle.
func ParseThrottle(jsonValue string) (Throttle, error) {
	var t Throttle
	if err := json.Unmarshal([]byte(jsonValue), &t); err != nil {
		return Throttle{}, fmt.Errorf("jevshadowsettings: decode runtime setting %s=%q: %w", KeyThrottle, jsonValue, err)
	}
	_, dateErr := time.Parse(time.DateOnly, t.Date)
	_, atErr := time.Parse(time.RFC3339, t.At)
	_, untilErr := time.Parse(time.RFC3339, t.Until)
	if dateErr != nil || atErr != nil || untilErr != nil || t.Count < 1 || ThrottleReason(t.Status) != t.Reason || (t.Source != ThrottleSourceShadow && t.Source != ThrottleSourceLive) {
		return Throttle{}, fmt.Errorf("jevshadowsettings: runtime setting %s=%q needs a date, a count of 1 or more, a 429/529 status with its reason, a source and the times", KeyThrottle, jsonValue)
	}
	return t, nil
}
