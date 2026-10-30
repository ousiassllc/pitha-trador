package disclosureskip

import (
	"slices"
	"time"
)

// RuleVersion identifies the rule's definition (kindRules, the window and the
// fetch conditions below); it is stored with each record.
const RuleVersion = 1

// FetchLag is how long after the decision a fetch must have started to count
// as covering it. The やのしん index is unofficial and may list a disclosure
// some time after it was published; a fetch seconds after t would then call a
// decision "no skip" for a disclosure the index had not listed yet. The value
// is a guard, not a measured lag.
const FetchLag = 5 * time.Minute

// windowAnchorHour is the JST hour (15:00) of the preceding trading day from
// which its disclosures count.
const windowAnchorHour = 15

// maxLookback bounds the search for the preceding trading day. The longest
// TSE closure is well under two weeks.
const maxLookback = 31

var jst = time.FixedZone("JST", 9*60*60)

// Calendar is the TSE trading calendar (marketcalendar.Calendar).
type Calendar interface {
	IsTradingDay(t time.Time) bool
}

// Disclosure is one stored disclosure of the feed's index.
type Disclosure struct {
	Symbol      string
	ID          string
	PublishedAt time.Time
	Title       string
	URL         string
}

// Fetch is one News Ingest fetch of a symbol, as the recorder saw it.
type Fetch struct {
	Symbol string
	// At is when the request started.
	At time.Time
	// OK is false for a failed fetch.
	OK bool
	// Truncated says the response reached the request's item limit, so older
	// disclosures may have been cut off.
	Truncated bool
	// Undated is how many items had no readable publication time.
	Undated int
	// Oldest is the oldest publication time of the response (zero when none).
	Oldest time.Time
}

// Verdict is the rule's answer for one decision.
type Verdict string

// The three answers. Unknown is not "no skip": it says the feed's coverage
// does not allow an answer.
const (
	VerdictSkip    Verdict = "skip"
	VerdictNoSkip  Verdict = "no_skip"
	VerdictUnknown Verdict = "unknown"
)

// UnknownReason says why a verdict is unknown.
type UnknownReason string

// The reasons.
const (
	// ReasonNoFetch: no successful fetch of the symbol started at least FetchLag after the decision.
	ReasonNoFetch UnknownReason = "no_fetch"
	// ReasonIncompleteFetch: successful fetches exist, but each was cut off by
	// the item limit before reaching the window's start, or had an undated item.
	ReasonIncompleteFetch UnknownReason = "incomplete_fetch"
)

// Match is a disclosure the rule matched.
type Match struct {
	Disclosure
	Kind Kind
}

// Result is the rule's output.
type Result struct {
	Verdict Verdict
	// Reason is set for VerdictUnknown only.
	Reason UnknownReason
	// WindowFrom is the window's start (the window is [WindowFrom, t]).
	WindowFrom time.Time
	// Matches are the matching disclosures, oldest first (VerdictSkip only).
	Matches []Match
	// CoveredBy is the start of the fetch that covered the window; nil when none did.
	CoveredBy *time.Time
}

// WindowStart is 15:00 JST of the TSE trading day preceding t's JST date. The
// rule's window is [WindowStart, t]: t's own day and the evening of the trading
// day before it, across any weekend or holiday in between.
func WindowStart(cal Calendar, t time.Time) time.Time {
	t = t.In(jst)
	day := time.Date(t.Year(), t.Month(), t.Day(), windowAnchorHour, 0, 0, 0, jst)
	for range maxLookback {
		day = day.AddDate(0, 0, -1)
		if cal.IsTradingDay(day) {
			return day
		}
	}
	return day
}

// Evaluate applies the rule to the decision of symbol at t.
//
// A disclosure matches when it is of the symbol, has a publication time in
// [WindowStart(t), t] (never after t) and its title is of a covered Kind. Any
// match is a skip, whatever the coverage: a disclosure that is known to exist
// is not undone by missing data. Without a match the window must have been
// covered by a fetch of the symbol that started at or after t+FetchLag, was
// successful, had no undated item and either was not cut off by the item limit
// or reached back before the window's start; otherwise the verdict is unknown.
func Evaluate(cal Calendar, symbol string, t time.Time, disclosures []Disclosure, fetches []Fetch) Result {
	from := WindowStart(cal, t)
	res := Result{WindowFrom: from}

	for _, d := range disclosures {
		if d.Symbol != symbol || d.PublishedAt.IsZero() || d.PublishedAt.After(t) || d.PublishedAt.Before(from) {
			continue
		}
		if kind, ok := Classify(d.Title); ok {
			res.Matches = append(res.Matches, Match{Disclosure: d, Kind: kind})
		}
	}
	slices.SortStableFunc(res.Matches, func(a, b Match) int { return a.PublishedAt.Compare(b.PublishedAt) })

	covered, anySuccess := coverage(symbol, t, from, fetches)
	res.CoveredBy = covered
	switch {
	case len(res.Matches) > 0:
		res.Verdict = VerdictSkip
	case covered != nil:
		res.Verdict = VerdictNoSkip
	case anySuccess:
		res.Verdict, res.Reason = VerdictUnknown, ReasonIncompleteFetch
	default:
		res.Verdict, res.Reason = VerdictUnknown, ReasonNoFetch
	}
	return res
}

// coverage returns the start of the earliest fetch that covers the window
// (nil when none) and whether any successful fetch started late enough.
func coverage(symbol string, t, from time.Time, fetches []Fetch) (covered *time.Time, anySuccess bool) {
	for _, f := range fetches {
		if f.Symbol != symbol || !f.OK || f.At.Before(t.Add(FetchLag)) {
			continue
		}
		anySuccess = true
		if f.Undated > 0 || (f.Truncated && (f.Oldest.IsZero() || !f.Oldest.Before(from))) {
			continue
		}
		if covered == nil || f.At.Before(*covered) {
			at := f.At
			covered = &at
		}
	}
	return covered, anySuccess
}
