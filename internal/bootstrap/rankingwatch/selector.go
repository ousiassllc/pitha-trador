// Package rankingwatch is the default way the monitored symbols are decided
// (functional.md FR-SCHED-9, issues #651/#652): instead of ingesting the whole
// universe by REST every 60 seconds, kabuステーション GET /ranking
// (値上がり率・値下がり率・売買高・売買代金・TICK回数・売買高急増・売買代金急増)
// is fetched about once a minute and drives a watch list of at most
// MaxWatched symbols. The watch list is the PUSH registration, the set of
// symbols market-data jobs are enqueued for (together with the market-context
// index rows), and the universe of the Fast Screener candidates; outside the
// session the candidate list keeps the last watch list while PUSH and
// ingestion cover the held symbols only (Selector.Retain).
//
// Held (open position / pending order) symbols occupy fixed slots, at most
// MaxReplacePerCycle ranked symbols are replaced per cycle, and a ranked
// symbol stays at least MinHold. An empty or failed ranking yields no ranked
// symbol at all - only the held ones - and the next cycle recovers by itself.
//
// The ranking's prices and board data are never decoded (kabu adapter:
// marketdata.Client.RankingSymbols keeps symbol codes only), stored or logged
// (kabu利用規約, kabusapi#1343).
package rankingwatch

import (
	"slices"
	"strings"
	"time"
)

const (
	// DefaultMaxWatched is the watch list cap used when Selector.Max is unset:
	// the kabu adapter's broker.Capabilities.MaxStreamSymbols (kabu station
	// caps the API登録銘柄リスト at 50, shared with REST /board and /symbol
	// registrations; 5 stay free for those). Bootstrap sets Watcher.MaxWatched
	// from the selected adapter's capabilities.
	DefaultMaxWatched = 45
	// MaxReplacePerCycle is how many ranked symbols one cycle may replace
	// (drop for a newly ranked one). Free slots are filled without this cap.
	MaxReplacePerCycle = 5
	// MinHold is how long a ranked symbol stays on the watch list before it
	// may be replaced.
	MinHold = 5 * time.Minute
)

// Selector decides the watch list from the held symbols and the ranking. It
// is not safe for concurrent use; Watcher calls it from one goroutine.
type Selector struct {
	// Max is the most symbols on the watch list; zero means DefaultMaxWatched.
	Max int
	// ranked maps each ranking-selected (non-held) symbol to when it was put in.
	ranked map[string]time.Time
}

func (s *Selector) limit() int {
	if s.Max > 0 {
		return s.Max
	}
	return DefaultMaxWatched
}

