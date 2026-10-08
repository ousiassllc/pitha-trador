package bootstrap

import (
	"context"
	"log/slog"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap/rankingwatch"
	"github.com/ousiassllc/pitha-trador/internal/bootstrap/tachibanawatch"
	"github.com/ousiassllc/pitha-trador/internal/config/tachibanasource"
	"github.com/ousiassllc/pitha-trador/internal/router"
	"github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana/adapter"
	"github.com/ousiassllc/pitha-trador/internal/service/opsettings"
)

// startWatchList launches the 立花 watch list decision (issue #730, #726)
// when the 立花 adapter is selected: after each night's daily-bar batch it
// decides and saves the next 立会日's 監視リスト (日足スクリーニング or the
// operator's fixed list), and a fallback list (with an Activity notice) when
// the daily bars were unusable. The held symbols take the first slots, like
// the kabu ranking watch's. The saved list is what tachibanawatch.Source
// serves as the broker.CandidateSource.
func (s *Services) startWatchList(ctx context.Context) {
	a, ok := s.Broker.(*adapter.Adapter)
	if !ok {
		return
	}
	decider := tachibanawatch.New(tachibanawatch.Config{
		Settings: func(ctx context.Context) (tachibanasource.TachibanaSourceSettings, error) {
			return opsettings.LoadTachibanaSource(ctx, s.Settings)
		},
		Bars:   s.DailyBars,
		Runs:   s.DailyBarRuns,
		Lists:  s.WatchLists,
		Held:   rankingwatch.Held{Positions: s.Positions, Orders: s.Orders},
		Max:    a.Capabilities().MaxStreamSymbols,
		Notify: s.Activity.ObserveBrokerNotice,
	})
	slog.Info("bootstrap: 立花 watch list decision enabled", "max_symbols", a.Capabilities().MaxStreamSymbols)
	s.wg.Add(1)
	go func() { defer s.wg.Done(); decider.Run(ctx) }()
}

// watchlistRouterOptions serves the Watchlist screen and the banner's
// fallback notice when the 立花 adapter is selected.
func (s *Services) watchlistRouterOptions() []router.Option {
	if _, ok := s.Broker.(*adapter.Adapter); !ok {
		return nil
	}
	return []router.Option{router.WithWatchlistSource(tachibanawatch.Viewer{Lists: s.WatchLists})}
}
