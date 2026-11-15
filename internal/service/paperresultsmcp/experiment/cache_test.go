package experiment

import (
	"testing"
	"time"
)

func ended(id int64, at time.Time) Experiment {
	s := at.UTC().Format(time.RFC3339)
	return Experiment{ID: id, Status: StatusClosed, StartedAt: "2026-10-01T00:00:00Z", EndedAt: &s, endedAt: &at}
}

// A summary is kept only once its experiment ended CacheSettle ago, never when
// the row failed, never for a running experiment, and per min_trades.
func TestSummaryCacheKeepsOnlyFinalSummaries(t *testing.T) {
	c := NewSummaryCache()
	now := time.Date(2026, 10, 18, 3, 0, 0, 0, time.UTC)

	old := ended(1, now.Add(-CacheSettle))
	c.put(Summary{Experiment: old, MinTrades: MinTradesFloor, Verdict: VerdictImproved}, now)
	if s, ok := c.get(old, MinTradesFloor); !ok || s.Verdict != VerdictImproved {
		t.Errorf("an experiment that ended CacheSettle ago: %+v, %v, want it cached", s, ok)
	}
	if _, ok := c.get(old, 100); ok {
		t.Error("the entry of min_trades 64 answered min_trades 100")
	}

	recent := ended(2, now.Add(-CacheSettle+time.Second))
	c.put(Summary{Experiment: recent, MinTrades: MinTradesFloor}, now)
	if _, ok := c.get(recent, MinTradesFloor); ok {
		t.Error("an experiment that ended a moment ago was cached: its periods can still receive rows")
	}

	failed := ended(3, now.Add(-24*time.Hour))
	c.put(Summary{Experiment: failed, MinTrades: MinTradesFloor, Error: SummaryFailed}, now)
	if _, ok := c.get(failed, MinTradesFloor); ok {
		t.Error("a failed row was cached")
	}

	running := Experiment{ID: 4, Status: StatusRunning, StartedAt: "2026-10-01T00:00:00Z"}
	c.put(Summary{Experiment: running, MinTrades: MinTradesFloor}, now)
	if _, ok := c.get(running, MinTradesFloor); ok {
		t.Error("a running experiment was cached")
	}
}

func TestNilSummaryCacheHoldsNothing(t *testing.T) {
	var c *SummaryCache
	now := time.Date(2026, 10, 18, 3, 0, 0, 0, time.UTC)
	e := ended(1, now.Add(-48*time.Hour))
	c.put(Summary{Experiment: e, MinTrades: MinTradesFloor}, now)
	if _, ok := c.get(e, MinTradesFloor); ok {
		t.Error("a nil cache answered")
	}
}

func TestSummaryCacheIsBounded(t *testing.T) {
	c := NewSummaryCache()
	now := time.Date(2026, 10, 18, 3, 0, 0, 0, time.UTC)
	for i := int64(1); i <= maxCacheEntries+10; i++ {
		c.put(Summary{Experiment: ended(i, now.Add(-48*time.Hour)), MinTrades: MinTradesFloor}, now)
	}
	if len(c.m) > maxCacheEntries {
		t.Errorf("%d entries, want at most %d", len(c.m), maxCacheEntries)
	}
	last := ended(maxCacheEntries+10, now.Add(-48*time.Hour))
	if _, ok := c.get(last, MinTradesFloor); !ok {
		t.Error("the newest entry is gone")
	}
}
