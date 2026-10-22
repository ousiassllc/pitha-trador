package shadowquery

import (
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/jevshadowsettings"
)

// ShadowThrottle is why the shadow stopped because the Jev API answered 429
// (rate limited) or 529 (overloaded): the last such event (fixed codes, no free
// text). The shadow sends nothing until Until; the live calls are not affected.
type ShadowThrottle struct {
	// Date is the JST day of the event and Count the events of that day.
	Date  string `json:"date"`
	Count int    `json:"count"`
	// Status is 429 or 529 and Reason its code (api_rate_limited, api_overloaded).
	Status int    `json:"status"`
	Reason string `json:"reason"`
	// Source is "shadow" (the shadow's own call got it) or "live" (a live call got
	// it and the shadow's next comparison saw it).
	Source string `json:"source"`
	At     string `json:"at"`
	Until  string `json:"until"`
}

// throttleOf converts the stored record; nil for none.
func throttleOf(t *jevshadowsettings.Throttle) *ShadowThrottle {
	if t == nil {
		return nil
	}
	return &ShadowThrottle{Date: t.Date, Count: t.Count, Status: t.Status, Reason: t.Reason, Source: t.Source, At: t.At, Until: t.Until}
}

// secondsOf is d in whole seconds for the JSON.
func secondsOf(d time.Duration) int { return int(d / time.Second) }
