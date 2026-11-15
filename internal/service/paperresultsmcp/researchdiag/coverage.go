package researchdiag

import (
	"context"
	"fmt"
	"sort"
	"time"

	rlabel "github.com/ousiassllc/pitha-trador/internal/domain/researchlabel"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// coverage compares the period's snapshots (or decisions) with the labeled rows
// of each horizon, and lists the days with their recording state.
func coverage(ctx context.Context, q query.Q, req Request, r query.Range, now time.Time) (Coverage, error) {
	from, to := r.Bounds()
	c := Coverage{Unit: "snapshots", Labeled: map[string]int{}, SnapshotDays: []DayCoverage{}}
	days := map[string]*DayCoverage{}
	dayOf := func(d string) *DayCoverage {
		if days[d] == nil {
			days[d] = &DayCoverage{Day: d, Labeled: map[string]int{}}
		}
		return days[d]
	}

	// Days the recorder completed keep the bars it read (market_snapshots may be
	// purged since) and the rows it labeled per horizon; every other day of the
	// period is counted from market_snapshots and has no labeled rows in the
	// summary (a day the recorder has not completed is not summarized).
	fromDay, toDay := dayBounds(r)
	rows, err := q.QueryContext(ctx, `SELECT day, snapshots, labeled_30m, labeled_60m, labeled_close FROM research_label_days WHERE day >= ? AND day < ?`, fromDay, toDay)
	if err != nil {
		return c, fmt.Errorf("researchdiag: coverage of recorded days: %w", err)
	}
	for rows.Next() {
		var (
			day              string
			n, l30, l60, lcl int
		)
		if err := rows.Scan(&day, &n, &l30, &l60, &lcl); err != nil {
			_ = rows.Close()
			return c, fmt.Errorf("researchdiag: scan coverage of recorded days: %w", err)
		}
		d := dayOf(day)
		d.Recorded, d.Snapshots = true, n
		d.Labeled[rlabel.Horizon30m], d.Labeled[rlabel.Horizon60m], d.Labeled[rlabel.HorizonClose] = l30, l60, lcl
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return c, err
	}
	_ = rows.Close()
	for day := r.From.In(query.JST); day.Before(r.To); day = day.AddDate(0, 0, 1) {
		name := day.Format(time.DateOnly)
		if d := days[name]; d != nil && d.Recorded {
			continue
		}
		var n int
		err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM market_snapshots WHERE timestamp >= ? AND timestamp < ?`,
			sqlutil.FormatTime(day), sqlutil.FormatTime(day.AddDate(0, 0, 1))).Scan(&n)
		if err != nil {
			return c, fmt.Errorf("researchdiag: count snapshots of %s: %w", name, err)
		}
		if n > 0 {
			dayOf(name).Snapshots = n
		}
	}

	today := now.In(query.JST).Format(time.DateOnly)
	for _, d := range days {
		c.SnapshotDays = append(c.SnapshotDays, *d)
		if req.scope() == ScopeSnapshots {
			c.InPeriod += d.Snapshots
			for h, n := range d.Labeled {
				c.Labeled[h] += n
			}
		}
		if !d.Recorded && d.Snapshots > 0 && d.Day < today {
			c.UnrecordedDays++
		}
	}
	sort.Slice(c.SnapshotDays, func(i, j int) bool { return c.SnapshotDays[i].Day < c.SnapshotDays[j].Day })
	if req.scope() == ScopeSnapshots {
		return c, nil
	}
	return decisionCoverage(ctx, q, req, from, to, c)
}

// decisionCoverage counts the decisions of a decision scope (LONG/SHORT, of the
// direction filter) in the period and, per horizon, those with a labeled row at
// their direction.
func decisionCoverage(ctx context.Context, q query.Q, req Request, from, to string, c Coverage) (Coverage, error) {
	c.Unit = "decisions"
	where, args := req.decisionWhere(from, to)
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM jev_decisions d WHERE `+where, args...).Scan(&c.InPeriod); err != nil {
		return c, fmt.Errorf("researchdiag: count decisions: %w", err)
	}
	rows, err := q.QueryContext(ctx, `SELECT r.horizon, COUNT(*) FROM jev_decisions d
JOIN research_horizon_labels r ON r.instrument_id = d.instrument_id AND r.snapshot_at = d.timestamp
WHERE `+where+` AND CASE d.direction WHEN 'LONG' THEN r.long_net_bps ELSE r.short_net_bps END IS NOT NULL
	AND r.day IN (SELECT day FROM research_label_days) GROUP BY r.horizon`, args...)
	if err != nil {
		return c, fmt.Errorf("researchdiag: count labeled decisions: %w", err)
	}
	defer func() { _ = rows.Close() }()
	c.Labeled = map[string]int{}
	for rows.Next() {
		var (
			horizon string
			n       int
		)
		if err := rows.Scan(&horizon, &n); err != nil {
			return c, fmt.Errorf("researchdiag: scan labeled decisions: %w", err)
		}
		c.Labeled[horizon] = n
	}
	return c, rows.Err()
}
