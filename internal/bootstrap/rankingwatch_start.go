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
	caps := s.Broker.Capabilities()
	if s.strategy.Scan.FullScanOn() || !caps.Ranking {
		return
	}
	s.watchlist = &rankingwatch.Watchlist{}
	s.Broker.UseWatchlist()
	s.candidates.Watch = s.watchlist
	s.rankingWatcher = &rankingwatch.Watcher{
		Source:     s.Broker,
		Held:       rankingwatch.Held{Positions: s.Positions, Orders: s.Orders},
		Universe:   s.Instruments,
		Registrar:  s.Broker,
		Ingester:   s.Scheduler,
		List:       s.watchlist,
		Open:       marketcalendarOpen,
		MaxWatched: caps.MaxStreamSymbols,
	}
}

// startRankingWatch launches the watch loop (once a minute) when
// buildRankingWatch built one. A failed ranking never stops it: the cycle
// yields zero ranked candidates and the next tick recovers.
func (s *Services) startRankingWatch(ctx context.Context) {
	if s.rankingWatcher == nil {
		return
	}
	slog.Info("bootstrap: ranking watch enabled (scan.full_scan_enabled is not true): the broker ranking drives the stream watch list",
		"interval_seconds", int(rankingwatch.DefaultInterval/time.Second), "max_watched", s.Broker.Capabilities().MaxStreamSymbols,
		"max_replace_per_cycle", rankingwatch.MaxReplacePerCycle, "min_hold_minutes", int(rankingwatch.MinHold/time.Minute))
	s.wg.Add(1)
	go func() { defer s.wg.Done(); s.rankingWatcher.Run(ctx, rankingwatch.DefaultInterval) }()
}
