// Package query holds the read-only SQL behind the Paper results MCP tools
// (issue #783, docs/api/endpoints/paper-results-mcp.md §3).
//
// Every function selects named columns only (never SELECT *), and every
// result type lists exactly the fields the API whitelist allows: a column
// that is not a field of a result struct cannot reach the client. The
// functions run against a *sql.Tx on the connection readdb.Open returned and
// only ever issue SELECT statements.
package query

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

// Limits fixed by the API specification (§2).
const (
	// MaxClosedScan is the most closed positions one aggregate may read.
	MaxClosedScan = 5000
	// MaxHorizons is the most calibration horizons jev_calibration returns.
	MaxHorizons = 8
	// MaxRows caps the per-day and missing-date rows (the period limit).
	MaxRows = 366
	// MaxReasonRunes is where metalabel reject_reason is cut.
	MaxReasonRunes = 200
)

// ErrLimitExceeded is returned instead of a partial result when a scan would
// pass MaxClosedScan or MaxHorizons.
var ErrLimitExceeded = errors.New("query: scan limit exceeded")

// ErrBadCursor is returned for a paper_trades / order_proposals_list cursor that is not one this
// package issued.
var ErrBadCursor = errors.New("query: invalid cursor")

// Q is satisfied by *sql.Tx (and *sql.DB).
type Q interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Range is the half-open instant interval [From, To) a tool reads.
type Range struct {
	From time.Time
	To   time.Time
}

func (r Range) bounds() (from, to string) {
	return sqlutil.FormatTime(r.From), sqlutil.FormatTime(r.To)
}

// Bounds is the range as the stored TEXT timestamps to compare against.
func (r Range) Bounds() (from, to string) { return r.bounds() }

// JST is the zone of the calendar days in periods and daily rows. Japan has
// no DST, so a fixed zone is exact and needs no tzdata.
var JST = time.FixedZone("JST", 9*60*60)

// FormatOut renders a stored TEXT timestamp as UTC RFC3339 (seconds). A value
// that is not a timestamp is returned unchanged.
func FormatOut(stored string) string {
	t, err := sqlutil.ParseTime(stored)
	if err != nil {
		return stored
	}
	return t.UTC().Format(time.RFC3339)
}

// formatOutPtr is FormatOut for a nullable column.
func formatOutPtr(stored sql.NullString) *string {
	if !stored.Valid {
		return nil
	}
	s := FormatOut(stored.String)
	return &s
}

func floatPtr(n sql.NullFloat64) *float64 {
	if !n.Valid {
		return nil
	}
	return &n.Float64
}

func ratio(num, den int) *float64 {
	if den == 0 {
		return nil
	}
	v := float64(num) / float64(den)
	return &v
}
