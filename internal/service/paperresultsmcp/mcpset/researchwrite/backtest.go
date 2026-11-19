// Package researchwrite holds the research tables' narrow writes of the MCP's
// Writer: the backtest_runs lifecycle (issue #1086) and the virtual Paper
// profiles (issue #1087). Each write is one statement of its own repository in
// one transaction run by mcpset.Writer.InTx; none touches a setting, an audit
// row, an order, a position or a signal.
package researchwrite

import (
	"context"
	"database/sql"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/backtestrun"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/mcpset"
)

// Backtests is the research-backtest side of the Writer (it is a
// backtestjob.Store). Its whole write surface is four transactions on the
// backtest_runs table, each one statement (repository/backtestrun):
// CreateBacktestRun (one INSERT), StartBacktestRun and FinishBacktestRun (one
// UPDATE each, the run's lifecycle) and FailOrphanBacktestRuns (one UPDATE).
//
// Only CreateBacktestRun looks at the write switch: it is what the MCP is
// allowed to start. Once a run is accepted its row must still reach a final
// status, switch or not (a run stuck in "running" would block the next one),
// and the orphan sweep is housekeeping of the same table.
type Backtests struct{ W *mcpset.Writer }

// CreateBacktestRun records req as a queued run, behind the write switch
// (ErrWriteDisabled while it is off, like experiment_create). client is the
// caller's host.
func (b Backtests) CreateBacktestRun(ctx context.Context, client string, req backtestrun.NewRun) (backtestrun.Run, error) {
	var out backtestrun.Run
	req.Client = client
	err := b.W.InTx(ctx, "backtest run transaction", true, func(tx *sql.Tx, now time.Time) error {
		var err error
		out, err = backtestrun.Insert(ctx, tx, now, req)
		return err
	})
	return out, err
}

// StartBacktestRun marks the queued run id as running.
func (b Backtests) StartBacktestRun(ctx context.Context, id int64) error {
	return b.W.InTx(ctx, "backtest run start transaction", false, func(tx *sql.Tx, now time.Time) error {
		return backtestrun.Start(ctx, tx, now, id)
	})
}

// FinishBacktestRun ends the queued or running run id with out.
func (b Backtests) FinishBacktestRun(ctx context.Context, id int64, out backtestrun.Outcome) error {
	return b.W.InTx(ctx, "backtest run finish transaction", false, func(tx *sql.Tx, now time.Time) error {
		return backtestrun.Finish(ctx, tx, now, id, out)
	})
}

// FailOrphanBacktestRuns ends every unfinished run whose owner is not owner as
// failed (interrupted) and returns how many it changed: a restart of the MCP
// leaves such rows behind.
func (b Backtests) FailOrphanBacktestRuns(ctx context.Context, owner string) (int64, error) {
	var n int64
	err := b.W.InTx(ctx, "backtest orphan transaction", false, func(tx *sql.Tx, now time.Time) error {
		var err error
		n, err = backtestrun.FailOrphans(ctx, tx, now, owner)
		return err
	})
	return n, err
}
