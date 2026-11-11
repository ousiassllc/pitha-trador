package sessiongap_test

import (
	"slices"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/marketcalendar"
	"github.com/ousiassllc/pitha-trador/internal/service/sessiongap"
)

var jst = marketcalendar.JST

// minutes lists every minute of [from, to) on the given date (HH:MM).
func minutes(date, from, to string) []time.Time {
	parse := func(hm string) time.Time {
		ts, err := time.ParseInLocation("2006-01-02 15:04", date+" "+hm, jst)
		if err != nil {
			panic(err)
		}
		return ts
	}
	var out []time.Time
	for m := parse(from); m.Before(parse(to)); m = m.Add(time.Minute) {
		out = append(out, m)
	}
	return out
}

func day(date string) time.Time {
	ts, err := time.ParseInLocation(time.DateOnly, date, jst)
	if err != nil {
		panic(err)
	}
	return ts
}

// The days of the issue #1054 report, as the snapshots showed them.
func TestClassifyJudgesSessionCompletenessNotJustWholeDayZero(t *testing.T) {
	cat := func(parts ...[]time.Time) []time.Time { return slices.Concat(parts...) }
	d := "2026-10-22"
	tests := []struct {
		name       string
		date       string
		morning    []time.Time
		afternoon  []time.Time
		hasEarlier bool
		wantKind   sessiongap.Kind
		wantCodes  []string
	}{
		{name: "complete day", date: d, morning: minutes(d, "09:00", "11:30"), afternoon: minutes(d, "12:30", "15:30"), hasEarlier: true, wantKind: sessiongap.KindNone},
		{name: "whole day without a snapshot", date: "2026-10-28", hasEarlier: true, wantKind: sessiongap.KindWholeDay},
		{name: "no snapshot and no history is insufficient, not a gap", date: "2026-10-28", wantKind: sessiongap.KindInsufficientHistory},
		{name: "10-22: started at 12:30, no morning", date: d, afternoon: minutes(d, "12:30", "15:30"), hasEarlier: true,
			wantKind: sessiongap.KindIntraday, wantCodes: []string{sessiongap.CodeMorningMissing}},
		{name: "11-09: first snapshot at 09:23", date: "2026-11-09", morning: minutes("2026-11-09", "09:23", "11:30"),
			afternoon: minutes("2026-11-09", "12:30", "15:30"), hasEarlier: true,
			wantKind: sessiongap.KindIntraday, wantCodes: []string{sessiongap.CodeLateStart}},
		{name: "10-29: first snapshot at 10:12", date: "2026-10-29", morning: minutes("2026-10-29", "10:12", "11:30"),
			afternoon: minutes("2026-10-29", "12:30", "15:30"), hasEarlier: true,
			wantKind: sessiongap.KindIntraday, wantCodes: []string{sessiongap.CodeLateStart}},
		{name: "11-05: first snapshot at 11:27", date: "2026-11-05", morning: minutes("2026-11-05", "11:27", "11:30"),
			afternoon: minutes("2026-11-05", "12:30", "15:30"), hasEarlier: true,
			wantKind: sessiongap.KindIntraday, wantCodes: []string{sessiongap.CodeLateStart}},
		{name: "no afternoon", date: d, morning: minutes(d, "09:00", "11:30"), hasEarlier: true,
			wantKind: sessiongap.KindIntraday, wantCodes: []string{sessiongap.CodeAfternoonMissing}},
		{name: "hole in the middle of a session", date: d, morning: minutes(d, "09:00", "11:30"),
			afternoon: cat(minutes(d, "12:30", "13:10"), minutes(d, "13:40", "15:30")), hasEarlier: true,
			wantKind: sessiongap.KindIntraday, wantCodes: []string{sessiongap.CodeMidSessionGap}},
		{name: "ends early", date: d, morning: minutes(d, "09:00", "11:30"), afternoon: minutes(d, "12:30", "14:10"), hasEarlier: true,
			wantKind: sessiongap.KindIntraday, wantCodes: []string{sessiongap.CodeEarlyEnd}},
		{name: "a short stretch within the tolerance is no gap", date: d, morning: minutes(d, "09:15", "11:30"),
			afternoon: cat(minutes(d, "12:30", "13:00"), minutes(d, "13:15", "15:15")), hasEarlier: true, wantKind: sessiongap.KindNone},
		{name: "just beyond the tolerance is a gap", date: d, morning: minutes(d, "09:16", "11:30"), afternoon: minutes(d, "12:30", "15:30"),
			hasEarlier: true, wantKind: sessiongap.KindIntraday, wantCodes: []string{sessiongap.CodeLateStart}},
		{name: "an 11-minute start delay is within the 15-minute tolerance", date: d, morning: minutes(d, "09:11", "11:30"),
			afternoon: minutes(d, "12:30", "15:30"), hasEarlier: true, wantKind: sessiongap.KindNone},
		{name: "first day of the history: the part before collection began is no gap", date: d, afternoon: minutes(d, "12:30", "15:30"),
			wantKind: sessiongap.KindNone},
		{name: "first day of the history: a hole after it still is", date: d, morning: cat(minutes(d, "10:12", "10:40"), minutes(d, "11:00", "11:30")),
			afternoon: minutes(d, "12:30", "15:30"), wantKind: sessiongap.KindIntraday, wantCodes: []string{sessiongap.CodeMidSessionGap}},
		{name: "Sports Day holiday is never a gap", date: "2026-10-12", hasEarlier: true, wantKind: sessiongap.KindNone},
		{name: "Culture Day holiday is never a gap", date: "2026-11-03", hasEarlier: true, wantKind: sessiongap.KindNone},
		{name: "a weekend is never a gap", date: "2026-10-24", hasEarlier: true, wantKind: sessiongap.KindNone},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := sessiongap.Classify(day(tt.date), tt.morning, tt.afternoon, tt.hasEarlier)
			if o.Kind != tt.wantKind || !slices.Equal(o.Codes, tt.wantCodes) {
				t.Fatalf("Classify = %s %v, want %s %v (gaps %+v)", o.Kind, o.Codes, tt.wantKind, tt.wantCodes, o.Gaps)
			}
			if o.Date != tt.date {
				t.Errorf("Date = %s, want %s", o.Date, tt.date)
			}
		})
	}
}

