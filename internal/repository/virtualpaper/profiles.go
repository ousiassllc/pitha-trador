// Package virtualpaper persists the research-only "virtual Paper" record (issue
// #1087): virtual_paper_profiles (a profile is the difference of the Policy /
// Exit settings from the real Paper, stored as variant.Params JSON),
// virtual_paper_trades (the virtual entries and exits a profile would have made
// on the same stored market_snapshots and jev_decisions) and virtual_paper_days
// (the per-(profile, JST day) mark that a day is fully recorded; migration
// 000066).
//
// The record is for research only. Policy, Risk, order placement, execution, the
// Paper guard, Live, Jev and self-improvement never read it, and it never writes
// paper_orders, positions, capital or any setting: the only writers are the daily
// recorder (internal/service/virtualpaper, wired by
// internal/bootstrap/virtualpaperjob) and the profile create / stop writes of the
// Paper results MCP. isolation_test.go pins that. It MUST depend only on
// internal/repository/sqlutil (the params are stored as text; the service parses
// them). See docs/architecture/overview.md §3.
package virtualpaper

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

// Profile statuses.
const (
	StatusActive  = "active"
	StatusStopped = "stopped"
)

// ActorMCP is the only actor of a profile: it is created from the MCP.
const ActorMCP = "mcp"

var (
	// ErrNotFound is returned for a profile id that does not exist.
	ErrNotFound = errors.New("virtualpaper: profile not found")
	// ErrNameTaken is returned when another profile (stopped ones included) has the name.
	ErrNameTaken = errors.New("virtualpaper: profile name is taken")
)

// Querier is the read side of *sql.DB, *sql.Tx and the MCP's read-only
// connection (query.Q).
type Querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// DB is a Querier that can also write: *sql.DB or *sql.Tx.
type DB interface {
	Querier
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// Profile is one virtual_paper_profiles row.
type Profile struct {
	ID int64
	// Name is 1..40 characters, unique among all profiles.
	Name string
	// ParamsJSON is variant.Params.Canonical().
	ParamsJSON string
	Status     string
	CreatedAt  time.Time
	// FirstDay is the first JST day ("2006-01-02") the profile is replayed for.
	FirstDay   string
	StoppedAt  *time.Time
	ClonedFrom *int64
	Actor      string
	Client     string
}

// NewProfile is what InsertProfile stores (an active profile).
type NewProfile struct {
	Name       string
	ParamsJSON string
	CreatedAt  time.Time
	FirstDay   string
	ClonedFrom *int64
	Actor      string
	Client     string
}

const profileColumns = `id, name, params_json, status, created_at, first_day, stopped_at, cloned_from, actor, client`

// InsertProfile stores p as an active profile and returns its id; ErrNameTaken
// when the name exists.
func InsertProfile(ctx context.Context, db DB, p NewProfile) (int64, error) {
	res, err := db.ExecContext(ctx, `INSERT INTO virtual_paper_profiles (name, params_json, status, created_at, first_day, cloned_from, actor, client)
VALUES (?, ?, 'active', ?, ?, ?, ?, ?)`,
		p.Name, p.ParamsJSON, sqlutil.FormatTime(p.CreatedAt), p.FirstDay, sqlutil.NullableInt64(p.ClonedFrom), p.Actor, p.Client)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed: virtual_paper_profiles.name") {
			return 0, ErrNameTaken
		}
		return 0, fmt.Errorf("virtualpaper: insert profile: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("virtualpaper: profile id: %w", err)
	}
	return id, nil
}

// GetProfile returns profile id, or ErrNotFound.
func GetProfile(ctx context.Context, q Querier, id int64) (Profile, error) {
	p, err := scanProfile(q.QueryRowContext(ctx, `SELECT `+profileColumns+` FROM virtual_paper_profiles WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Profile{}, ErrNotFound
	}
	return p, err
}

// ListProfiles returns the profiles of status ("" for every status), oldest first.
func ListProfiles(ctx context.Context, q Querier, status string) ([]Profile, error) {
	rows, err := q.QueryContext(ctx, `SELECT `+profileColumns+` FROM virtual_paper_profiles WHERE (? = '' OR status = ?) ORDER BY id`, status, status)
	if err != nil {
		return nil, fmt.Errorf("virtualpaper: list profiles: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []Profile
	for rows.Next() {
		p, err := scanProfile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("virtualpaper: list profiles: %w", err)
	}
	return out, nil
}

// CountProfiles returns the number of all profiles and of the active ones.
func CountProfiles(ctx context.Context, q Querier) (total, active int, err error) {
	err = q.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(status = 'active'), 0) FROM virtual_paper_profiles`).Scan(&total, &active)
	if err != nil {
		return 0, 0, fmt.Errorf("virtualpaper: count profiles: %w", err)
	}
	return total, active, nil
}

// StopProfile stops an active profile at at and reports whether it changed one
// (false: it was already stopped, or does not exist). The recorded rows stay.
func StopProfile(ctx context.Context, db DB, id int64, at time.Time) (bool, error) {
	res, err := db.ExecContext(ctx, `UPDATE virtual_paper_profiles SET status = 'stopped', stopped_at = ? WHERE id = ? AND status = 'active'`, sqlutil.FormatTime(at), id)
	if err != nil {
		return false, fmt.Errorf("virtualpaper: stop profile: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("virtualpaper: stop profile: %w", err)
	}
	return n > 0, nil
}

func scanProfile(row sqlutil.RowScanner) (Profile, error) {
	var (
		p       Profile
		created string
		stopped sql.NullString
	)
	if err := row.Scan(&p.ID, &p.Name, &p.ParamsJSON, &p.Status, &created, &p.FirstDay, &stopped, sqlutil.NullInt64(&p.ClonedFrom), &p.Actor, &p.Client); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Profile{}, err
		}
		return Profile{}, fmt.Errorf("virtualpaper: scan profile: %w", err)
	}
	var err error
	if p.CreatedAt, err = sqlutil.ParseTime(created); err != nil {
		return Profile{}, err
	}
	if p.StoppedAt, err = sqlutil.ParseNullableTime(stopped); err != nil {
		return Profile{}, err
	}
	return p, nil
}
