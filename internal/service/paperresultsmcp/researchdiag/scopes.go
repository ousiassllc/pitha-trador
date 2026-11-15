package researchdiag

import (
	"context"
	"database/sql"
	"fmt"
	"slices"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	rlabel "github.com/ousiassllc/pitha-trador/internal/domain/researchlabel"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// snapshotsAcc reads the snapshots scope: the day sums research_label_day_stats
// holds (every labeled snapshot of the recorded days, both sides) and a sample
// of the rows for the medians.
func snapshotsAcc(ctx context.Context, q query.Q, r query.Range, recorded map[string]bool) (*acc, error) {
	a := newAcc()
	fromDay, toDay := dayBounds(r)
	rows, err := q.QueryContext(ctx, `SELECT horizon, side, day, row_count, sum_gross, sum_net, sum_cost, wins, spread_rows, sum_spread,
	spread_above_cost, sum_gross_less_half_spread FROM research_label_day_stats WHERE day >= ? AND day < ?`, fromDay, toDay)
	if err != nil {
		return nil, fmt.Errorf("researchdiag: read research day sums: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var (
			k   key
			day string
			d   dayRow
		)
		if err := rows.Scan(&k.horizon, &k.side, &day, &d.n, &d.gross, &d.net, &d.cost, &d.wins, &d.withSpread, &d.spread, &d.aboveCost, &d.grossLessHalfSpread); err != nil {
			return nil, fmt.Errorf("researchdiag: scan research day sums: %w", err)
		}
		if recorded[day] {
			a.day(k, day)
			*a.days[k][day] = d
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	_ = rows.Close()
	days := make([]string, 0, len(recorded))
	for day := range recorded {
		days = append(days, day)
	}
	slices.Sort(days)
	for _, h := range rlabel.Horizons {
		n := max(a.total(key{h, domain.JevDirectionLong}).n, a.total(key{h, domain.JevDirectionShort}).n)
		if n == 0 {
			continue
		}
		percent := samplePercent(n)
		a.percent[h] = percent
		for _, day := range days {
			if err := a.readSample(ctx, q, day, h, percent); err != nil {
				return nil, err
			}
		}
	}
	return a, nil
}

// samplePercent is the share (percent of the sample buckets) of a horizon's n
// rows read for the medians: all of them up to MedianSampleCap rows, then the
// share that keeps about MedianSampleCap (at least 1%).
func samplePercent(n int) int {
	return max(1, min(sampleBuckets, sampleBuckets*MedianSampleCap/max(n, 1)))
}

// readSample reads the rows of one day and horizon whose sample_bucket is below
// percent as the observations of the medians (the index serves it: day, horizon,
// bucket range).
func (a *acc) readSample(ctx context.Context, q query.Q, day, horizon string, percent int) error {
	rows, err := q.QueryContext(ctx, `SELECT long_gross_bps, long_net_bps, long_cost_bps, short_gross_bps, short_net_bps, short_cost_bps, spread_bps
FROM research_horizon_labels WHERE day = ? AND horizon = ? AND sample_bucket < ?`, day, horizon, percent)
	if err != nil {
		return fmt.Errorf("researchdiag: read research label sample of %s: %w", day, err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var lg, ln, lc, sg, sn, sc, spread sql.NullFloat64
		if err := rows.Scan(&lg, &ln, &lc, &sg, &sn, &sc, &spread); err != nil {
			return fmt.Errorf("researchdiag: scan research label sample: %w", err)
		}
		if ln.Valid {
			a.observe(key{horizon, domain.JevDirectionLong}, day, lg.Float64, ln.Float64, lc.Float64, spread, false)
		}
		if sn.Valid {
			a.observe(key{horizon, domain.JevDirectionShort}, day, sg.Float64, sn.Float64, sc.Float64, spread, false)
		}
	}
	return rows.Err()
}

// decisionsAcc reads a decision scope: one row per LONG/SHORT decision of the
// type (and direction filter) in the period, joined to the research row of its
// snapshot at the decision's own direction. The rows are summed here: decisions
// are far fewer than snapshots.
func decisionsAcc(ctx context.Context, q query.Q, r query.Range, req Request, recorded map[string]bool) (*acc, error) {
	a := newAcc()
	from, to := r.Bounds()
	where, args := req.decisionWhere(from, to)
	rows, err := q.QueryContext(ctx, `SELECT r.horizon, d.direction, r.day,
	CASE d.direction WHEN 'LONG' THEN r.long_gross_bps ELSE r.short_gross_bps END,
	CASE d.direction WHEN 'LONG' THEN r.long_net_bps ELSE r.short_net_bps END,
	CASE d.direction WHEN 'LONG' THEN r.long_cost_bps ELSE r.short_cost_bps END, r.spread_bps
FROM jev_decisions d JOIN research_horizon_labels r ON r.instrument_id = d.instrument_id AND r.snapshot_at = d.timestamp
WHERE `+where+` AND CASE d.direction WHEN 'LONG' THEN r.long_net_bps ELSE r.short_net_bps END IS NOT NULL`, args...)
	if err != nil {
		return nil, fmt.Errorf("researchdiag: read research labels of decisions: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var (
			k                key
			day              string
			gross, net, cost float64
			spread           sql.NullFloat64
		)
		if err := rows.Scan(&k.horizon, &k.side, &day, &gross, &net, &cost, &spread); err != nil {
			return nil, fmt.Errorf("researchdiag: scan research label of a decision: %w", err)
		}
		if recorded[day] {
			a.observe(k, day, gross, net, cost, spread, true)
			a.percent[k.horizon] = 100
		}
	}
	return a, rows.Err()
}
