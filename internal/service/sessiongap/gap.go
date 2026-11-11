// Package sessiongap defines when a trading day's market-data collection is
// incomplete (issue #1054) and records the verdict once a day.
//
// The older check (#808/#814) called a day missing only when it had no
// market_snapshots row at all. A day that starts late (the app was asleep until
// 12:30, a login at 10:12) or loses a stretch of the session was therefore
// never reported. Classify judges the minutes that have a snapshot against the
// two TSE sessions (前場 09:00-11:30, 後場 12:30-15:30; marketcalendar), so a
// holiday has no session and is never a gap.
//
// Everything here only describes what market_snapshots shows. Nothing is
// fetched from a broker and no snapshot is ever written, so a missed day stays
// missed (no backfill). Nothing in this package is read by the policy, risk,
// order, Paper/Live paths or the Jev prompts.
package sessiongap

import (
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/healthalert"
	"github.com/ousiassllc/pitha-trador/internal/service/marketcalendar"
)

// Kind is the verdict on one trading day. The values are stored (migration
// 000052) and returned by the MCP, so they are fixed strings.
type Kind string

const (
	// KindNone: the sessions are complete.
	KindNone Kind = "none"
	// KindWholeDay: no snapshot all day although earlier history exists.
	KindWholeDay Kind = "whole_day"
	// KindIntraday: some snapshots exist but a session is missing, starts late,
	// has a long hole or ends early (see the Code constants).
	KindIntraday Kind = "intraday"
	// KindInsufficientHistory: no snapshot all day and none before the day
	// either: the history began later (first start), so nothing can be said
	// (#814). It is never a warning.
	KindInsufficientHistory Kind = "insufficient_history"
	// KindInProgress is stored while the day is still being observed; it is a
	// storage state, never the verdict Classify returns.
	KindInProgress Kind = "in_progress"
)

// Codes of an intraday gap: fixed strings, never free text.
const (
	// CodeMorningMissing / CodeAfternoonMissing: no snapshot in that session
	// while the other has some (e.g. an app started at 12:30).
	CodeMorningMissing   = "morning_missing"
	CodeAfternoonMissing = "afternoon_missing"
	// CodeLateStart: a session's first snapshot came more than Tolerance after
	// its open (e.g. 09:23, 10:12, 11:27).
	CodeLateStart = "late_start"
	// CodeMidSessionGap: two consecutive snapshots of a session more than
	// Tolerance apart.
	CodeMidSessionGap = "mid_session_gap"
	// CodeEarlyEnd: a session's last snapshot ended more than Tolerance before
	// its close.
	CodeEarlyEnd = "early_end"
)

// Tolerance is the longest stretch of a session without a snapshot that is not
// a gap: the market_data_idle threshold (healthalert.IdleThreshold), so a hole
// the in-app alert would have raised is a hole here too. The snapshot minute is
// the unit, so a hole is counted from the end of the last covered minute.
const Tolerance = healthalert.IdleThreshold

// maxGaps caps the spans recorded for one day.
const maxGaps = 10

// Gap is one stretch of a session with no snapshot, longer than Tolerance.
type Gap struct {
	Code    string    `json:"code"`
	From    time.Time `json:"from"`
	To      time.Time `json:"to"`
	Minutes int       `json:"minutes"`
}

// Observation is the verdict on one trading day with the counts behind it. It
// holds no price or raw column.
type Observation struct {
	// Date is the JST date (YYYY-MM-DD).
	Date string
	Kind Kind
	// Codes are the intraday gap codes, unique and in the order found.
	Codes []string
	Gaps  []Gap
	// Rows is the number of market_snapshots rows inside the sessions (filled
	// by DBStore.Observe, not by Observe); Minutes the number of distinct
	// minutes with at least one.
	Rows    int
	Minutes int
	// FirstAt and LastAt are the first and last snapshot minute (zero when none).
	FirstAt, LastAt time.Time
	// LongestGapMinutes is the longest stretch with no snapshot inside the
	// sessions, the session edges included.
	LongestGapMinutes int
}

// Classify judges day (any instant of a JST date) from the snapshot minutes
// found inside its morning and afternoon sessions (each ascending, truncated to
// the minute). hasEarlier is whether any snapshot exists before the day. A day
// without sessions (not a trading day) is KindNone with no codes.
//
// With no earlier history the day is where collection began: the stretch before
// its first snapshot is not a gap (nothing was collecting yet), but holes and
// an early end after it still are.
func Classify(day time.Time, morning, afternoon []time.Time, hasEarlier bool) Observation {
	day = day.In(marketcalendar.JST)
	o := Observation{Date: day.Format(time.DateOnly), Kind: KindNone}
	sessions := marketcalendar.TSE.Sessions(day)
	if len(sessions) != 2 {
		return o
	}
	points := [2][]time.Time{morning, afternoon}
	o.Minutes = len(morning) + len(afternoon)
	if o.Minutes == 0 {
		o.Kind = KindWholeDay
		if !hasEarlier {
			o.Kind = KindInsufficientHistory
		}
		for _, s := range sessions {
			o.longest(s.Open, s.Close)
		}
		return o
	}
	for i := range points {
		if len(points[i]) == 0 {
			continue
		}
		if o.FirstAt.IsZero() {
			o.FirstAt = points[i][0]
		}
		o.LastAt = points[i][len(points[i])-1]
	}

	// Without history the leading edge starts at the first snapshot of the day.
	judged := hasEarlier
	missing := [2]string{CodeMorningMissing, CodeAfternoonMissing}
	for i, s := range sessions {
		p := points[i]
		if len(p) == 0 {
			if judged {
				o.add(Gap{Code: missing[i], From: s.Open, To: s.Close})
			}
			continue
		}
		if judged {
			o.addIfLong(CodeLateStart, s.Open, p[0])
		}
		judged = true
		for j := 1; j < len(p); j++ {
			o.addIfLong(CodeMidSessionGap, p[j-1].Add(time.Minute), p[j])
		}
		o.addIfLong(CodeEarlyEnd, p[len(p)-1].Add(time.Minute), s.Close)
	}
	if len(o.Gaps) > 0 {
		o.Kind = KindIntraday
	}
	return o
}

// addIfLong counts [from, to) toward the longest stretch and records it as a
// gap with code when it is longer than Tolerance.
func (o *Observation) addIfLong(code string, from, to time.Time) {
	if to.Sub(from) > Tolerance {
		o.add(Gap{Code: code, From: from, To: to})
		return
	}
	o.longest(from, to)
}

// add records g (its Minutes are derived) and keeps the unique code list.
func (o *Observation) add(g Gap) {
	g.Minutes = int(g.To.Sub(g.From) / time.Minute)
	o.longest(g.From, g.To)
	seen := false
	for _, c := range o.Codes {
		seen = seen || c == g.Code
	}
	if !seen {
		o.Codes = append(o.Codes, g.Code)
	}
	if len(o.Gaps) < maxGaps {
		o.Gaps = append(o.Gaps, g)
	}
}

func (o *Observation) longest(from, to time.Time) {
	if m := int(to.Sub(from) / time.Minute); m > o.LongestGapMinutes {
		o.LongestGapMinutes = m
	}
}
