package watchsettings

import (
	"encoding/json"
	"fmt"
	"math"
	"time"
)

// JST is the fixed Asia/Tokyo offset: the observation cap's "day" is a JST day,
// like the shadow's and the trading session.
var JST = time.FixedZone("JST", 9*60*60)

// Day returns t's JST calendar day as "YYYY-MM-DD".
func Day(t time.Time) string { return t.In(JST).Format(time.DateOnly) }

// Usage is the stored record of the observation Jev calls of one JST day.
type Usage struct {
	// Date is the JST day ("YYYY-MM-DD") the counters belong to.
	Date string `json:"date"`
	// Calls is how many observation Scout calls completed.
	Calls int `json:"calls"`
	// Failures is how many observation Scout calls failed.
	Failures int `json:"failures"`
	// CostUSD is the estimated cost of the completed calls
	// (see internal/service/jev/observation).
	CostUSD float64 `json:"cost_usd"`
}

// On returns u as the counters of day: u itself when it is day's record, the
// zero Usage of day otherwise (a new JST day starts from zero).
func (u Usage) On(day string) Usage {
	if u.Date == day {
		return u
	}
	return Usage{Date: day}
}

// EncodeUsage renders u as the runtime_settings value of KeyObservationUsage.
func EncodeUsage(u Usage) (string, error) {
	b, err := json.Marshal(u)
	if err != nil {
		return "", fmt.Errorf("watchsettings: encode %s: %w", KeyObservationUsage, err)
	}
	return string(b), nil
}

// ParseUsage decodes the runtime_settings value of KeyObservationUsage.
func ParseUsage(jsonValue string) (Usage, error) {
	var u Usage
	if err := json.Unmarshal([]byte(jsonValue), &u); err != nil {
		return Usage{}, fmt.Errorf("watchsettings: decode runtime setting %s=%q: %w", KeyObservationUsage, jsonValue, err)
	}
	if _, err := time.Parse(time.DateOnly, u.Date); err != nil || u.Calls < 0 || u.Failures < 0 || u.CostUSD < 0 || math.IsNaN(u.CostUSD) {
		return Usage{}, fmt.Errorf("watchsettings: runtime setting %s=%q needs a date and counters of 0 or more", KeyObservationUsage, jsonValue)
	}
	return u, nil
}