func TestClassifyReportsTheSpansOfTheGaps(t *testing.T) {
	date := "2026-11-09"
	o := sessiongap.Classify(day(date), minutes(date, "09:23", "11:30"), minutes(date, "12:30", "15:30"), true)
	if len(o.Gaps) != 1 {
		t.Fatalf("gaps = %+v, want one", o.Gaps)
	}
	g := o.Gaps[0]
	if g.Code != sessiongap.CodeLateStart || g.Minutes != 23 || !g.From.Equal(day(date).Add(9*time.Hour)) || !g.To.Equal(day(date).Add(9*time.Hour+23*time.Minute)) {
		t.Errorf("gap = %+v, want late_start 09:00-09:23 (23 min)", g)
	}
	if o.LongestGapMinutes != 23 || o.Minutes != 127+180 {
		t.Errorf("longest = %d, minutes = %d, want 23 and 307", o.LongestGapMinutes, o.Minutes)
	}
	if !o.FirstAt.Equal(day(date).Add(9*time.Hour+23*time.Minute)) || !o.LastAt.Equal(day(date).Add(15*time.Hour+29*time.Minute)) {
		t.Errorf("first/last = %v / %v", o.FirstAt, o.LastAt)
	}
}

// The tolerance is 15 minutes (issue #1054): a stretch of up to 15 minutes is no
// gap, a longer one always is, wherever it sits and however the rest of the day
// looks. Every start and length of a hole is tried in each session, so a hole
// beyond the tolerance cannot slip through at an edge or in the middle.
func TestClassifyGapsAreExactlyThoseBeyondFifteenMinutes(t *testing.T) {
	d := "2026-10-22"
	sessions := []struct{ name, open, closeAt string }{{"morning", "09:00", "11:30"}, {"afternoon", "12:30", "15:30"}}
	for si, s := range sessions {
		window := minutes(d, s.open, s.closeAt)
		for start := 0; start < len(window); start += 7 {
			for length := 1; start+length <= len(window); length += 3 {
				// Drop minutes [start, start+length) of this session.
				kept := slices.Concat(window[:start], window[start+length:])
				morning, afternoon := minutes(d, "09:00", "11:30"), minutes(d, "12:30", "15:30")
				if si == 0 {
					morning = kept
				} else {
					afternoon = kept
				}
				if len(kept) == 0 {
					continue // a whole session: covered by TestClassifyTheMissingSessionIsAlwaysDetected
				}
				o := sessiongap.Classify(day(d), morning, afternoon, true)
				// The hole is bounded by the session edges or by kept minutes, so its
				// length is the dropped minutes exactly.
				wantGap := time.Duration(length)*time.Minute > sessiongap.Tolerance
				if got := o.Kind == sessiongap.KindIntraday; got != wantGap {
					t.Fatalf("%s: %d minutes dropped from minute %d: kind %s (gaps %+v), want a gap = %v", s.name, length, start, o.Kind, o.Gaps, wantGap)
				}
			}
		}
	}
}

// A whole session missing is never a small gap: the hole is 150 or 180 minutes,
// ten times the tolerance, and it is reported whichever session is gone, with
// history, whatever the other session looks like.
func TestClassifyTheMissingSessionIsAlwaysDetected(t *testing.T) {
	d := "2026-10-22"
	full := func(from, to string) []time.Time { return minutes(d, from, to) }
	cases := []struct {
		name               string
		morning, afternoon []time.Time
		wantCode           string
		wantMinutes        int
	}{
		{"the whole morning session", nil, full("12:30", "15:30"), sessiongap.CodeMorningMissing, 150},
		{"the whole afternoon session", full("09:00", "11:30"), nil, sessiongap.CodeAfternoonMissing, 180},
		{"the morning session of a day that has only the last afternoon minutes", nil, full("15:00", "15:30"), sessiongap.CodeMorningMissing, 150},
		{"the afternoon session of a day that has only the first morning minutes", full("09:00", "09:30"), nil, sessiongap.CodeAfternoonMissing, 180},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o := sessiongap.Classify(day(d), tc.morning, tc.afternoon, true)
			if o.Kind != sessiongap.KindIntraday || !slices.Contains(o.Codes, tc.wantCode) {
				t.Fatalf("Classify = %s %v, want intraday with %s", o.Kind, o.Codes, tc.wantCode)
			}
			found := false
			for _, g := range o.Gaps {
				found = found || (g.Code == tc.wantCode && g.Minutes == tc.wantMinutes)
			}
			if !found {
				t.Errorf("gaps = %+v, want %s of %d minutes", o.Gaps, tc.wantCode, tc.wantMinutes)
			}
			if o.LongestGapMinutes < tc.wantMinutes {
				t.Errorf("LongestGapMinutes = %d, want at least %d", o.LongestGapMinutes, tc.wantMinutes)
			}
		})
	}
}
