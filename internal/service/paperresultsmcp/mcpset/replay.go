package mcpset

import (
	"context"
	"database/sql"

	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/replaytool"
)

// The replay side of the Writer (issue #1085). Its whole write surface is two
// transactions: StartReplay (one jev_replay_runs INSERT) and StopReplay (one
// UPDATE of the stop flag). It touches no setting (jev.replay.enabled stays the
// Settings screen's), audit row, position, signal or other table.

// StartReplay inserts a pending replay run for the plan args describes, behind
// the write switch (ErrWriteDisabled while it is off, like settings_set). client
// is the caller's host. The checks (the replay switch, one active run, the
// estimate's confirm token) are replaytool.Start's.
func (w *Writer) StartReplay(ctx context.Context, client string, r query.Range, args replaytool.StartArgs) (replaytool.RunView, error) {
	var out replaytool.RunView
	err := runInTx(ctx, w.db, "replay start transaction", func(tx *sql.Tx) error {
		on, err := WriteEnabled(ctx, system.NewRuntimeSettingsRepositoryOn(tx))
		if err != nil {
			return err
		}
		if !on {
			return ErrWriteDisabled
		}
		out, err = replaytool.Start(ctx, tx, w.now(), client, r, args)
		return err
	})
	return out, err
}

// StopReplay sets the stop flag of an active replay run. It does not look at
// the write switch: a stop only ends spending, and must work whatever the
// switch says.
func (w *Writer) StopReplay(ctx context.Context, runID int64) (replaytool.RunView, error) {
	var out replaytool.RunView
	err := runInTx(ctx, w.db, "replay stop transaction", func(tx *sql.Tx) (err error) {
		out, err = replaytool.Stop(ctx, tx, w.now(), runID)
		return err
	})
	return out, err
}
