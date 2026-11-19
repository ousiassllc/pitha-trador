package virtualpaper

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

// Day statuses (virtual_paper_days.status).
const (
	// DayDone is a day that was replayed (it may hold no trade).
	DayDone = "done"
	// DayEmpty is a day without any market_snapshots bar: nothing to replay.
	DayEmpty = "empty"
	// DayFailed is a day whose replay failed (Error says why); it is not retried.
	DayFailed = "failed"
)

// ExitReasonDataEnded marks a trade the replay closed only because the data
// ended (backtest.ExitReasonDataEnded): recorded, but left out of the metrics.
const ExitReasonDataEnded = "data_ended"

// ErrTooManyRows is returned by FinishedTrades when a profile has more rows than
// the caller's limit.
var ErrTooManyRows = errors.New("virtualpaper: too many rows")

// Day is one virtual_paper_days row.
type Day struct {
	ProfileID     int64
	Day           string
	Status        string
	Bars          int
	DecisionsSeen int
	Trades        int
	Unfinished    int
	Error         string
}

// Trade is one virtual_paper_trades row. The returns are percentages (1.0 = +1%),
// direction adjusted; Net is after slippage and fees.
type Trade struct {
	InstrumentID int64
	Symbol       string
	Direction    string
	EntryAt      time.Time
	EntryPrice   float64
	ExitAt       time.Time
	ExitPrice    float64
	ExitReason   string
	Quantity     int64
	Gross        float64
	Slippage     float64
	Fee          float64
	Net          float64
	// PnLJPY is Net × Quantity × EntryPrice / 100, computed by the recorder.
	PnLJPY float64
}

// TradeRow is the part of a recorded trade the comparison reads.
type TradeRow struct {
	Day          string
	InstrumentID int64
	Symbol       string
	EntryAt      time.Time
	ExitAt       time.Time
	Net          float64
	PnLJPY       float64
}

// RecordDay stores trades of d.Day and the day's mark in ONE transaction, so a
// day with a mark is completely recorded and a day without one is recorded again
// from scratch. Trades are ON CONFLICT DO NOTHING on (profile, instrument, entry time) (only that
// uniqueness is tolerated: a row that breaks a CHECK fails the whole day):
// replaying the same day twice never duplicates a row. It writes only the
// virtual_paper_trades and virtual_paper_days tables.
func RecordDay(ctx context.Context, db *sql.DB, d Day, trades []Trade, now time.Time) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("virtualpaper: begin day transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	at := sqlutil.FormatTime(now)
	for _, t := range trades {
		if _, err := tx.ExecContext(ctx, `INSERT INTO virtual_paper_trades
(profile_id, day, instrument_id, symbol, direction, entry_at, entry_price, exit_at, exit_price, exit_reason, quantity,
 gross_return_pct, slippage_return_pct, fee_return_pct, net_return_pct, pnl_jpy, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (profile_id, instrument_id, entry_at) DO NOTHING`,
			d.ProfileID, d.Day, t.InstrumentID, t.Symbol, t.Direction, sqlutil.FormatTime(t.EntryAt), t.EntryPrice,
			sqlutil.FormatTime(t.ExitAt), t.ExitPrice, t.ExitReason, t.Quantity,
			t.Gross, t.Slippage, t.Fee, t.Net, t.PnLJPY, at); err != nil {
			return fmt.Errorf("virtualpaper: insert trade: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO virtual_paper_days
(profile_id, day, status, bars, decisions_seen, trades, unfinished, error, completed_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		d.ProfileID, d.Day, d.Status, d.Bars, d.DecisionsSeen, d.Trades, d.Unfinished, d.Error, at); err != nil {
		return fmt.Errorf("virtualpaper: mark day: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("virtualpaper: commit day: %w", err)
	}
	return nil
}

// MarkedDays returns the days from `from` (inclusive, "2006-01-02") of profile
// that have a mark of any status.
func MarkedDays(ctx context.Context, q Querier, profileID int64, from string) (map[string]bool, error) {
	rows, err := q.QueryContext(ctx, `SELECT day FROM virtual_paper_days WHERE profile_id = ? AND day >= ?`, profileID, from)
	if err != nil {
		return nil, fmt.Errorf("virtualpaper: list marked days: %w", err)
	}
	defer func() { _ = rows.Close() }()
	marked := map[string]bool{}
	for rows.Next() {
		var day string
		if err := rows.Scan(&day); err != nil {
			return nil, fmt.Errorf("virtualpaper: scan marked day: %w", err)
		}
		marked[day] = true
	}
	return marked, rows.Err()
}

// Days returns the marks of profile for the days in [from, to), oldest first.
func Days(ctx context.Context, q Querier, profileID int64, from, to string) ([]Day, error) {
	rows, err := q.QueryContext(ctx, `SELECT day, status, bars, decisions_seen, trades, unfinished, error
FROM virtual_paper_days WHERE profile_id = ? AND day >= ? AND day < ? ORDER BY day`, profileID, from, to)
	if err != nil {
		return nil, fmt.Errorf("virtualpaper: list days: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []Day
	for rows.Next() {
		d := Day{ProfileID: profileID}
		if err := rows.Scan(&d.Day, &d.Status, &d.Bars, &d.DecisionsSeen, &d.Trades, &d.Unfinished, &d.Error); err != nil {
			return nil, fmt.Errorf("virtualpaper: scan day: %w", err)
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// ProfileIDsWithDoneDays returns the ids of the profiles that have at least one
// completed day in [from, to), ascending.
func ProfileIDsWithDoneDays(ctx context.Context, q Querier, from, to string) ([]int64, error) {
	rows, err := q.QueryContext(ctx, `SELECT DISTINCT profile_id FROM virtual_paper_days
WHERE status = 'done' AND day >= ? AND day < ? ORDER BY profile_id`, from, to)
	if err != nil {
		return nil, fmt.Errorf("virtualpaper: list profiles with data: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("virtualpaper: scan profile id: %w", err)
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// FinishedTrades returns the trades of profile entered in [from, to) JST days
// that the replay closed by an exit condition (not ExitReasonDataEnded), oldest
// exit first; ErrTooManyRows when there are more than limit.
func FinishedTrades(ctx context.Context, q Querier, profileID int64, from, to string, limit int) ([]TradeRow, error) {
	rows, err := q.QueryContext(ctx, `SELECT day, instrument_id, symbol, entry_at, exit_at, net_return_pct, pnl_jpy
FROM virtual_paper_trades WHERE profile_id = ? AND day >= ? AND day < ? AND exit_reason <> ?
ORDER BY exit_at, instrument_id LIMIT ?`, profileID, from, to, ExitReasonDataEnded, limit+1)
	if err != nil {
		return nil, fmt.Errorf("virtualpaper: list trades: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []TradeRow
	for rows.Next() {
		var (
			r             TradeRow
			entry, exitAt string
		)
		if err := rows.Scan(&r.Day, &r.InstrumentID, &r.Symbol, &entry, &exitAt, &r.Net, &r.PnLJPY); err != nil {
			return nil, fmt.Errorf("virtualpaper: scan trade: %w", err)
		}
		if r.EntryAt, err = sqlutil.ParseTime(entry); err != nil {
			return nil, err
		}
		if r.ExitAt, err = sqlutil.ParseTime(exitAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("virtualpaper: list trades: %w", err)
	}
	if len(out) > limit {
		return nil, ErrTooManyRows
	}
	return out, nil
}
