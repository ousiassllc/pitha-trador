// Package researchquote persists the research-only 時価 samples (issue #1083):
// research_quote_samples, one row per (source, symbol, minute) of what 立花
// quoted next to the kabu broker's market_snapshots (migration 000060).
//
// The record is for research only. Policy, Risk, order placement, the Paper
// guard, Live, the Feature Engine, Scout, Trader and the Jev prompts never
// read it: the only users are the recorder (internal/bootstrap/researchquotes),
// the retention task (internal/bootstrap/researchquotejob) and the read-only MCP
// tool research_quote_source_compare. isolation_test.go pins that. The package
// MUST depend only on internal/repository/sqlutil.
package researchquote

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

// Sources and ways a sample arrived (the table's CHECK values).
const (
	SourceTachibana = "tachibana"
	ViaEvent        = "event"
	ViaREST         = "rest"
)

// Session phases of a sample (the table's CHECK values).
const (
	PhaseOpenAuction = "open_auction"
	PhaseMorning     = "morning"
	PhaseLunch       = "lunch"
	PhaseAfternoon   = "afternoon"
	PhaseClose       = "close"
)

// Minutes after JST midnight at which the session phase changes. The phases
// are a way to look at quote quality (the opening and closing minutes behave
// differently from the continuous ones), not a trading calendar: whether to
// record at all is the recorder's InSession.
const (
	openAuctionEnd = 9*60 + 5
	morningEnd     = 11*60 + 25
	lunchEnd       = 12*60 + 35
	afternoonEnd   = 15*60 + 20
)

// Phases lists the session phases in the order of the day.
var Phases = []string{PhaseOpenAuction, PhaseMorning, PhaseLunch, PhaseAfternoon, PhaseClose}

var jst = time.FixedZone("JST", 9*60*60)

// PhaseAt is the session phase of the JST minute of t: the first minutes after
// the 9:00 open, the morning, the minutes around the lunch break (11:25 to
// 12:35, which includes the first five minutes of the afternoon session), the
// afternoon, and the closing minutes from 15:20.
func PhaseAt(t time.Time) string {
	t = t.In(jst)
	switch m := t.Hour()*60 + t.Minute(); {
	case m < openAuctionEnd:
		return PhaseOpenAuction
	case m < morningEnd:
		return PhaseMorning
	case m < lunchEnd:
		return PhaseLunch
	case m < afternoonEnd:
		return PhaseAfternoon
	default:
		return PhaseClose
	}
}

// Sample is one recorded quote. Timestamp is the minute it stands for (the
// recorder truncates it), so (Source, Symbol, Timestamp) is unique.
type Sample struct {
	Timestamp    time.Time
	Source       string
	Symbol       string
	Price        float64
	Bid, Ask     *float64
	High, Low    *float64
	VWAP         *float64
	Volume       *float64
	Turnover     *float64
	SpecialQuote bool
	ObservedAt   *time.Time // when the broker value was last updated
	Via          string
	SessionPhase string
}

// Repository reads and writes research_quote_samples.
type Repository struct{ db *sql.DB }

// New returns a Repository backed by db.
func New(db *sql.DB) *Repository { return &Repository{db: db} }

// InsertBatch stores samples in one transaction and returns how many rows were
// new: a sample whose (source, symbol, timestamp) exists is skipped (only that
// conflict: a CHECK violation is an error), so recording a minute twice adds nothing.
func (r *Repository) InsertBatch(ctx context.Context, samples []Sample) (int, error) {
	if len(samples) == 0 {
		return 0, nil
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("researchquote: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO research_quote_samples
		(timestamp, source, symbol, price, bid, ask, high, low, vwap, volume, turnover, special_quote, observed_at, via, session_phase)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (source, symbol, timestamp) DO NOTHING`)
	if err != nil {
		return 0, fmt.Errorf("researchquote: prepare insert: %w", err)
	}
	defer func() { _ = stmt.Close() }()
	inserted := 0
	for _, s := range samples {
		var observed any
		if s.ObservedAt != nil {
			observed = sqlutil.FormatTime(*s.ObservedAt)
		}
		res, err := stmt.ExecContext(ctx, sqlutil.FormatTime(s.Timestamp), s.Source, s.Symbol, s.Price,
			nullable(s.Bid), nullable(s.Ask), nullable(s.High), nullable(s.Low), nullable(s.VWAP),
			nullable(s.Volume), nullable(s.Turnover), s.SpecialQuote, observed, s.Via, s.SessionPhase)
		if err != nil {
			return 0, fmt.Errorf("researchquote: insert %s: %w", s.Symbol, err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return 0, fmt.Errorf("researchquote: insert %s: %w", s.Symbol, err)
		}
		inserted += int(n)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("researchquote: commit: %w", err)
	}
	return inserted, nil
}

func nullable(v *float64) any {
	if v == nil {
		return nil
	}
	return *v
}

// DeleteBefore deletes up to limit rows stamped before the UTC date `before`
// ("YYYY-MM-DD") whose id is at most maxID (the state an archive verified), and
// returns how many were deleted. Callers repeat it until it returns fewer than
// limit, pausing between calls so other writers can take the lock.
func (r *Repository) DeleteBefore(ctx context.Context, before string, maxID int64, limit int) (int64, error) {
	res, err := r.db.ExecContext(ctx, `DELETE FROM research_quote_samples WHERE id IN (
		SELECT id FROM research_quote_samples WHERE timestamp < ? AND id <= ? ORDER BY timestamp, id LIMIT ?)`,
		before, maxID, limit)
	if err != nil {
		return 0, fmt.Errorf("researchquote: delete expired rows: %w", err)
	}
	return res.RowsAffected()
}

// Any reports whether the table holds a row.
func (r *Repository) Any(ctx context.Context) (bool, error) {
	var found bool
	if err := r.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM research_quote_samples)`).Scan(&found); err != nil {
		return false, fmt.Errorf("researchquote: check for rows: %w", err)
	}
	return found, nil
}
