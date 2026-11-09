package replaylayer

import (
	"slices"
	"sort"
	"strings"
	"time"
)

// ReasonNoSnapshots is the reason of a stretch between two gaps that has no
// market data at all.
const ReasonNoSnapshots = "no market_snapshots in the segment"

// plan is the common segmentation: the segments every cell is replayed on and
// the periods none is.
type plan struct {
	segments    []SegmentSpec
	unavailable []UnavailablePeriod
}

// normalizeGaps expands the caller's unavailable periods to whole JST days,
// clips them to [from, to), sorts them and merges overlapping or touching ones
// (their reasons are joined).
func normalizeGaps(from, to time.Time, in []Unavailable) []UnavailablePeriod {
	var gaps []UnavailablePeriod
	for _, u := range in {
		start, end := startOfDay(u.From), u.To
		if !startOfDay(end).Equal(end) {
			end = startOfDay(end).AddDate(0, 0, 1)
		}
		start, end = maxTime(start, from), minTime(end, to)
		if start.Before(end) {
			gaps = append(gaps, UnavailablePeriod{From: start, To: end, Reason: u.Reason})
		}
	}
	sort.SliceStable(gaps, func(i, j int) bool { return gaps[i].From.Before(gaps[j].From) })
	var merged []UnavailablePeriod
	for _, g := range gaps {
		if n := len(merged); n > 0 && !g.From.After(merged[n-1].To) {
			last := &merged[n-1]
			last.To = maxTime(last.To, g.To)
			if !strings.Contains("; "+last.Reason+"; ", "; "+g.Reason+"; ") {
				last.Reason += "; " + g.Reason
			}
			continue
		}
		merged = append(merged, g)
	}
	return merged
}

// newPlan splits [in.From, in.To) at the unavailable periods into segments.
// A stretch between gaps with no market data is itself unavailable.
func newPlan(in Input) plan {
	var p plan
	p.unavailable = normalizeGaps(in.From, in.To, in.Unavailable)
	cursor := in.From
	add := func(from, to time.Time) {
		if !from.Before(to) {
			return
		}
		days := tradingDays(in.Instruments, from, to)
		if len(days) == 0 {
			p.unavailable = append(p.unavailable, UnavailablePeriod{From: from, To: to, Reason: ReasonNoSnapshots})
			return
		}
		p.segments = append(p.segments, SegmentSpec{Index: len(p.segments), From: from, To: to, Days: days, InitialCapitalJPY: in.capital()})
	}
	for _, g := range slices.Clone(p.unavailable) {
		add(cursor, g.From)
		cursor = g.To
	}
	add(cursor, in.To)
	sort.SliceStable(p.unavailable, func(i, j int) bool { return p.unavailable[i].From.Before(p.unavailable[j].From) })
	return p
}

// tradingDays are the distinct JST days with a snapshot of any instrument in
// [from, to), ascending: the days every cell's daily series is built on.
func tradingDays(insts []Instrument, from, to time.Time) []string {
	seen := map[string]bool{}
	for _, inst := range insts {
		for i := firstIndexFrom(inst.Snapshots, from); i < len(inst.Snapshots) && inst.Snapshots[i].Timestamp.Before(to); i++ {
			seen[dayOf(inst.Snapshots[i].Timestamp)] = true
		}
	}
	days := make([]string, 0, len(seen))
	for d := range seen {
		days = append(days, d)
	}
	sort.Strings(days)
	return days
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
