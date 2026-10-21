package jevshadowsettings

import (
	"encoding/json"
	"fmt"
	"time"
)

// ProposedDailyFailureCap is how many failed shadow calls a JST day may have
// before the shadow stops for the day, while it is not changed (it is a
// constant: there is no setting for it).
//
// Why it exists: the cost cap sums request_cost of the calls that were stored,
// but the vendor may bill a call that is never stored (an invalid or unusable
// response, a failed insert, a timeout). A candidate that answers invalidly
// every time would otherwise cost money forever at a recorded cost of 0. A
// failed call is counted here whatever it cost, and the cap bounds that
// unrecorded spend to this many calls a day.
//
// 暫定の案・未承認: this number is a proposal and has NOT been approved. The
// basis is in the pull request of issue #872; this constant is its only
// place, and the Settings screen, the MCP and the docs say "暫定・未承認".
const ProposedDailyFailureCap = 20

// HaltReasonDailyCostCap and HaltReasonDailyFailureCap are the Halt.Reason
// codes of a day stopped by the cost cap and by the failure cap.
const (
	HaltReasonDailyCostCap    = "daily_cost_cap"
	HaltReasonDailyFailureCap = "daily_failure_cap"
)

// Failures is the stored count of the failed shadow calls of one JST day.
type Failures struct {
	Date  string `json:"date"`
	Count int    `json:"count"`
}

// FailuresOn returns the failed calls counted for the JST day day (0 for none
// or another day).
func (f *Failures) FailuresOn(day string) int {
	if f == nil || f.Date != day {
		return 0
	}
	return f.Count
}

// EncodeFailures renders f as the runtime_settings value of KeyFailures.
func EncodeFailures(f Failures) (string, error) {
	b, err := json.Marshal(f)
	if err != nil {
		return "", fmt.Errorf("jevshadowsettings: encode %s: %w", KeyFailures, err)
	}
	return string(b), nil
}

// ParseFailures decodes the runtime_settings value of KeyFailures.
func ParseFailures(jsonValue string) (Failures, error) {
	var f Failures
	if err := json.Unmarshal([]byte(jsonValue), &f); err != nil {
		return Failures{}, fmt.Errorf("jevshadowsettings: decode runtime setting %s=%q: %w", KeyFailures, jsonValue, err)
	}
	if _, err := time.Parse(time.DateOnly, f.Date); err != nil || f.Count < 0 {
		return Failures{}, fmt.Errorf("jevshadowsettings: runtime setting %s=%q needs a date and a count of 0 or more", KeyFailures, jsonValue)
	}
	return f, nil
}

// Halt is the stored record of a day stopped by the cost cap or the failure
// cap.
type Halt struct {
	// Date is the JST day ("YYYY-MM-DD") the shadow is stopped for.
	Date string `json:"date"`
	// Reason is a fixed code (HaltReasonDailyCostCap, HaltReasonDailyFailureCap).
	Reason string `json:"reason"`
	// SpentUSD is the day's shadow cost when the stop happened.
	SpentUSD float64 `json:"spent_usd"`
	// CapUSD is the cost cap in force.
	CapUSD float64 `json:"cap_usd"`
	// Failures and FailureCap are the day's failed calls and the cap on them
	// (HaltReasonDailyFailureCap only).
	Failures   int `json:"failures,omitempty"`
	FailureCap int `json:"failure_cap,omitempty"`
	// At is when the shadow was stopped (RFC 3339, UTC).
	At string `json:"at"`
}

// EncodeHalt renders h as the runtime_settings value of KeyHalt.
func EncodeHalt(h Halt) (string, error) {
	b, err := json.Marshal(h)
	if err != nil {
		return "", fmt.Errorf("jevshadowsettings: encode %s: %w", KeyHalt, err)
	}
	return string(b), nil
}

// ParseHalt decodes the runtime_settings value of KeyHalt.
func ParseHalt(jsonValue string) (Halt, error) {
	var h Halt
	if err := json.Unmarshal([]byte(jsonValue), &h); err != nil {
		return Halt{}, fmt.Errorf("jevshadowsettings: decode runtime setting %s=%q: %w", KeyHalt, jsonValue, err)
	}
	if _, err := time.Parse(time.DateOnly, h.Date); err != nil || h.Reason == "" {
		return Halt{}, fmt.Errorf("jevshadowsettings: runtime setting %s=%q needs a date and a reason", KeyHalt, jsonValue)
	}
	return h, nil
}
