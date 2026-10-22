package mcpset

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/experiment"
)

// The tuning-experiment side of the Writer (issue #871). Its whole write
// surface is two transactions: CreateExperiment (one tuning_experiments
// INSERT) and CloseExperiment (one UPDATE that ends a running experiment). It
// touches no setting, audit row, position or signal.

// CreateExperiment records req as a running experiment, behind the write
// switch (ErrWriteDisabled while it is off, like settings_set). client is the
// caller's host. Validation (experiment.Args.Validate) has already happened.
func (w *Writer) CreateExperiment(ctx context.Context, client string, req experiment.Request) (experiment.Experiment, error) {
	var out experiment.Experiment
	err := runInTx(ctx, w.db, "experiment transaction", func(tx *sql.Tx) error {
		on, err := WriteEnabled(ctx, system.NewRuntimeSettingsRepositoryOn(tx))
		if err != nil {
			return err
		}
		if !on {
			return ErrWriteDisabled
		}
		out, err = experiment.Insert(ctx, tx, w.now(), experiment.ActorMCP, client, req)
		return err
	})
	return out, err
}

// CloseExperiment ends the running experiment id (status closed or aborted) at
// endedAt, or now when nil, behind the write switch.
func (w *Writer) CloseExperiment(ctx context.Context, id int64, status string, endedAt *time.Time) (experiment.Experiment, error) {
	var out experiment.Experiment
	err := runInTx(ctx, w.db, "experiment close transaction", func(tx *sql.Tx) error {
		on, err := WriteEnabled(ctx, system.NewRuntimeSettingsRepositoryOn(tx))
		if err != nil {
			return err
		}
		if !on {
			return ErrWriteDisabled
		}
		out, err = experiment.Close(ctx, tx, w.now(), id, status, endedAt)
		return err
	})
	return out, err
}

// The Settings screen's side of the experiments. A person at the screen is not
// the MCP, so these do not look at the MCP write switch (which gates what the
// MCP may write, like the Settings screen's own settings saves).

// CreateExperiment records req as a running experiment made at the screen.
func (g *Gate) CreateExperiment(ctx context.Context, req experiment.Request) (experiment.Experiment, error) {
	var out experiment.Experiment
	err := g.inTx(ctx, func(tx *sql.Tx, now time.Time) (err error) {
		out, err = experiment.Insert(ctx, tx, now, experiment.ActorGUI, "", req)
		return err
	})
	return out, err
}

// CloseExperiment ends the running experiment id at now.
func (g *Gate) CloseExperiment(ctx context.Context, id int64, status string) (experiment.Experiment, error) {
	var out experiment.Experiment
	err := g.inTx(ctx, func(tx *sql.Tx, now time.Time) (err error) {
		out, err = experiment.Close(ctx, tx, now, id, status, nil)
		return err
	})
	return out, err
}

// Experiments lists up to limit experiments, newest first, with their verdicts
// (the same calculation as experiments_list, default min_trades).
func (g *Gate) Experiments(ctx context.Context, limit int) ([]experiment.Summary, error) {
	var out []experiment.Summary
	err := g.readTx(ctx, func(tx *sql.Tx) (err error) {
		capital, err := PaperCapitalJPY(ctx, tx, 0)
		if err != nil {
			return err
		}
		out, err = experiment.SummarizeAll(ctx, tx, "", limit, g.now(), 0, capital)
		return err
	})
	return out, err
}

// Compare is experiment_compare for experiment id (default min_trades).
func (g *Gate) Compare(ctx context.Context, id int64) (experiment.Comparison, error) {
	var out experiment.Comparison
	err := g.readTx(ctx, func(tx *sql.Tx) error {
		exp, err := experiment.Get(ctx, tx, id)
		if err != nil {
			return err
		}
		capital, err := PaperCapitalJPY(ctx, tx, 0)
		if err != nil {
			return err
		}
		out, err = experiment.Compare(ctx, tx, exp, g.now(), 0, capital, true)
		return err
	})
	return out, err
}

// readTx runs fn in one read-only transaction, so every query of a listing or
// comparison sees one snapshot.
func (g *Gate) readTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := g.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return fmt.Errorf("mcpset: begin read transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	return fn(tx)
}
