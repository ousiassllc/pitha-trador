package jevtrialsettings

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/jevshadowsettings"
)

// Reasons of a Halt (a stop of one arm, or of the whole trial, for a JST day).
const (
	// HaltReasonDailyCostCap: the overall daily cap was reached (whole trial).
	HaltReasonDailyCostCap = "daily_cost_cap"
	// HaltReasonArmCostCap: the arm's own daily cap was reached.
	HaltReasonArmCostCap = "arm_cost_cap"
	// HaltReasonDailyFailureCap: the arm's failed calls reached DailyFailureCap.
	HaltReasonDailyFailureCap = "daily_failure_cap"
	// HaltReasonPaymentRequired: HTTP 402, the account is out of credit
	// (whole trial: the key is shared).
	HaltReasonPaymentRequired = "payment_required"
	// HaltReasonForbidden: HTTP 403 for this arm (the key, the model or the
	// provider preference is not allowed).
	HaltReasonForbidden = "forbidden"
)

// Halt is the record of a stop for one JST day. The trial (or the arm) runs
// again from the next day. Which of the whole trial or one arm it concerns is
// the key it is stored under (KeyHalt or HaltKey).
type Halt struct {
	Date   string `json:"date"`
	Reason string `json:"reason"`
	// SpentUSD and CapUSD are the counted cost and the cap that was reached
	// (cost-cap reasons).
	SpentUSD float64 `json:"spent_usd,omitempty"`
	CapUSD   float64 `json:"cap_usd,omitempty"`
	// Failures and FailureCap are the day's failed calls and the cap (failure-cap reason).
	Failures   int `json:"failures,omitempty"`
	FailureCap int `json:"failure_cap,omitempty"`
	// Status is the HTTP status behind a 402/403 stop.
	Status int    `json:"status,omitempty"`
	At     string `json:"at"`
}

// EncodeHalt renders h for key.
func EncodeHalt(key string, h Halt) (string, error) { return encode(key, h) }

// ParseHalt decodes the runtime_settings value stored under key.
func ParseHalt(key, jsonValue string) (Halt, error) {
	var h Halt
	if err := json.Unmarshal([]byte(jsonValue), &h); err != nil {
		return Halt{}, fmt.Errorf("jevtrialsettings: decode runtime setting %s=%q: %w", key, jsonValue, err)
	}
	if _, err := time.Parse(time.DateOnly, h.Date); err != nil || h.Reason == "" {
		return Halt{}, fmt.Errorf("jevtrialsettings: runtime setting %s=%q needs a date and a reason", key, jsonValue)
	}
	return h, nil
}

// HaltOn reports whether h is a record of the JST day day.
func (h *Halt) HaltOn(day string) bool { return h != nil && h.Date == day }

// Failures is an arm's failed-call counter of one JST day.
type Failures struct {
	Date  string `json:"date"`
	Count int    `json:"count"`
}

// FailuresOn is the count stored for day (0 for another day or none).
func (f *Failures) FailuresOn(day string) int {
	if f == nil || f.Date != day {
		return 0
	}
	return f.Count
}

// EncodeFailures renders f for key.
func EncodeFailures(key string, f Failures) (string, error) { return encode(key, f) }

// ParseFailures decodes the runtime_settings value stored under key.
func ParseFailures(key, jsonValue string) (Failures, error) {
	var f Failures
	if err := json.Unmarshal([]byte(jsonValue), &f); err != nil {
		return Failures{}, fmt.Errorf("jevtrialsettings: decode runtime setting %s=%q: %w", key, jsonValue, err)
	}
	if _, err := time.Parse(time.DateOnly, f.Date); err != nil || f.Count < 0 {
		return Failures{}, fmt.Errorf("jevtrialsettings: runtime setting %s=%q needs a date and a count of 0 or more", key, jsonValue)
	}
	return f, nil
}

// Throttle is the record of an arm's stop after the API answered 429 or 529:
// the arm waits out ThrottleCooldown and never retries.
type Throttle struct {
	Date   string `json:"date"`
	Count  int    `json:"count"`
	Status int    `json:"status"`
	Reason string `json:"reason"`
	At     string `json:"at"`
	Until  string `json:"until"`
}

// ActiveAt reports whether the cooldown is still running at now.
func (t *Throttle) ActiveAt(now time.Time) bool {
	if t == nil {
		return false
	}
	until, err := time.Parse(time.RFC3339, t.Until)
	return err == nil && now.Before(until)
}

// CountOn is the number of stops recorded for day.
func (t *Throttle) CountOn(day string) int {
	if t == nil || t.Date != day {
		return 0
	}
	return t.Count
}

// ThrottleReason is the reason of a 429/529 status, "" for any other.
func ThrottleReason(status int) string { return jevshadowsettings.ThrottleReason(status) }

// IsThrottleStatus reports whether status is 429 or 529.
func IsThrottleStatus(status int) bool { return ThrottleReason(status) != "" }

// EncodeThrottle renders t for key.
func EncodeThrottle(key string, t Throttle) (string, error) { return encode(key, t) }

// ParseThrottle decodes the runtime_settings value stored under key.
func ParseThrottle(key, jsonValue string) (Throttle, error) {
	var t Throttle
	if err := json.Unmarshal([]byte(jsonValue), &t); err != nil {
		return Throttle{}, fmt.Errorf("jevtrialsettings: decode runtime setting %s=%q: %w", key, jsonValue, err)
	}
	_, dateErr := time.Parse(time.DateOnly, t.Date)
	_, atErr := time.Parse(time.RFC3339, t.At)
	_, untilErr := time.Parse(time.RFC3339, t.Until)
	if dateErr != nil || atErr != nil || untilErr != nil || t.Count < 1 || ThrottleReason(t.Status) != t.Reason {
		return Throttle{}, fmt.Errorf("jevtrialsettings: runtime setting %s=%q needs a date, a count of 1 or more, a 429/529 status with its reason and the times", key, jsonValue)
	}
	return t, nil
}

func encode(key string, v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("jevtrialsettings: encode %s: %w", key, err)
	}
	return string(b), nil
}
