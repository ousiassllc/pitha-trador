package labelcompare

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	rlabel "github.com/ousiassllc/pitha-trador/internal/domain/researchlabel"
	"github.com/ousiassllc/pitha-trador/internal/service/actuals"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// day is one JST day's aggregate of a series: n observations summing to sum.
type day struct {
	key int64 // actuals.JSTDay
	n   int
	sum float64
}

// series is the per-day aggregates of one (side) series of a window.
type series map[int64]*day

func (s series) add(key int64, n int, sum float64) {
	d := s[key]
	if d == nil {
		d = &day{key: key}
		s[key] = d
	}
	d.n += n
	d.sum += sum
}

// merge adds every day of o to s.
func (s series) merge(o series) {
	for _, d := range o {
		s.add(d.key, d.n, d.sum)
	}
}

// dayKey parses a JST date (YYYY-MM-DD) into actuals.JSTDay's number.
func dayKey(date string) (int64, error) {
	t, err := time.ParseInLocation(time.DateOnly, date, query.JST)
	if err != nil {
		return 0, fmt.Errorf("labelcompare: parse day %q: %w", date, err)
	}
	return actuals.JSTDay(t), nil
}

// decisionCounts counts a window's trader decisions by direction. It first
// counts them (a covering index range) and returns query.ErrLimitExceeded
// beyond MaxDecisions, so the direction grouping never scans more than that.
func decisionCounts(ctx context.Context, q query.Q, r query.Range) (Decisions, error) {
	from, to := r.Bounds()
	var total int
	err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM jev_decisions WHERE decision_type = 'trader' AND timestamp >= ? AND timestamp < ?`, from, to).Scan(&total)
	if err != nil {
		return Decisions{}, fmt.Errorf("labelcompare: count trader decisions: %w", err)
	}
	if total > MaxDecisions {
		return Decisions{}, query.ErrLimitExceeded
	}
	rows, err := q.QueryContext(ctx, `SELECT direction, COUNT(*) FROM jev_decisions
WHERE decision_type = 'trader' AND timestamp >= ? AND timestamp < ? GROUP BY direction`, from, to)
	if err != nil {
		return Decisions{}, fmt.Errorf("labelcompare: count decisions by direction: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out Decisions
	for rows.Next() {
		var (
			direction sql.NullString
			n         int
		)
		if err := rows.Scan(&direction, &n); err != nil {
			return Decisions{}, fmt.Errorf("labelcompare: scan decision count: %w", err)
		}
		switch direction.String {
		case domain.JevDirectionLong:
			out.Long = n
		case domain.JevDirectionShort:
			out.Short = n
		case domain.JevDirectionNone:
			out.None = n
		default:
			out.Unknown += n
		}
		out.Total += n
	}
	return out, rows.Err()
}

// storedLabel is the per-day aggregate of the stored label of one window,
// keyed by the label's horizon (minutes) and the decision's side.
type storedLabel map[int]map[string]series

// storedLabels reads, per JST day, side and label horizon, how many LONG /
// SHORT trader decisions of the window have a stored cost-inclusive label
// (calibration_outcomes, label_version 2, net_return_bps: the label with the
// cost of costsettings.LabelBarriers already in) and the sum of their net bps.
// One grouped statement over the decisions' timestamp index and the outcomes'
// (decision, horizon) index: no row of a decision is read here.
func storedLabels(ctx context.Context, q query.Q, r query.Range) (storedLabel, error) {
	from, to := r.Bounds()
	rows, err := q.QueryContext(ctx, `
SELECT date(d.timestamp, '+9 hours'), d.direction, o.horizon_minutes, COUNT(*), SUM(o.net_return_bps)
FROM jev_decisions d
JOIN calibration_outcomes o ON o.jev_decision_id = d.id AND o.label_version = ? AND o.net_return_bps IS NOT NULL
WHERE d.decision_type = 'trader' AND d.direction IN ('LONG', 'SHORT') AND d.timestamp >= ? AND d.timestamp < ?
GROUP BY 1, 2, 3`, domain.CalibrationLabelVersionTripleBarrier, from, to)
	if err != nil {
		return nil, fmt.Errorf("labelcompare: read stored labels: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := storedLabel{}
	for rows.Next() {
		var (
			date, side string
			horizon, n int
			sum        float64
		)
		if err := rows.Scan(&date, &side, &horizon, &n, &sum); err != nil {
			return nil, fmt.Errorf("labelcompare: scan stored label: %w", err)
		}
		key, err := dayKey(date)
		if err != nil {
			return nil, err
		}
		if out[horizon] == nil {
			out[horizon] = map[string]series{}
		}
		if out[horizon][side] == nil {
			out[horizon][side] = series{}
		}
		out[horizon][side].add(key, n, sum)
	}
	return out, rows.Err()
}

// rows is the number of labeled decisions at horizon (both sides).
func (s storedLabel) rows(horizon int) (n int) {
	for _, ser := range s[horizon] {
		for _, d := range ser {
			n += d.n
		}
	}
	return n
}

// researchSeries is a window's research label day sums, by horizon and side.
type researchSeries map[string]map[string]series

// researchLabels reads research_label_day_stats for the JST days that lie wholly
// inside r and that the recorder completed (research_label_days): a partly
// covered edge day, or a day the recorder stopped in the middle of, is left
// out. One row per (day, horizon, side), at most MaxWindowDays * 6.
func researchLabels(ctx context.Context, q query.Q, r query.Range) (researchSeries, error) {
	first, last, ok := wholeDays(r)
	if !ok {
		return researchSeries{}, nil
	}
	rows, err := q.QueryContext(ctx, `
SELECT s.day, s.horizon, s.side, s.row_count, s.sum_net
FROM research_label_day_stats s JOIN research_label_days d ON d.day = s.day
WHERE s.day >= ? AND s.day <= ?`, first, last)
	if err != nil {
		return nil, fmt.Errorf("labelcompare: read research label day sums: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := researchSeries{}
	for rows.Next() {
		var (
			date, horizon, side string
			n                   int
			sum                 float64
		)
		if err := rows.Scan(&date, &horizon, &side, &n, &sum); err != nil {
			return nil, fmt.Errorf("labelcompare: scan research label day sum: %w", err)
		}
		key, err := dayKey(date)
		if err != nil {
			return nil, err
		}
		if out[horizon] == nil {
			out[horizon] = map[string]series{}
		}
		if out[horizon][side] == nil {
			out[horizon][side] = series{}
		}
		out[horizon][side].add(key, n, sum)
	}
	return out, rows.Err()
}

// wholeDays are the first and last JST dates (YYYY-MM-DD) whose whole day lies
// inside [r.From, r.To); ok is false when none does.
func wholeDays(r query.Range) (first, last string, ok bool) {
	from := r.From.In(query.JST)
	start := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, query.JST)
	if start.Before(from) {
		start = start.AddDate(0, 0, 1)
	}
	end := r.To.In(query.JST)
	finish := time.Date(end.Year(), end.Month(), end.Day(), 0, 0, 0, 0, query.JST) // the start of end's day: the last day wholly inside ends here
	if !start.Before(finish) {
		return "", "", false
	}
	return start.Format(time.DateOnly), finish.AddDate(0, 0, -1).Format(time.DateOnly), true
}

// horizons are the research horizons in output order.
func horizons() []string { return rlabel.Horizons }
