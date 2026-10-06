package bootstrap

import (
	"context"
	"log/slog"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap/rankingwatch"
)

// buildRankingWatch builds the ranking-driven watch list (functional.md
// FR-SCHED-9, issues #651/#652), the default way symbols are monitored: it
// is active unless scan.full_scan_enabled is explicitly true. Active, the PUSH
// registration and the Fast Screener candidates follow the watch list instead
// of the universe.
func (s *Services) buildRankingWatch() {
	if s.strategy.Scan.FullScanOn() {
		return
	}
	s.watchlist = &rankingwatch.Watchlist{}
	s.PushFeed.UseWatchlist()
	s.candidates.Watch = s.watchlist
	s.rankingWatcher = &rankingwatch.Watcher{
		Source:    s.MarketData,
		Held:      rankingwatch.Held{Positions: s.Positions, Orders: s.Orders},
		Universe:  s.Instruments,
		Registrar: s.PushFeed,
		Ingester:  s.Scheduler,
		List:      s.watchlist,
		Open:      marketcalendarOpen,
	}
}

// startRankingWatch launches the watch loop (once a minute) when
// buildRankingWatch built one. A failed ranking never stops it: the cycle
// yields zero ranked candidates and the next tick recovers.
func (s *Services) startRankingWatch(ctx context.Context) {
	if s.rankingWatcher == nil {
		return
	}
	slog.Info("bootstrap: ranking watch enabled (scan.full_scan_enabled is not true): GET /ranking drives the PUSH watch list",
		"interval_seconds", int(rankingwatch.DefaultInterval/time.Second), "max_watched", rankingwatch.MaxWatched,
		"max_replace_per_cycle", rankingwatch.MaxReplacePerCycle, "min_hold_minutes", int(rankingwatch.MinHold/time.Minute))
	s.wg.Add(1)
	go func() { defer s.wg.Done(); s.rankingWatcher.Run(ctx, rankingwatch.DefaultInterval) }()
}
