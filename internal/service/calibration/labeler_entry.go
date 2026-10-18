package calibration

import (
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// ResponseTime is when Jev's response was received: the decision row is
// written right after the call returns, so its created_at, never earlier
// than the decision's bar timestamp.
func ResponseTime(d domain.JevDecision) time.Time {
	if d.CreatedAt.After(d.Timestamp) {
		return d.CreatedAt
	}
	return d.Timestamp
}

// FindEntry returns the index of the first bar within EntryWaitTolerance of
// responseAt that carries the executable entry quote, and that quote: the
// ask for LONG, the bid for SHORT, the bar's last price for a NONE decision
// (no side to price). It returns -1 when there is none.
func FindEntry(window []domain.Snapshot, direction string, responseAt time.Time) (int, float64) {
	for i, bar := range window {
		if bar.Timestamp.Sub(responseAt) > EntryWaitTolerance {
			break
		}
		quote := bar.Price
		switch direction {
		case domain.JevDirectionLong:
			quote = ptrOrZero(bar.Ask)
		case domain.JevDirectionShort:
			quote = ptrOrZero(bar.Bid)
		}
		if quote > 0 {
			return i, quote
		}
	}
	return -1, 0
}

func ptrOrZero(v *float64) float64 {
	if v == nil {
		return 0
	}
	return *v
}
