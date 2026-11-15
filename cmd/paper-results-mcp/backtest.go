package main

import (
	"context"
	"fmt"
	"sync"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap"
	"github.com/ousiassllc/pitha-trador/internal/bootstrap/backtestsource"
	"github.com/ousiassllc/pitha-trador/internal/service/backtestlab"
)

// backtestSources builds the replay input of backtest_start (issue #1086). It
// opens its own read-only handle of the application database on the first
// backtest, separate from the MCP's tool connection, so a long replay never
// holds up the other tools; the strategy and risk config are loaded as the
// application and cmd/backtestverify do (PITHA_STRATEGY_PATH / PITHA_RISK_PATH,
// a config/ next to the executable, then the compiled-in default). The handle
// is read-only: a replay stores nothing.
type backtestSources struct {
	dbPath string

	mu    sync.Mutex
	state *bootstrap.State
}

// Source is a backtestjob.SourceFactory.
func (b *backtestSources) Source(_ context.Context, replayMode string) (backtestlab.ConfigSource, error) {
	mode, err := backtestsource.ParseReplayMode(replayMode)
	if err != nil {
		return nil, err
	}
	state, err := b.open()
	if err != nil {
		return nil, err
	}
	return backtestsource.NewForDB(state.DB, state.Strategy, state.Risk, mode), nil
}

// open returns the shared state, opening it on first use. A failed open is not
// cached.
func (b *backtestSources) open() (*bootstrap.State, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.state != nil {
		return b.state, nil
	}
	state, err := bootstrap.Run(bootstrap.Config{DBPath: b.dbPath, ReadOnly: true})
	if err != nil {
		return nil, fmt.Errorf("open the database for backtests: %w", err)
	}
	b.state = state
	return state, nil
}

// Close releases the handle, if one was opened. Call it after the server has
// stopped (which waits for a backtest in flight).
func (b *backtestSources) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.state == nil {
		return nil
	}
	err := b.state.Close()
	b.state = nil
	return err
}
