package experiment

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// Tx is the transaction handle the writes get: *sql.Tx.
type Tx interface {
	sqlutil.Execer
	query.Q
}

// The experiment table's whole write surface: one INSERT per experiment and
// one UPDATE per ending. settingstests.TestSettingsWritePathIsNarrow pins both
// statements.
const (
	insertSQL = `
INSERT INTO tuning_experiments (
    created_at, actor, client, hypothesis, variable_kind, variable_name, before_value, after_value,
    audit_id, started_at, ended_at, status)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, 'running')`

	closeSQL = `UPDATE tuning_experiments SET status = ?, ended_at = ? WHERE id = ? AND status = 'running'`

	selectColumns = `id, created_at, actor, client, hypothesis, variable_kind, variable_name, before_value, after_value,
    audit_id, started_at, ended_at, status`
)

// Insert stores req as a running experiment created at now by actor (client is
// the MCP caller's host, "" for the GUI) and returns it. For a setting change
// with an audit id, that row must be a set / reset of the same key.
func Insert(ctx context.Context, tx Tx, now time.Time, actor, client string, req Request) (Experiment, error) {
	if req.AuditID != nil {
		var key string
		err := tx.QueryRowContext(ctx, `SELECT setting_key FROM mcp_settings_audit WHERE id = ? AND action IN ('set', 'reset')`, *req.AuditID).Scan(&key)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && key != req.Change.Name) {
			return Experiment{}, invalid("audit_id は name の設定の変更（set / reset）の監査 ID ではありません")
		}
		if err != nil {
			return Experiment{}, fmt.Errorf("experiment: read audit row %d: %w", *req.AuditID, err)
		}
	}
	if req.Change.Kind == KindModel {
		if err := checkModelStart(ctx, tx, req.Change, req.StartedAt); err != nil {
			return Experiment{}, err
		}
	}
	c := req.Change
	res, err := tx.ExecContext(ctx, insertSQL, sqlutil.FormatTime(now), actor, client, req.Hypothesis, c.Kind, c.Name, c.Before, c.After,
		nullableInt(req.AuditID), sqlutil.FormatTime(req.StartedAt))
	if err != nil {
		return Experiment{}, fmt.Errorf("experiment: insert: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Experiment{}, fmt.Errorf("experiment: id: %w", err)
	}
	return Get(ctx, tx, id)
}

// Close ends the running experiment id with status (closed or aborted) at
// endedAt, or at now when endedAt is nil, and returns it. The end must not be
// before the start or after now. ErrNotFound for an unknown id and
// ErrNotRunning for one that already ended; nothing is stored then.
func Close(ctx context.Context, tx Tx, now time.Time, id int64, status string, endedAt *time.Time) (Experiment, error) {
	if status != StatusClosed && status != StatusAborted {
		return Experiment{}, invalid("status は %s か %s です", StatusClosed, StatusAborted)
	}
	exp, err := Get(ctx, tx, id)
	if err != nil {
		return Experiment{}, err
	}
	if exp.Status != StatusRunning {
		return Experiment{}, ErrNotRunning
	}
	end := now
	if endedAt != nil {
		end = endedAt.UTC()
	}
	if end.Before(exp.startedAt) || end.After(now) {
		return Experiment{}, invalid("ended_at は started_at 以後・現在以前の時刻です")
	}
	res, err := tx.ExecContext(ctx, closeSQL, status, sqlutil.FormatTime(end), id)
	if err != nil {
		return Experiment{}, fmt.Errorf("experiment: close %d: %w", id, err)
	}
	if n, err := res.RowsAffected(); err != nil || n == 0 {
		return Experiment{}, ErrNotRunning
	}
	return Get(ctx, tx, id)
}

// Get returns experiment id, or ErrNotFound.
func Get(ctx context.Context, q query.Q, id int64) (Experiment, error) {
	exp, err := scan(q.QueryRowContext(ctx, `SELECT `+selectColumns+` FROM tuning_experiments WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Experiment{}, ErrNotFound
	}
	return exp, err
}

// List returns up to limit experiments, newest first, optionally only those of
// one status ("" for all).
func List(ctx context.Context, q query.Q, status string, limit int) ([]Experiment, error) {
	stmt := `SELECT ` + selectColumns + ` FROM tuning_experiments`
	args := []any{}
	if status != "" {
		stmt += ` WHERE status = ?`
		args = append(args, status)
	}
	rows, err := q.QueryContext(ctx, stmt+` ORDER BY id DESC LIMIT ?`, append(args, limit)...)
	if err != nil {
		return nil, fmt.Errorf("experiment: list: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []Experiment{}
	for rows.Next() {
		exp, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, exp)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("experiment: list: %w", err)
	}
	return out, nil
}

type rowScanner interface{ Scan(dest ...any) error }

func scan(r rowScanner) (Experiment, error) {
	var (
		e                         Experiment
		createdAt, startedAt      string
		endedAt                   sql.NullString
		audit                     sql.NullInt64
		kind, name, before, after string
	)
	err := r.Scan(&e.ID, &createdAt, &e.Actor, &e.Client, &e.Hypothesis, &kind, &name, &before, &after,
		&audit, &startedAt, &endedAt, &e.Status)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Experiment{}, err
		}
		return Experiment{}, fmt.Errorf("experiment: scan: %w", err)
	}
	e.Variable = Variable{Kind: kind, Name: name, Before: before, After: after}
	if audit.Valid {
		e.Variable.AuditID = &audit.Int64
	}
	e.CreatedAt = query.FormatOut(createdAt)
	if e.startedAt, err = sqlutil.ParseTime(startedAt); err != nil {
		return Experiment{}, err
	}
	e.StartedAt = e.startedAt.Format(time.RFC3339)
	if endedAt.Valid {
		t, err := sqlutil.ParseTime(endedAt.String)
		if err != nil {
			return Experiment{}, err
		}
		e.endedAt = &t
		s := t.Format(time.RFC3339)
		e.EndedAt = &s
	}
	return e, nil
}

func nullableInt(v *int64) any {
	if v == nil {
		return nil
	}
	return *v
}
