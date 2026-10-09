package bootstrap

import (
	"context"
	"log/slog"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap/dailybars"
	"github.com/ousiassllc/pitha-trador/internal/config/tachibanasource"
	"github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana/adapter"
)

// startDailyBars launches the 立花 nightly daily-bar batch (issue #729,
// #726) when the 立花 adapter is selected and the candidate source is
// daily_screen (the fixed list needs no 日足). The batch runs after the
// configured time, never between 8:00 and 15:30, and does nothing otherwise.
func (s *Services) startDailyBars(ctx context.Context) {
	a, ok := s.Broker.(*adapter.Adapter)
	if !ok || s.tachibana.CandidateSource != tachibanasource.TachibanaSourceDailyScreen {
		return
	}
	runner := dailybars.New(dailybars.Config{Source: a, Bars: s.DailyBars, Runs: s.DailyBarRuns, Settings: s.tachibana.Nightly})
	slog.Info("bootstrap: 立花 nightly daily bars enabled", "run_time", s.tachibana.Nightly.RunTime,
		"max_per_second", s.tachibana.Nightly.MaxPerSecond, "markets", s.tachibana.Nightly.Markets)
	s.wg.Add(1)
	go func() { defer s.wg.Done(); runner.Run(ctx) }()
}

// The 立花 adapter is the batch's Source.
var _ dailybars.Source = (*adapter.Adapter)(nil)
