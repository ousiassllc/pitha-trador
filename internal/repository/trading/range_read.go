package trading

import (
	"context"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

// The Each*InRange methods stream a whole period for internal/service/dataexport
// (取引ログ export, issue #770): every row whose timestamp falls in the
// half-open range [from, to), oldest first, one fn call per row. They never
// materialize the period; the first error fn returns stops the read and is
// returned wrapped (errors.Is / errors.As still match it).

// EachSubmittedInRange calls fn for every paper_orders row submitted in
// [from, to), oldest first (submitted_at, then id).
func (r *OrderRepository) EachSubmittedInRange(ctx context.Context, from, to time.Time, fn func(domain.PaperOrder) error) error {
	err := sqlutil.Each(ctx, r.db, scanOrder, fn,
		orderSelectColumns+` WHERE submitted_at >= ? AND submitted_at < ? ORDER BY submitted_at ASC, id ASC`,
		sqlutil.FormatTime(from), sqlutil.FormatTime(to))
	return wrapRange(err, "paper orders", from, to)
}

// EachInRange calls fn for every trade_signals row timestamped in
// [from, to), oldest first.
func (r *SignalRepository) EachInRange(ctx context.Context, from, to time.Time, fn func(domain.TradeSignal) error) error {
	err := sqlutil.Each(ctx, r.db, scanSignal, fn,
		signalSelectColumns+` WHERE timestamp >= ? AND timestamp < ? ORDER BY timestamp ASC, id ASC`,
		sqlutil.FormatTime(from), sqlutil.FormatTime(to))
	return wrapRange(err, "trade signals", from, to)
}

// EachByDecisionRange calls fn for every trade_signals row derived from a
// jev_decisions row timestamped in [from, to), ordered by decision id then
// signal id: the skip/reject reasons of the decision-log export (issue
// #771). Signals with no Jev decision (jev_decision_id NULL) are not
// returned; the trade log carries them.
func (r *SignalRepository) EachByDecisionRange(ctx context.Context, from, to time.Time, fn func(domain.TradeSignal) error) error {
	err := sqlutil.Each(ctx, r.db, scanSignal, fn,
		signalSelectColumns+` WHERE jev_decision_id IN (SELECT id FROM jev_decisions WHERE timestamp >= ? AND timestamp < ?) ORDER BY jev_decision_id ASC, id ASC`,
		sqlutil.FormatTime(from), sqlutil.FormatTime(to))
	return wrapRange(err, "trade signals by decision", from, to)
}

// wrapRange labels a read error with what was read and the period; it
// returns nil for a nil err.
func wrapRange(err error, what string, from, to time.Time) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("repository: read %s in [%s, %s): %w", what, from.UTC().Format(time.RFC3339), to.UTC().Format(time.RFC3339), err)
}

// ListTouchingBetween returns every position opened or closed in
// [start, end) - closed ones and still-open ones alike - oldest opened
// first (then id). It is the read of cmd/dailyguardreplay (issue #782): a
// day's trade count is by open day and its realized P&L by close day, so the
// replay needs both the positions opened in the range and the ones closed
// in it.
func (r *PositionRepository) ListTouchingBetween(ctx context.Context, start, end time.Time) ([]domain.Position, error) {
	from, to := sqlutil.FormatTime(start), sqlutil.FormatTime(end)
	rows, err := r.db.QueryContext(ctx,
		positionSelectColumns+` WHERE (opened_at >= ? AND opened_at < ?) OR (closed_at >= ? AND closed_at < ?) ORDER BY opened_at ASC, id ASC`,
		from, to, from, to,
	)
	if err != nil {
		return nil, wrapRange(err, "positions opened or closed", start, end)
	}
	return scanPositions(rows)
}
