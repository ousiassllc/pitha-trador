// Package researchwire is the composition root's wiring of the 立花
// research-only collections (issues #1082, #1083): the nightly daily bars and
// the intraday 時価 recording that record 立花 data next to the selected
// broker. Both are OFF by default (the tachibanasource Research settings, read
// every minute) and share one 立花 session through researchhub, which logs in
// only when a switch is on, the credentials are set and the collection is
// about to need the broker. They write only research tables and daily_bars;
// nothing they record reaches the Feature Engine, Scout, Trader, Paper,
// Policy, Risk or an order, and they never touch the broker selection, the
// watch list or kabu's data path.
package researchwire

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap/dailybars"
	"github.com/ousiassllc/pitha-trador/internal/bootstrap/rankingwatch"
	"github.com/ousiassllc/pitha-trador/internal/bootstrap/researchhub"
	"github.com/ousiassllc/pitha-trador/internal/bootstrap/researchquotes"
	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/config/tachibanasource"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/repository/researchbars"
	"github.com/ousiassllc/pitha-trador/internal/repository/researchquote"
	"github.com/ousiassllc/pitha-trador/internal/repository/researchstatus"
	"github.com/ousiassllc/pitha-trador/internal/service/opsettings"
)

// Deps are what the collections read.
type Deps struct {
	DB       *sql.DB
	Settings opsettings.Store // runtime_settings
	Secrets  config.Secrets
	Broker   config.BrokerSettings // the start-up selection
	// DailyBars is the daily_bars store (read for the screening, written by the bars).
	DailyBars *market.DailyBarRepository
	// Watchlist is kabu's watch list (read-only; may be nil).
	Watchlist *rankingwatch.Watchlist
	// InSession reports whether the market is in session.
	InSession func(t time.Time) bool
}

// Wire holds the collections.
type Wire struct {
	Hub    *researchhub.Hub
	Bars   *dailybars.Research
	Quotes *researchquotes.Recorder
}

// Build builds the collections; nothing runs (and nothing is contacted) until Start.
func Build(d Deps) *Wire {
	settings := func(ctx context.Context) (tachibanasource.TachibanaSourceSettings, error) {
		return opsettings.LoadTachibanaSource(ctx, d.Settings)
	}
	hub := researchhub.New(researchhub.Config{
		Broker: d.Broker, Secrets: d.Secrets,
		Research: func(ctx context.Context) tachibanasource.TachibanaSourceSettings {
			set, err := settings(ctx)
			if err != nil { // the defaults apply; the feed limits stay modest
				return tachibanasource.BuildTachibanaSource(nil)
			}
			return set
		},
	})
	status := researchstatus.New(d.DB)
	return &Wire{
		Hub: hub,
		Bars: dailybars.NewResearch(dailybars.ResearchConfig{
			Settings: settings,
			Acquire: func(ctx context.Context) (dailybars.Source, error) {
				a, err := hub.Acquire(ctx)
				switch {
				case errors.Is(err, researchhub.ErrNotConfigured):
					return nil, dailybars.ErrNotConfigured
				case errors.Is(err, researchhub.ErrBrokerIsTachibana):
					return nil, dailybars.ErrBrokerIsTachibana
				case err != nil:
					return nil, err
				}
				return a, nil
			},
			Bars: d.DailyBars, Store: researchbars.New(d.DB), Status: status,
		}),
		Quotes: researchquotes.New(researchquotes.Config{
			Settings: settings,
			Start: func(ctx context.Context) (researchquotes.Feed, error) {
				a, err := hub.StartFeed(ctx)
				switch {
				case errors.Is(err, researchhub.ErrNotConfigured):
					return nil, researchquotes.ErrNotConfigured
				case errors.Is(err, researchhub.ErrBrokerIsTachibana):
					return nil, researchquotes.ErrBrokerIsTachibana
				case err != nil:
					return nil, err
				}
				return a.Feed(), nil
			},
			KabuSymbols:   d.Watchlist.Symbols,
			ScreenSymbols: researchquotes.ScreenSymbols(d.DailyBars),
			Store:         researchquote.New(d.DB), Status: status,
			InSession: d.InSession,
		}),
	}
}

// Start launches the collections on ctx; goroutines join wg. They poll their
// (off by default) switches every minute and do nothing, and contact no
// broker, while a switch is off or the credentials are unset.
func (w *Wire) Start(ctx context.Context, wg *sync.WaitGroup) {
	w.Hub.Start(ctx, wg)
	wg.Add(2)
	go func() { defer wg.Done(); w.Bars.Run(ctx) }()
	go func() { defer wg.Done(); w.Quotes.Run(ctx) }()
}
