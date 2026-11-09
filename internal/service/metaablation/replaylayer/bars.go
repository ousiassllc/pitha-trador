package replaylayer

import (
	"fmt"
	"sort"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/featureengine"
)

// jst bounds a trading day: TSE trades in JST (as simrisk's own day does).
var jst = time.FixedZone("JST", 9*60*60)

const dayLayout = "2006-01-02"

// dayOf is the JST trading day of t.
func dayOf(t time.Time) string { return t.In(jst).Format(dayLayout) }

// startOfDay is the JST midnight at or before t.
func startOfDay(t time.Time) time.Time {
	y, m, d := t.In(jst).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, jst)
}

// firstIndexFrom is the index of the first bar at or after t.
func firstIndexFrom(bars []domain.Snapshot, t time.Time) int {
	return sort.Search(len(bars), func(i int) bool { return !bars[i].Timestamp.Before(t) })
}

// segmentBars cuts bars for a replay of [from, to): the bars before to, with the
// history before from reduced to the look-ahead check's lookback. The leading
// bars are history only (the returned warm-up count), so the check covers
// exactly the bars from from on - once per segment instead of once per replay
// of the whole series. A position still open at the end is closed at the last
// bar before to, never on a bar of a later segment.
func segmentBars(bars []domain.Snapshot, from, to time.Time) (cut []domain.Snapshot, warmup int) {
	end := firstIndexFrom(bars, to)
	start := firstIndexFrom(bars, from)
	if start > end {
		start = end
	}
	lead := max(0, start-featureengine.HistoryLookbackBars)
	return bars[lead:end], start - lead
}

// parseDay is the JST midnight of a "2006-01-02" day string produced by dayOf.
func parseDay(day string) (time.Time, error) {
	t, err := time.ParseInLocation(dayLayout, day, jst)
	if err != nil {
		return time.Time{}, fmt.Errorf("replaylayer: bad trading day %q: %w", day, err)
	}
	return t, nil
}
