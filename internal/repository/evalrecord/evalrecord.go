// Package evalrecord persists the research-only daily record of the Jev
// evaluation (issue #1089): jev_evaluation_daily, one row per JST day, per
// (model_id, question_version) cohort and per label horizon, holding the day's
// counts and the sums of the net bps (cost-inclusive triple-barrier label) of the
// LLM arm and of the random / momentum / contrarian baselines (migration 000064).
//
// The record is for research only. Policy, Risk, order placement, the Paper
// guard, Live, the Jev prompts, RAG, the meta-model training and self-improvement
// never read it: the only users are the recorder (internal/service/evalrecord,
// wired by internal/bootstrap/nightlyjobs) and the read-only MCP tool
// jev_evaluation_trend. isolation_test.go pins that. It writes only
// jev_evaluation_daily and reads only jev_decisions (which JST days had trader
// decisions); it MUST depend only on internal/repository/sqlutil.
package evalrecord

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

// Row is one row of jev_evaluation_daily: one JST day of one cohort at one
// horizon. The *Sum fields are sums of net bps over the Evaluated decisions (the
// label's cost is already deducted), so the day's mean is Sum / Evaluated.
type Row struct {
	// Day is the JST date ("2006-01-02").
	Day             string
	ModelID         string
	QuestionVersion string
	HorizonMinutes  int
	// TakeProfitBps, StopLossBps and BarrierCostBps are the label settings the day was evaluated with.
	TakeProfitBps, StopLossBps, BarrierCostBps float64
	// Decisions are the trader LONG/SHORT decisions of the day; Evaluated those that reached
	// the comparison set (Excluded = Decisions - Evaluated is derived, never stored); Entered
	// those of Evaluated that passed Risk; Wins those of Evaluated with an LLM net above 0.
	Decisions, Evaluated, Entered, Wins int
	LLMSum, RandomSum, MomentumSum      float64
	ContrarianSum                       float64
}

// Excluded is the number of decisions of the day that were not evaluated.
func (r Row) Excluded() int { return r.Decisions - r.Evaluated }

// Reader is the read access the list functions need; *sql.DB, *sql.Tx and the
// MCP's read-only query.Q implement it.
type Reader interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Repository reads jev_decisions and writes jev_evaluation_daily.
type Repository struct {
	db *sql.DB
}

// NewRepository returns a Repository backed by db.
func NewRepository(db *sql.DB) *Repository { return &Repository{db: db} }

// jstHours is the offset that turns a stored UTC timestamp into its JST date.
const jstHours = "+9 hours"

// DecisionDays returns the JST days of [from, to) ("2006-01-02"; from inclusive, to
// exclusive) that have at least one trader LONG/SHORT decision, oldest first. The
// timestamp range is served by the jev_decisions timestamp index.
func (r *Repository) DecisionDays(ctx context.Context, from, to string) ([]string, error) {
	lo, hi, err := dayBounds(from, to)
	if err != nil {
		return nil, err
	}
	return scanDays(ctx, r.db, `SELECT DISTINCT strftime('%Y-%m-%d', timestamp, '`+jstHours+`') AS day FROM jev_decisions
WHERE decision_type = 'trader' AND direction IN ('LONG', 'SHORT') AND timestamp >= ? AND timestamp < ? ORDER BY day`,
		sqlutil.FormatTime(lo), sqlutil.FormatTime(hi))
}

// RecordedDays returns the JST days of [from, to) that have at least one row in
// jev_evaluation_daily, oldest first.
func (r *Repository) RecordedDays(ctx context.Context, from, to string) ([]string, error) {
	if _, _, err := dayBounds(from, to); err != nil {
		return nil, err
	}
	return scanDays(ctx, r.db, `SELECT DISTINCT day FROM jev_evaluation_daily WHERE day >= ? AND day < ? ORDER BY day`, from, to)
}

