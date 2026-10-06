package bootstrap

import (
	"context"
	"log/slog"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap/rankingmeasure"
)

// startRankingMeasure launches the opt-in GET /ranking measurement loop
// (issue #652) when scan.ranking_measure.enabled is true; it does nothing
// otherwise. The loop only logs measurement values, never prices.
func (s *Services) startRankingMeasure(ctx context.Context) {
	cfg := s.strategy.Scan.RankingMeasure
	if !cfg.Enabled {
		return
	}
	m := rankingmeasure.Measurer{Ranker: s.MarketData, Types: cfg.Types, Exchanges: cfg.Exchanges}
	if !cfg.IncludeOutsideSession {
		m.Open = marketcalendarOpen
	}
	interval := time.Duration(cfg.IntervalSeconds) * time.Second
	slog.Warn("bootstrap: ranking measurement enabled: GET /ranking is called periodically and only measurement values are logged",
		"interval_seconds", cfg.IntervalSeconds, "types", cfg.Types, "exchanges", cfg.Exchanges,
		"include_outside_session", cfg.IncludeOutsideSession)
	s.wg.Add(1)
	go func() { defer s.wg.Done(); m.Run(ctx, interval) }()
}
