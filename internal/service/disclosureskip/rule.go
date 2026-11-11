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

// UnknownDetail says more precisely why a decision is unknown (issue #1053),
// so the next measure can be chosen: it refines UnknownReason and is stored
// with the record, so it stays readable after the per-fetch rows are pruned.
// Records written before it existed have none.
type UnknownDetail string

// The details of ReasonNoFetch (no successful fetch started at or after
// t+FetchLag): the first one that holds, in this order.
const (
	// DetailFetchFailed: fetches of the symbol were attempted after the
	// decision and failed (never counted as coverage).
	DetailFetchFailed UnknownDetail = "fetch_failed"
	// DetailLunchBreak: t+FetchLag falls in the lunch break (11:30–12:30), when
	// News Ingest does not poll; no fetch of the next session covered the decision either.
	DetailLunchBreak UnknownDetail = "lunch_break"
	// DetailAfterClose: t+FetchLag is after the close (15:30), or otherwise
	// outside a session; the next session did not cover the decision either.
	DetailAfterClose UnknownDetail = "after_close"
	// DetailWithinFetchLag: only successful fetches inside [t, t+FetchLag)
	// exist, which are too early to count (the index may lag).
	DetailWithinFetchLag UnknownDetail = "within_fetch_lag"
	// DetailDroppedFromCandidates: the symbol was fetched before the decision
	// only; it left News Ingest's targets (Fast Screener candidates and
	// positions) and no later fetch reached it.
	DetailDroppedFromCandidates UnknownDetail = "dropped_from_candidates"
	// DetailNoFetchAtAll: no fetch of the symbol near the decision at all.
	DetailNoFetchAtAll UnknownDetail = "no_fetch_at_all"
)

// The details of ReasonIncompleteFetch (successful fetches exist, none covers).
const (
	// DetailUndatedItems: a fetch had an item without a readable publication
	// time (or one the feed dropped).
	DetailUndatedItems UnknownDetail = "undated_items"
	// DetailItemLimit: every fetch was cut off by the item limit before
	// reaching the window's start.
	DetailItemLimit UnknownDetail = "item_limit"
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
	// Detail refines Reason (VerdictUnknown only).
	Detail UnknownDetail
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
		res.Detail = incompleteDetail(symbol, t, fetches)
	default:
		res.Verdict, res.Reason = VerdictUnknown, ReasonNoFetch
		res.Detail = noFetchDetail(cal, symbol, t, fetches)
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

// incompleteDetail says why the successful fetches after t+FetchLag did not
// cover the window: an undated item in any of them, else the item limit.
func incompleteDetail(symbol string, t time.Time, fetches []Fetch) UnknownDetail {
	for _, f := range fetches {
		if f.Symbol == symbol && f.OK && !f.At.Before(t.Add(FetchLag)) && f.Undated > 0 {
			return DetailUndatedItems
		}
	}
	return DetailItemLimit
}

// noFetchDetail says why no successful fetch started at or after t+FetchLag
// (see the Detail constants for the order). fetches may reach back before t.
func noFetchDetail(cal Calendar, symbol string, t time.Time, fetches []Fetch) UnknownDetail {
	var inLag, before bool
	for _, f := range fetches {
		if f.Symbol != symbol {
			continue
		}
		switch {
		case !f.OK && !f.At.Before(t):
			return DetailFetchFailed
		case f.OK && f.At.Before(t):
			before = true
		case f.OK && f.At.Before(t.Add(FetchLag)):
			inLag = true
		}
	}
	if d := outsideSession(cal, t.Add(FetchLag)); d != "" {
		return d
	}
	switch {
	case inLag:
		return DetailWithinFetchLag
	case before:
		return DetailDroppedFromCandidates
	}
	return DetailNoFetchAtAll
}
