package changeguard

import (
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/marketcalendar"
)

// Reasons of a Block.
const (
	// ReasonModelSwapRateLimited: a model was adopted less than ModelSwapDays
	// trading days ago.
	ReasonModelSwapRateLimited = "model_swap_rate_limited"
	// ReasonPolicyChangeFreeze: a policy.* change was applied or rolled back
	// less than FreezeDays trading days ago, so no model is adopted yet.
	ReasonPolicyChangeFreeze = "policy_change_freeze"
	// ReasonPolicyChangeRateLimited: a policy.* change was applied less than
	// PolicyChangeDays trading days ago.
	ReasonPolicyChangeRateLimited = "policy_change_rate_limited"
	// ReasonModelAdoptionFreeze: a model was adopted less than FreezeDays
	// trading days ago, so no policy.* change is applied yet.
	ReasonModelAdoptionFreeze = "model_adoption_freeze"
)

// Limits are the windows, in TSE trading days. The defaults give the metamodel
// two nightly attempts between two policy changes (policy changes are at
// least PolicyChangeDays apart and the freeze is shorter), so neither flow can
// starve the other.
type Limits struct {
	// ModelSwapDays is the least number of trading days between two model adoptions.
	ModelSwapDays int
	// PolicyChangeDays is the least number of trading days between two policy.* changes.
	PolicyChangeDays int
	// FreezeDays is the least number of trading days between a change of one
	// kind and a change of the other.
	FreezeDays int
}

// DefaultLimits returns 5 trading days (one week, also FR-SELFIMPROVE-6's
// post-apply tracking window) between changes of the same kind and 3 between
// changes of different kinds.
func DefaultLimits() Limits {
	return Limits{ModelSwapDays: 5, PolicyChangeDays: 5, FreezeDays: 3}
}

// History is when each kind of change last happened; the zero time means never.
type History struct {
	LastModelAdoption time.Time
	LastPolicyChange  time.Time
}

// Block is the reason a change is not allowed now.
type Block struct {
	Reason string
	Detail string
	// Until is the first instant (JST midnight of a trading day) the change is allowed.
	Until time.Time
}

// CheckModelAdoption returns why a model may not be adopted at now, or nil.
// The first-ever adoption (no earlier model) is not rate limited, only frozen.
func (l Limits) CheckModelAdoption(now time.Time, h History) *Block {
	if b := blockWithin(now, h.LastModelAdoption, l.ModelSwapDays, ReasonModelSwapRateLimited, "the last model adoption"); b != nil {
		return b
	}
	return blockWithin(now, h.LastPolicyChange, l.FreezeDays, ReasonPolicyChangeFreeze, "the last policy.* change")
}

// CheckPolicyChange returns why a policy.* change may not be applied at now, or nil.
func (l Limits) CheckPolicyChange(now time.Time, h History) *Block {
	if b := blockWithin(now, h.LastPolicyChange, l.PolicyChangeDays, ReasonPolicyChangeRateLimited, "the last policy.* change"); b != nil {
		return b
	}
	return blockWithin(now, h.LastModelAdoption, l.FreezeDays, ReasonModelAdoptionFreeze, "the last model adoption")
}

// blockWithin blocks while fewer than days trading days passed since last.
func blockWithin(now, last time.Time, days int, reason, what string) *Block {
	if last.IsZero() || days <= 0 {
		return nil
	}
	passed := TradingDaysSince(last, now)
	if passed >= days {
		return nil
	}
	return &Block{
		Reason: reason,
		Detail: fmt.Sprintf("%d of %d trading days passed since %s (%s)", passed, days, what, last.UTC().Format(time.RFC3339)),
		Until:  untilAfter(last, days),
	}
}

// TradingDaysSince counts the TSE trading days (by JST date) in (from, to]:
// the number of whole trading days that have started since from. It is 0 when
// to is not after from's JST date.
func TradingDaysSince(from, to time.Time) int {
	d := midnight(from)
	end := midnight(to)
	n := 0
	for d.Before(end) {
		d = d.AddDate(0, 0, 1)
		if marketcalendar.TSE.IsTradingDay(d) {
			n++
		}
	}
	return n
}

// untilAfter is the JST midnight of the days-th trading day after last.
func untilAfter(last time.Time, days int) time.Time {
	d := midnight(last)
	for n := 0; n < days; {
		d = d.AddDate(0, 0, 1)
		if marketcalendar.TSE.IsTradingDay(d) {
			n++
		}
	}
	return d
}

func midnight(t time.Time) time.Time {
	t = t.In(marketcalendar.JST)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, marketcalendar.JST)
}