// InsertDay adds the rows in one short transaction and returns how many were new.
// A row whose (day, model_id, question_version, horizon_minutes) is already
// recorded is left exactly as it is (INSERT OR IGNORE): the record is of that day,
// a re-run never duplicates or rewrites it.
func (r *Repository) InsertDay(ctx context.Context, rows []Row) (int, error) {
	if len(rows) == 0 {
		return 0, nil
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("evalrecord: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	stmt, err := tx.PrepareContext(ctx, `INSERT OR IGNORE INTO jev_evaluation_daily (day, model_id, question_version, horizon_minutes,
	take_profit_bps, stop_loss_bps, barrier_cost_bps, decisions, evaluated, entered, wins,
	llm_sum_bps, random_sum_bps, momentum_sum_bps, contrarian_sum_bps, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return 0, fmt.Errorf("evalrecord: prepare insert: %w", err)
	}
	defer func() { _ = stmt.Close() }()
	created := sqlutil.FormatTime(time.Now())
	added := 0
	for _, x := range rows {
		res, err := stmt.ExecContext(ctx, x.Day, x.ModelID, x.QuestionVersion, x.HorizonMinutes, x.TakeProfitBps, x.StopLossBps, x.BarrierCostBps,
			x.Decisions, x.Evaluated, x.Entered, x.Wins, x.LLMSum, x.RandomSum, x.MomentumSum, x.ContrarianSum, created)
		if err != nil {
			return 0, fmt.Errorf("evalrecord: insert %s %s/%s: %w", x.Day, x.ModelID, x.QuestionVersion, err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return 0, fmt.Errorf("evalrecord: insert %s: %w", x.Day, err)
		}
		added += int(n)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("evalrecord: commit: %w", err)
	}
	return added, nil
}

// PurgeBefore deletes up to limit rows of jev_evaluation_daily whose day is before
// the given JST day and returns how many. One short statement; it touches no
// other table.
func (r *Repository) PurgeBefore(ctx context.Context, day string, limit int) (int, error) {
	if limit <= 0 {
		return 0, nil
	}
	if _, err := time.Parse(time.DateOnly, day); err != nil {
		return 0, fmt.Errorf("evalrecord: day %q: %w", day, err)
	}
	res, err := r.db.ExecContext(ctx, `DELETE FROM jev_evaluation_daily WHERE id IN (SELECT id FROM jev_evaluation_daily WHERE day < ? LIMIT ?)`, day, limit)
	if err != nil {
		return 0, fmt.Errorf("evalrecord: purge before %s: %w", day, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("evalrecord: purge before %s: %w", day, err)
	}
	return int(n), nil
}

// Filter narrows List; an empty field does not filter.
type Filter struct {
	ModelID, QuestionVersion string
	HorizonMinutes           *int
}

// List returns the rows of the JST days [from, to) ordered by model_id,
// question_version, horizon_minutes, day, through the day index. More than limit
// rows is ok=false (nothing partial is returned). It only reads.
func List(ctx context.Context, q Reader, from, to string, f Filter, limit int) (rows []Row, ok bool, err error) {
	if _, _, err := dayBounds(from, to); err != nil {
		return nil, false, err
	}
	var where strings.Builder
	args := []any{from, to}
	if f.ModelID != "" {
		where.WriteString(" AND model_id = ?")
		args = append(args, f.ModelID)
	}
	if f.QuestionVersion != "" {
		where.WriteString(" AND question_version = ?")
		args = append(args, f.QuestionVersion)
	}
	if f.HorizonMinutes != nil {
		where.WriteString(" AND horizon_minutes = ?")
		args = append(args, *f.HorizonMinutes)
	}
	args = append(args, limit+1)
	rs, err := q.QueryContext(ctx, `SELECT day, model_id, question_version, horizon_minutes, take_profit_bps, stop_loss_bps, barrier_cost_bps,
	decisions, evaluated, entered, wins, llm_sum_bps, random_sum_bps, momentum_sum_bps, contrarian_sum_bps
FROM jev_evaluation_daily WHERE day >= ? AND day < ?`+where.String()+`
ORDER BY model_id, question_version, horizon_minutes, day LIMIT ?`, args...)
	if err != nil {
		return nil, false, fmt.Errorf("evalrecord: list daily rows: %w", err)
	}
	defer func() { _ = rs.Close() }()
	for rs.Next() {
		var x Row
		if err := rs.Scan(&x.Day, &x.ModelID, &x.QuestionVersion, &x.HorizonMinutes, &x.TakeProfitBps, &x.StopLossBps, &x.BarrierCostBps,
			&x.Decisions, &x.Evaluated, &x.Entered, &x.Wins, &x.LLMSum, &x.RandomSum, &x.MomentumSum, &x.ContrarianSum); err != nil {
			return nil, false, fmt.Errorf("evalrecord: scan daily row: %w", err)
		}
		if len(rows) == limit {
			return nil, false, nil
		}
		rows = append(rows, x)
	}
	if err := rs.Err(); err != nil {
		return nil, false, fmt.Errorf("evalrecord: list daily rows: %w", err)
	}
	return rows, true, nil
}

// Span is what the table holds overall: the first and last recorded JST day and
// how many distinct days (all empty / 0 for an empty table).
type Span struct {
	FirstDay, LastDay string
	Days              int
}

// RecordedSpan reads the table's span from the day index only.
func RecordedSpan(ctx context.Context, q Reader) (Span, error) {
	var first, last sql.NullString
	var s Span
	if err := q.QueryRowContext(ctx, `SELECT MIN(day), MAX(day), COUNT(DISTINCT day) FROM jev_evaluation_daily`).Scan(&first, &last, &s.Days); err != nil {
		return Span{}, fmt.Errorf("evalrecord: recorded span: %w", err)
	}
	s.FirstDay, s.LastDay = first.String, last.String
	return s, nil
}

// dayBounds is the UTC range [lo, hi) of the JST days [from, to).
func dayBounds(from, to string) (lo, hi time.Time, err error) {
	jst := time.FixedZone("JST", 9*60*60)
	if lo, err = time.ParseInLocation(time.DateOnly, from, jst); err != nil {
		return lo, hi, fmt.Errorf("evalrecord: day %q: %w", from, err)
	}
	if hi, err = time.ParseInLocation(time.DateOnly, to, jst); err != nil {
		return lo, hi, fmt.Errorf("evalrecord: day %q: %w", to, err)
	}
	return lo, hi, nil
}

func scanDays(ctx context.Context, q Reader, stmt string, args ...any) ([]string, error) {
	rs, err := q.QueryContext(ctx, stmt, args...)
	if err != nil {
		return nil, fmt.Errorf("evalrecord: list days: %w", err)
	}
	defer func() { _ = rs.Close() }()
	var days []string
	for rs.Next() {
		var d string
		if err := rs.Scan(&d); err != nil {
			return nil, fmt.Errorf("evalrecord: scan day: %w", err)
		}
		days = append(days, d)
	}
	return days, rs.Err()
}
