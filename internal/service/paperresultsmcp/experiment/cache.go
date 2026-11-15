package experiment

import (
	"sync"
	"time"
)

// CacheSettle is how long after its end an experiment's summary is recomputed
// before it is kept. Both periods of an ended experiment are closed intervals
// of the past, so its verdict does not change - except that rows timestamped
// inside them can still land a little later (a Jev decision that finished
// after it was timestamped, a trade closed in the last seconds). Past this
// margin a summary is final and is served from memory.
const CacheSettle = time.Hour

// maxCacheEntries bounds the memory of SummaryCache; one entry is a few hundred
// bytes. At the bound the cache is emptied (the listing shows at most
// maxExperimentList rows, so it refills at once).
const maxCacheEntries = 512

// summaryKey is everything a finished experiment's Summary depends on: the
// experiment as stored (an experiment is never edited, only ended) and the
// minimum number of trades the verdict was made with.
type summaryKey struct {
	id                 int64
	status             string
	startedAt, endedAt string
	minTrades          int
}

// SummaryCache keeps the Summary of experiments that ended at least
// CacheSettle ago, so that listing them again does not aggregate their trades
// again (issue #880). It is in memory on purpose: nothing about it is stored,
// so the experiment tables' write surface (one INSERT, one UPDATE) and the
// read-only MCP connection are untouched; after a restart the first listing
// aggregates once more. Entries that failed are never kept. A nil
// *SummaryCache is a cache that holds nothing. Safe for concurrent use.
type SummaryCache struct {
	mu sync.Mutex
	m  map[summaryKey]Summary
}

// NewSummaryCache returns an empty cache.
func NewSummaryCache() *SummaryCache { return &SummaryCache{m: map[summaryKey]Summary{}} }

func keyOf(exp Experiment, minTrades int) (summaryKey, bool) {
	if exp.EndedAt == nil || exp.endedAt == nil {
		return summaryKey{}, false
	}
	return summaryKey{id: exp.ID, status: exp.Status, startedAt: exp.StartedAt, endedAt: *exp.EndedAt, minTrades: minTrades}, true
}

func (c *SummaryCache) get(exp Experiment, minTrades int) (Summary, bool) {
	k, ok := keyOf(exp, minTrades)
	if c == nil || !ok {
		return Summary{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	s, ok := c.m[k]
	return s, ok
}

// put keeps s when it is final: its experiment ended at least CacheSettle
// before now and the row did not fail.
func (c *SummaryCache) put(s Summary, now time.Time) {
	k, ok := keyOf(s.Experiment, s.MinTrades)
	if c == nil || !ok || s.Error != "" || now.Sub(*s.Experiment.endedAt) < CacheSettle {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.m) >= maxCacheEntries {
		clear(c.m)
	}
	c.m[k] = s
}
