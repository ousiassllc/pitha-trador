package system

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

// jevModelActor is the actor of every jev_model_changes row: the Settings
// screen is the only path that writes secrets (migration 000048).
const jevModelActor = "gui"

// inModelTx runs fn in one transaction with the stored JEV_MODEL value as
// before (nil when unset), so the secret and its jev_model_changes row commit
// or roll back together (issue #885).
func (r *SecretsRepository) inModelTx(ctx context.Context, key string, fn func(tx *sql.Tx, before *string) error) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("repository: begin %q change: %w", key, err)
	}
	defer func() { _ = tx.Rollback() }()
	value, ok, err := getSecret(ctx, tx, key)
	if err != nil {
		return err
	}
	var before *string
	if ok {
		before = &value
	}
	if err := fn(tx, before); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("repository: commit %q change: %w", key, err)
	}
	return nil
}

// recordJevModelChange appends one jev_model_changes row for a switch from
// before to after (nil = unset). Only the JEV_MODEL name goes in: this
// function is handed those two values and nothing else, so no other secret can
// reach the table. A save that does not change the value records nothing.
func recordJevModelChange(ctx context.Context, x sqlutil.Execer, before, after *string) error {
	if before == nil && after == nil || before != nil && after != nil && *before == *after {
		return nil
	}
	if _, err := x.ExecContext(ctx, `INSERT INTO jev_model_changes (at, actor, before_value, after_value) VALUES (?, ?, ?, ?)`,
		sqlutil.FormatTime(time.Now()), jevModelActor, before, after); err != nil {
		return fmt.Errorf("repository: record jev model change: %w", err)
	}
	return nil
}
