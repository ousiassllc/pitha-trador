package bootstrap

import (
	"context"
	"log/slog"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap/rankingmeasure"
	"github.com/ousiassllc/pitha-trador/internal/service/marketdata/kabu"
)

// startRankingMeasure launches the opt-in GET /ranking measurement loop
// (issue #652) when scan.ranking_measure.enabled is true; it does nothing
// otherwise. The loop only logs measurement values, never prices.
func (s *Services) startRankingMeasure(ctx context.Context) {
	cfg := s.strategy.Scan.RankingMeasure
	if !cfg.Enabled {
		return
	}
	adapter, ok := s.Broker.(*kabu.Adapter)
	if !ok { // kabu-only measurement (FR-SCHED-8): GET /ranking exists only there
		slog.Warn("bootstrap: scan.ranking_measure.enabled is ignored: the ranking measurement needs the kabu broker adapter",
			"broker", s.Broker.Capabilities().Name)
		return
	}
	m := rankingmeasure.Measurer{Ranker: adapter.Client(), Types: cfg.Types, Exchanges: cfg.Exchanges}
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
