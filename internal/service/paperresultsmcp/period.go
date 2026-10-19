package paperresultsmcp

import (
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// Period limits (API spec §1, §2).
const (
	maxPeriodDays     = 366
	defaultPeriodDays = 30
)

// periodArgs are the from/to arguments every tool takes.
type periodArgs struct {
	From *string `json:"from"`
	To   *string `json:"to"`
}

// resolve turns the arguments into the half-open JST range [from, to). Both
// absent means the last 30 JST days including today (to = tomorrow). One
// alone, a bad format, to <= from, more than 366 days, or to later than
// JST's tomorrow is invalid_period.
func (p periodArgs) resolve(now time.Time) (query.Range, *toolError) {
	tomorrow := midnightJST(now).AddDate(0, 0, 1)
	switch {
	case p.From == nil && p.To == nil:
		return query.Range{From: tomorrow.AddDate(0, 0, -defaultPeriodDays), To: tomorrow}, nil
	case p.From == nil || p.To == nil:
		return query.Range{}, fail(CodeInvalidPeriod)
	}
	from, err := time.ParseInLocation(time.DateOnly, *p.From, query.JST)
	if err != nil {
		return query.Range{}, fail(CodeInvalidPeriod)
	}
	to, err := time.ParseInLocation(time.DateOnly, *p.To, query.JST)
	if err != nil {
		return query.Range{}, fail(CodeInvalidPeriod)
	}
	if !to.After(from) || to.After(tomorrow) || to.After(from.AddDate(0, 0, maxPeriodDays)) {
		return query.Range{}, fail(CodeInvalidPeriod)
	}
	return query.Range{From: from, To: to}, nil
}

func midnightJST(t time.Time) time.Time {
	y, m, d := t.In(query.JST).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, query.JST)
}