// Update returns the watch list for this cycle (held symbols first, then the
// ranked ones in symbol order) and how many ranked symbols were added and
// removed. ranked is the ranking's symbols in priority order; empty means the
// ranking is empty or failed, which drops every ranked symbol at once (only
// held ones remain) rather than keeping stale ones.
func (s *Selector) Update(now time.Time, held, ranked []string) (watch []string, added, removed int) {
	heldSet := s.heldSlots(held)
	isHeld := make(map[string]bool, len(heldSet))
	for _, sym := range heldSet {
		isHeld[sym] = true
	}
	if len(ranked) == 0 {
		removed = len(s.ranked)
		s.ranked = nil
		return heldSet, 0, removed
	}
	if s.ranked == nil {
		s.ranked = make(map[string]time.Time)
	}
	for sym := range s.ranked { // a symbol that became held moves to a fixed slot
		if isHeld[sym] {
			delete(s.ranked, sym)
		}
	}

	slots := s.limit() - len(heldSet)
	target := make([]string, 0, slots)
	inTarget := make(map[string]bool, slots)
	for _, sym := range unique(ranked) {
		if len(target) == slots {
			break
		}
		if !isHeld[sym] {
			target = append(target, sym)
			inTarget[sym] = true
		}
	}

	// Symbols not in the ranking's top, oldest first: the replacement
	// candidates. When held symbols took slots away, the surplus is evicted
	// at once (held beats MinHold and the replace cap).
	stale := make([]string, 0, len(s.ranked))
	for sym := range s.ranked {
		if !inTarget[sym] {
			stale = append(stale, sym)
		}
	}
	slices.SortFunc(stale, func(a, b string) int {
		if c := s.ranked[a].Compare(s.ranked[b]); c != 0 {
			return c
		}
		return strings.Compare(a, b)
	})
	for forced := len(s.ranked) - slots; forced > 0 && len(stale) > 0; forced-- {
		delete(s.ranked, stale[0])
		stale = stale[1:]
		removed++
	}

	newcomers := make([]string, 0, len(target))
	for _, sym := range target {
		if _, ok := s.ranked[sym]; !ok {
			newcomers = append(newcomers, sym)
		}
	}
	fill := min(slots-len(s.ranked), len(newcomers))
	replaceable := make([]string, 0, len(stale))
	for _, sym := range stale {
		if now.Sub(s.ranked[sym]) >= MinHold {
			replaceable = append(replaceable, sym)
		}
	}
	replace := min(MaxReplacePerCycle, len(replaceable), len(newcomers)-fill)
	for _, sym := range replaceable[:replace] {
		delete(s.ranked, sym)
		removed++
	}
	for _, sym := range newcomers[:fill+replace] {
		s.ranked[sym] = now
		added++
	}

	watch = slices.Clone(heldSet)
	rankedSyms := make([]string, 0, len(s.ranked))
	for sym := range s.ranked {
		rankedSyms = append(rankedSyms, sym)
	}
	slices.Sort(rankedSyms)
	return append(watch, rankedSyms...), added, removed
}

// Retain is the off-session counterpart of Update (no ranking is requested
// outside the trading session): watch is the held symbols only - what PUSH
// registration and market-data ingestion use - while screen also keeps the
// ranked symbols of the last in-session cycle (held first, then the ranked
// ones in symbol order), so the candidate list can go on showing the last
// watch list from the stored data (FR-SCAN-7, non-functional.md §3). The ranked
// state and its MinHold timestamps survive, so the next session continues
// from it instead of replacing everything at once. Ranked symbols that became
// held move to a fixed slot; if held symbols took slots away, the oldest
// ranked ones are dropped.
func (s *Selector) Retain(held []string) (watch, screen []string) {
	watch = s.heldSlots(held)
	for _, sym := range watch {
		delete(s.ranked, sym)
	}
	rankedSyms := make([]string, 0, len(s.ranked))
	for sym := range s.ranked {
		rankedSyms = append(rankedSyms, sym)
	}
	slices.SortFunc(rankedSyms, func(a, b string) int {
		if c := s.ranked[b].Compare(s.ranked[a]); c != 0 { // newest first
			return c
		}
		return strings.Compare(a, b)
	})
	if slots := s.limit() - len(watch); len(rankedSyms) > slots {
		for _, sym := range rankedSyms[slots:] {
			delete(s.ranked, sym)
		}
		rankedSyms = rankedSyms[:slots]
	}
	slices.Sort(rankedSyms)
	return watch, append(slices.Clone(watch), rankedSyms...)
}

// heldSlots returns the held symbols that occupy fixed slots: de-duplicated and
// at most the watch list cap.
func (s *Selector) heldSlots(held []string) []string {
	heldSet := unique(held)
	if limit := s.limit(); len(heldSet) > limit {
		heldSet = heldSet[:limit]
	}
	return heldSet
}

// unique returns symbols without empty entries and duplicates, order kept.
func unique(symbols []string) []string {
	seen := make(map[string]struct{}, len(symbols))
	out := make([]string, 0, len(symbols))
	for _, sym := range symbols {
		if sym == "" {
			continue
		}
		if _, dup := seen[sym]; dup {
			continue
		}
		seen[sym] = struct{}{}
		out = append(out, sym)
	}
	return out
}
