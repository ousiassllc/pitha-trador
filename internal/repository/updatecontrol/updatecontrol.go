// Package updatecontrol is the hand-over between the desktop application's
// self-updater and the Paper results MCP, a separate process (migration
// 000071, issue #1222): the application publishes the updater's state in
// update_control_status and executes the requests the MCP records in
// update_control_requests. The MCP holds no Checker, SafeGate or installer
// launcher; it can only read the state and insert a request.
//
// Only fixed codes are stored: the kinds of updater.BlockKind and
// updater.ErrorKind, never a raw error text, an installer path, a URL or a
// key. Nothing here is an input of trading.
package updatecontrol

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

// Request kinds.
const (
	KindCheck = "check"
	KindApply = "apply"
)

// ActorMCP is the only actor of a request.
const ActorMCP = "mcp"

// Request states.
const (
	StatePending = "pending"
	StateRunning = "running"
	StateDone    = "done"
)

// Outcomes of a done request.
const (
	OutcomeChecked     = "checked"
	OutcomeCheckFailed = "check_failed"
	OutcomeAccepted    = "accepted"
	OutcomeDeferred    = "deferred"
	OutcomeExpired     = "expired"
)

// StaleAfter is the heartbeat age after which the MCP ignores the published
// status (the application is gone and left its row behind).
const StaleAfter = time.Minute

// ReasonNotReady is the deferral reason "no verified installer yet" (the SafeGate kinds and updater.BlockMarketHours are the other reasons).
const ReasonNotReady = "not_ready"

// Reader is satisfied by *sql.DB and *sql.Tx.
type Reader interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Status is the update_control_status row.
type Status struct {
	HeartbeatAt    time.Time
	CurrentVersion string
	CheckedAt      *time.Time
	DevBuild       bool
	NoRelease      bool
	Available      bool
	LatestVersion  string
	Blocked        bool
	BlockedKind    string
	Ready          bool
	ErrorKind      string
}

// Request is one update_control_requests row.
type Request struct {
	ID          int64
	Kind        string
	Client      string
	RequestedAt time.Time
	State       string
	Outcome     string
	Reason      string
}

// Done reports whether the application finished (or the MCP withdrew) the request.
func (r Request) Done() bool { return r.State == StateDone }

const statusColumns = `heartbeat_at, current_version, checked_at, dev_build, no_release, available, latest_version,
	blocked, blocked_kind, ready, error_kind`

const requestColumns = `id, kind, client, requested_at, state, outcome, reason`

// ReadStatus returns the published status; ok is false when the application
// has not published one (no updater in this build, or the application is gone).
func ReadStatus(ctx context.Context, q Reader) (s Status, ok bool, err error) {
	var heartbeat string
	var checked sql.NullString
	err = q.QueryRowContext(ctx, `SELECT `+statusColumns+` FROM update_control_status WHERE id = 1`).Scan(
		&heartbeat, &s.CurrentVersion, &checked, &s.DevBuild, &s.NoRelease, &s.Available, &s.LatestVersion,
		&s.Blocked, &s.BlockedKind, &s.Ready, &s.ErrorKind)
	if errors.Is(err, sql.ErrNoRows) {
		return Status{}, false, nil
	}
	if err != nil {
		return Status{}, false, fmt.Errorf("updatecontrol: read status: %w", err)
	}
	if s.HeartbeatAt, err = sqlutil.ParseTime(heartbeat); err != nil {
		return Status{}, false, fmt.Errorf("updatecontrol: status heartbeat: %w", err)
	}
	if s.CheckedAt, err = sqlutil.ParseNullableTime(checked); err != nil {
		return Status{}, false, fmt.Errorf("updatecontrol: status checked_at: %w", err)
	}
	return s, true, nil
}

// GetRequest returns request id.
func GetRequest(ctx context.Context, q Reader, id int64) (Request, error) {
	return scanRequest(q.QueryRowContext(ctx, `SELECT `+requestColumns+` FROM update_control_requests WHERE id = ?`, id))
}

// LastApply returns the most recent apply request (the update audit), ok false when there is none.
func LastApply(ctx context.Context, q Reader) (Request, bool, error) {
	r, err := scanRequest(q.QueryRowContext(ctx, `SELECT `+requestColumns+` FROM update_control_requests WHERE kind = 'apply' ORDER BY id DESC LIMIT 1`))
	if errors.Is(err, sql.ErrNoRows) {
		return Request{}, false, nil
	}
	return r, err == nil, err
}

// OpenCheck returns the id of a check request still pending or running, ok
// false when there is none. A second check_now joins it instead of queuing
// another GitHub round trip.
func OpenCheck(ctx context.Context, q Reader) (int64, bool, error) {
	var id int64
	err := q.QueryRowContext(ctx, `SELECT id FROM update_control_requests WHERE kind = 'check' AND state <> 'done' ORDER BY id DESC LIMIT 1`).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	return id, err == nil, err
}

func scanRequest(row sqlutil.RowScanner) (Request, error) {
	var r Request
	var requested string
	if err := row.Scan(&r.ID, &r.Kind, &r.Client, &requested, &r.State, &r.Outcome, &r.Reason); err != nil {
		return Request{}, err
	}
	var err error
	if r.RequestedAt, err = sqlutil.ParseTime(requested); err != nil {
		return Request{}, fmt.Errorf("updatecontrol: request requested_at: %w", err)
	}
	return r, nil
}

// InsertRequest records a pending request of kind from client, stamped now,
// and returns its id. A check request also drops the finished check rows
// (they carry no audit value; the apply rows are the audit).
func InsertRequest(ctx context.Context, ex sqlutil.Execer, now time.Time, kind, client string) (int64, error) {
	if kind == KindCheck {
		if _, err := ex.ExecContext(ctx, `DELETE FROM update_control_requests WHERE kind = 'check' AND state = 'done'`); err != nil {
			return 0, fmt.Errorf("updatecontrol: prune finished checks: %w", err)
		}
	}
	res, err := ex.ExecContext(ctx, `INSERT INTO update_control_requests (kind, actor, client, requested_at) VALUES (?, ?, ?, ?)`,
		kind, ActorMCP, client, sqlutil.FormatTime(now))
	if err != nil {
		return 0, fmt.Errorf("updatecontrol: insert request: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("updatecontrol: request id: %w", err)
	}
	return id, nil
}

// Withdraw ends request id as expired while it is still pending, and reports
// whether it did. False means the application already took it: its outcome
// follows, so the caller must read it instead of assuming nothing happened.
func Withdraw(ctx context.Context, ex sqlutil.Execer, now time.Time, id int64) (bool, error) {
	return finish(ctx, ex, now, `id = ? AND state = 'pending'`, OutcomeExpired, "", id)
}

func finish(ctx context.Context, ex sqlutil.Execer, now time.Time, where, outcome, reason string, args ...any) (bool, error) {
	res, err := ex.ExecContext(ctx, `UPDATE update_control_requests SET state = 'done', outcome = ?, reason = ?, finished_at = ? WHERE `+where,
		append([]any{outcome, reason, sqlutil.FormatTime(now)}, args...)...)
	if err != nil {
		return false, fmt.Errorf("updatecontrol: finish request: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("updatecontrol: finish request: %w", err)
	}
	return n > 0, nil
}
