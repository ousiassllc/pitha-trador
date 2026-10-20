// Package mcpset is the settings side of the Paper results MCP (issues #806,
// #807): the narrow writer the settings tools use, the write switch, the audit
// log and the read models behind settings_get / settings_diff /
// settings_audit.
//
// The MCP's database connection stays mode=ro. The only write path is Writer,
// a second connection that is used for exactly two operations, each one
// transaction: upsert one whitelisted runtime_settings row plus one
// mcp_settings_audit row (Writer.Set), and flip the write switch plus its
// audit row (Writer.SetEnabled). The values are validated and stored by
// internal/service/opsettings over the same runtime_settings repository the
// Settings screen uses, so the application sees a saved value at once.
package mcpset

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/mcpwritesettings"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// Actors of an audit row.
const (
	ActorMCP = "mcp"
	ActorGUI = "gui"
)

// Actions of an audit row.
const (
	ActionSet          = "set"
	ActionReset        = "reset"
	ActionWriteEnable  = "write_enable"
	ActionWriteDisable = "write_disable"
)

// MaxAuditList is the most audit rows one settings_audit call returns.
const MaxAuditList = 200

// Entry is one audit row. Before and After are nil for "no stored value (the
// default applies)"; secret-class keys carry mcpwritesettings.Masked.
type Entry struct {
	ID     int64   `json:"id"`
	At     string  `json:"at"`
	Actor  string  `json:"actor"`
	Client string  `json:"client,omitempty"`
	Action string  `json:"action"`
	Key    string  `json:"key"`
	Before *string `json:"before"`
	After  *string `json:"after"`
}

// maskedEntry applies the masking policy: values of a secret-class key (or any
// key off the whitelist) never reach the table or an output.
func maskedEntry(e Entry) Entry {
	if e.Action == ActionWriteEnable || e.Action == ActionWriteDisable {
		return e // the switch's own "1"/"0"
	}
	if mcpwritesettings.IsSecret(e.Key) {
		m := mcpwritesettings.Masked
		if e.Before != nil {
			e.Before = &m
		}
		if e.After != nil {
			e.After = &m
		}
	}
	return e
}

// insertAudit appends e, stamped at now, in ex's transaction and returns it
// with its id and time filled in.
func insertAudit(ctx context.Context, ex sqlutil.Execer, now time.Time, e Entry) (Entry, error) {
	e = maskedEntry(e)
	res, err := ex.ExecContext(ctx, `
INSERT INTO mcp_settings_audit (at, actor, client, action, setting_key, before_value, after_value)
VALUES (?, ?, ?, ?, ?, ?, ?)`,
		sqlutil.FormatTime(now), e.Actor, e.Client, e.Action, e.Key, nullable(e.Before), nullable(e.After))
	if err != nil {
		return Entry{}, fmt.Errorf("mcpset: insert audit row: %w", err)
	}
	if e.ID, err = res.LastInsertId(); err != nil {
		return Entry{}, fmt.Errorf("mcpset: audit row id: %w", err)
	}
	e.At = now.UTC().Format(time.RFC3339)
	return e, nil
}

func nullable(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}

const auditColumns = `id, at, actor, client, action, setting_key, before_value, after_value`

type rowScanner interface{ Scan(dest ...any) error }

func scanEntry(r rowScanner) (Entry, error) {
	var (
		e        Entry
		at       string
		bef, aft sql.NullString
	)
	if err := r.Scan(&e.ID, &at, &e.Actor, &e.Client, &e.Action, &e.Key, &bef, &aft); err != nil {
		return Entry{}, err
	}
	e.At = at
	if t, err := sqlutil.ParseTime(at); err == nil {
		e.At = t.Format(time.RFC3339)
	}
	if bef.Valid {
		e.Before = &bef.String
	}
	if aft.Valid {
		e.After = &aft.String
	}
	return maskedEntry(e), nil
}

// ListAudit returns up to limit audit rows, newest first, with id below
// beforeID when beforeID > 0 (paging: pass the last id of the previous page).
func ListAudit(ctx context.Context, q query.Q, limit int, beforeID int64) ([]Entry, error) {
	if limit < 1 || limit > MaxAuditList {
		return nil, fmt.Errorf("mcpset: audit limit %d out of range", limit)
	}
	stmt := `SELECT ` + auditColumns + ` FROM mcp_settings_audit`
	args := []any{}
	if beforeID > 0 {
		stmt += ` WHERE id < ?`
		args = append(args, beforeID)
	}
	rows, err := q.QueryContext(ctx, stmt+` ORDER BY id DESC LIMIT ?`, append(args, limit)...)
	if err != nil {
		return nil, fmt.Errorf("mcpset: list audit: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []Entry{}
	for rows.Next() {
		e, err := scanEntry(rows)
		if err != nil {
			return nil, fmt.Errorf("mcpset: scan audit row: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// LatestAuditID returns the newest audit row's id, 0 for an empty log. It is
// the snapshot id settings_get reports and settings_diff accepts.
func LatestAuditID(ctx context.Context, q query.Q) (int64, error) {
	var id sql.NullInt64
	if err := q.QueryRowContext(ctx, `SELECT MAX(id) FROM mcp_settings_audit`).Scan(&id); err != nil {
		return 0, fmt.Errorf("mcpset: latest audit id: %w", err)
	}
	return id.Int64, nil
}
