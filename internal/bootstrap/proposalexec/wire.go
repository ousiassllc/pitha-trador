package proposalexec

import (
	"context"
	"database/sql"
	"log/slog"
	"sync"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/repository/proposalstore"
	"github.com/ousiassllc/pitha-trador/internal/repository/trading"
)

// New builds the Processor over the Paper pipeline's own Risk and Execution
// engines (issue #828). Paper is the only mode: there is no Live wiring.
// snapshotMaxAge is the staleness limit Policy and Jev use for the symbol's
// latest bar.
func New(db *sql.DB, instruments *market.InstrumentRepository, snapshots *market.SnapshotRepository, signals *trading.SignalRepository,
	orders *trading.OrderRepository, positions *trading.PositionRepository, riskEngine Risk, exec Execution, snapshotMaxAge time.Duration) Processor {
	return Processor{
		Proposals: proposalstore.New(db, nil), Instruments: instruments, Snapshots: snapshots, Signals: signals,
		Orders: orders, Positions: positions, Risk: riskEngine, Execution: exec, SnapshotMaxAge: snapshotMaxAge,
	}
}

// Launch settles the proposals a previous run left in processing (once, before
// anything else touches them) and then starts the processing loop on wg until
// ctx is done. A recovery error is logged and the loop starts anyway: the
// affected proposals stay in processing until the next start.
func (p Processor) Launch(ctx context.Context, wg *sync.WaitGroup) {
	n, err := p.Recover(ctx)
	if err != nil {
		slog.Error("bootstrap: order proposal recovery failed", "error", err)
	}
	if n > 0 {
		slog.Info("bootstrap: recovered order proposals left in processing", "count", n)
	}
	wg.Add(1)
	go func() { defer wg.Done(); p.Run(ctx, DefaultInterval) }()
}
