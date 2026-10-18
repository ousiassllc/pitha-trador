package query

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

// closedPosition is the part of a closed positions row the aggregates need.
type closedPosition struct {
	closedAt time.Time
	pnl      float64
	notional float64 // entry_price * quantity
}

// loadClosed reads the closed positions of r (closed_at and realized_pnl both
// set), oldest first and ties by id. More than MaxClosedScan rows is
// ErrLimitExceeded: the aggregates never see a truncated set.
func loadClosed(ctx context.Context, q Q, r Range) ([]closedPosition, error) {
	from, to := r.bounds()
	rows, err := q.QueryContext(ctx, `
SELECT closed_at, realized_pnl, entry_price * quantity
FROM positions
WHERE closed_at IS NOT NULL AND realized_pnl IS NOT NULL
	AND closed_at >= ? AND closed_at < ?
ORDER BY closed_at ASC, id ASC
LIMIT ?`, from, to, MaxClosedScan+1)
	if err != nil {
		return nil, fmt.Errorf("query: closed positions: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []closedPosition
	for rows.Next() {
		var (
			closedAtText string
			p            closedPosition
		)
		if err := rows.Scan(&closedAtText, &p.pnl, &p.notional); err != nil {
			return nil, fmt.Errorf("query: scan closed position: %w", err)
		}
		if p.closedAt, err = sqlutil.ParseTime(closedAtText); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("query: closed positions: %w", err)
	}
	if len(out) > MaxClosedScan {
		return nil, ErrLimitExceeded
	}
	return out, nil
}

// Performance is paper_performance_summary's data (API spec §3.1). A ratio
// that is undefined is nil, so zero trades never read as 0%.
type Performance struct {
	ClosedTradeCount        int      `json:"closed_trade_count"`
	OpenPositionCount       int      `json:"open_position_count"`
	WinCount                int      `json:"win_count"`
	LossCount               int      `json:"loss_count"`
	FlatCount               int      `json:"flat_count"`
	WinRate                 *float64 `json:"win_rate"`
	TotalPnlJPY             float64  `json:"total_pnl_jpy"`
	ExpectancyJPY           *float64 `json:"expectancy_jpy"`
	ExpectancyReturnPct     *float64 `json:"expectancy_return_pct"`
	ProfitFactor            *float64 `json:"profit_factor"`
	MaxDrawdownJPY          float64  `json:"max_drawdown_jpy"`
	CapitalJPY              *float64 `json:"capital_jpy"`
	MaxDrawdownCapitalRatio *float64 `json:"max_drawdown_capital_ratio"`
}

// PerformanceSummary aggregates the positions closed in r. capitalJPY is the
// Paper initial capital; zero or negative (an unreadable risk.yaml) leaves
// capital_jpy and the drawdown ratio nil while the yen drawdown is still
// returned.
func PerformanceSummary(ctx context.Context, q Q, r Range, capitalJPY float64) (Performance, int, error) {
	closed, err := loadClosed(ctx, q, r)
	if err != nil {
		return Performance{}, 0, err
	}
	var open int
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM positions WHERE closed_at IS NULL`).Scan(&open); err != nil {
		return Performance{}, 0, fmt.Errorf("query: open positions: %w", err)
	}

	p := Performance{ClosedTradeCount: len(closed), OpenPositionCount: open}
	var winSum, lossSum, returnPctSum, equity, peak float64
	for _, c := range closed {
		p.TotalPnlJPY += c.pnl
		switch {
		case c.pnl > 0:
			p.WinCount++
			winSum += c.pnl
		case c.pnl < 0:
			p.LossCount++
			lossSum += c.pnl
		default:
			p.FlatCount++
		}
		// A non-positive entry notional counts as a 0% return, like
		// internal/service/insight.Aggregate.
		if c.notional > 0 {
			returnPctSum += c.pnl / c.notional * 100
		}
		// The curve starts at 0, so a first loss is already a drawdown.
		equity += c.pnl
		peak = math.Max(peak, equity)
		p.MaxDrawdownJPY = math.Max(p.MaxDrawdownJPY, peak-equity)
	}
	if n := len(closed); n > 0 {
		p.WinRate = ratio(p.WinCount, n)
		mean := p.TotalPnlJPY / float64(n)
		p.ExpectancyJPY = &mean
		meanPct := returnPctSum / float64(n)
		p.ExpectancyReturnPct = &meanPct
	}
	if p.LossCount > 0 {
		pf := winSum / math.Abs(lossSum)
		p.ProfitFactor = &pf
	}
	if capitalJPY > 0 {
		p.CapitalJPY = &capitalJPY
		dd := p.MaxDrawdownJPY / capitalJPY
		p.MaxDrawdownCapitalRatio = &dd
	}
	return p, len(closed), nil
}

// Day is one JST settlement day of paper_daily_pnl.
type Day struct {
	Date       string  `json:"date"`
	TradeCount int     `json:"trade_count"`
	PnlJPY     float64 `json:"pnl_jpy"`
	WinCount   int     `json:"win_count"`
	LossCount  int     `json:"loss_count"`
}

// DailyPnL groups the positions closed in r by JST close date, oldest day
// first. A day with no closes has no row.
func DailyPnL(ctx context.Context, q Q, r Range) ([]Day, error) {
	closed, err := loadClosed(ctx, q, r)
	if err != nil {
		return nil, err
	}
	days := []Day{}
	for _, c := range closed {
		date := c.closedAt.In(JST).Format(time.DateOnly)
		if len(days) == 0 || days[len(days)-1].Date != date {
			days = append(days, Day{Date: date})
		}
		d := &days[len(days)-1]
		d.TradeCount++
		d.PnlJPY += c.pnl
		switch {
		case c.pnl > 0:
			d.WinCount++
		case c.pnl < 0:
			d.LossCount++
		}
	}
	return days, nil
}
