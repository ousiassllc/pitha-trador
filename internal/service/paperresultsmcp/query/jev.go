package query

import (
	"context"
	"database/sql"
	"fmt"
)

// Bucket is one confidence band of jev_calibration (FR-CAL-3).
type Bucket struct {
	Low         float64  `json:"low"`
	High        float64  `json:"high"`
	SampleCount int      `json:"sample_count"`
	HitRate     *float64 `json:"hit_rate"`
}

// BarrierCounts are the triple-barrier outcomes of label_version 2.
type BarrierCounts struct {
	TP      int `json:"tp"`
	SL      int `json:"sl"`
	Timeout int `json:"timeout"`
}

// LabelVersionStats is one by_label_version element (API spec §3.4). Labels
// of different versions define "correct" differently (FR-CAL-4), so they are
// never mixed into one figure.
type LabelVersionStats struct {
	LabelVersion     int           `json:"label_version"`
	SampleCount      int           `json:"sample_count"`
	ExcludedCount    int           `json:"excluded_count"`
	HitRate          *float64      `json:"hit_rate"`
	Brier            *float64      `json:"brier"`
	Buckets          []Bucket      `json:"buckets"`
	OutOfBucketCount int           `json:"out_of_bucket_count"`
	BarrierCounts    BarrierCounts `json:"barrier_counts"`
	AvgNetReturnBps  *float64      `json:"avg_net_return_bps"`
}

// Horizon is one jev_calibration horizon.
type Horizon struct {
	HorizonMinutes int                 `json:"horizon_minutes"`
	ByLabelVersion []LabelVersionStats `json:"by_label_version"`
}

// DecisionCounts are the Jev decisions made in the period.
type DecisionCounts struct {
	ScoutCount  int `json:"scout_count"`
	TraderCount int `json:"trader_count"`
}

// Calibration is jev_calibration's data.
type Calibration struct {
	Decisions DecisionCounts `json:"decisions"`
	Horizons  []Horizon      `json:"horizons"`
}

// bucketBands are domain.DefaultConfidenceBucketRanges; the last band
// includes its upper bound. Kept literal so the SQL CASE below and this list
// are reviewed together.
var bucketBands = [5][2]float64{{0.50, 0.60}, {0.60, 0.70}, {0.70, 0.80}, {0.80, 0.90}, {0.90, 1.00}}

// bucketCase maps confidence to a band index 0..4, or -1 outside [0.50, 1.00].
const bucketCase = `CASE
	WHEN d.confidence < 0.50 OR d.confidence > 1.00 THEN -1
	WHEN d.confidence >= 0.90 THEN 4
	WHEN d.confidence >= 0.80 THEN 3
	WHEN d.confidence >= 0.70 THEN 2
	WHEN d.confidence >= 0.60 THEN 1
	ELSE 0 END`

// JevCalibration aggregates Jev trader decisions' labels in r. horizon > 0
// restricts to one horizon. More than MaxHorizons distinct horizons is
// ErrLimitExceeded.
func JevCalibration(ctx context.Context, q Q, r Range, horizon int) (Calibration, error) {
	from, to := r.bounds()
	var c Calibration
	err := q.QueryRowContext(ctx, `
SELECT COALESCE(SUM(decision_type = 'scout'), 0), COALESCE(SUM(decision_type = 'trader'), 0)
FROM jev_decisions WHERE timestamp >= ? AND timestamp < ?`, from, to).
		Scan(&c.Decisions.ScoutCount, &c.Decisions.TraderCount)
	if err != nil {
		return Calibration{}, fmt.Errorf("query: jev decision counts: %w", err)
	}
	c.Horizons = []Horizon{}

	horizonFilter, args := "", []any{from, to}
	if horizon > 0 {
		horizonFilter = " AND co.horizon_minutes = ?"
		args = append(args, horizon)
	}
	const joined = `
FROM calibration_outcomes co
JOIN jev_decisions d ON d.id = co.jev_decision_id
WHERE d.decision_type = 'trader' AND d.timestamp >= ? AND d.timestamp < ?`

	rows, err := q.QueryContext(ctx, `
SELECT co.horizon_minutes, co.label_version, COUNT(*),
	SUM(d.confidence IS NULL OR co.was_direction_correct IS NULL),
	COUNT(co.was_direction_correct), COALESCE(SUM(co.was_direction_correct = 1), 0),
	SUM(d.confidence IS NOT NULL AND co.was_direction_correct IS NOT NULL),
	SUM(CASE WHEN d.confidence IS NOT NULL AND co.was_direction_correct IS NOT NULL
		THEN (d.confidence - (co.was_direction_correct <> 0)) * (d.confidence - (co.was_direction_correct <> 0)) END),
	SUM(d.confidence IS NOT NULL AND co.was_direction_correct IS NOT NULL AND `+bucketCase+` = -1),
	COALESCE(SUM(co.barrier_hit = 'tp'), 0), COALESCE(SUM(co.barrier_hit = 'sl'), 0),
	COALESCE(SUM(co.barrier_hit = 'timeout'), 0), AVG(co.net_return_bps)`+joined+horizonFilter+`
GROUP BY co.horizon_minutes, co.label_version
ORDER BY co.horizon_minutes, co.label_version`, args...)
	if err != nil {
		return Calibration{}, fmt.Errorf("query: jev calibration: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var (
			h                      int
			s                      LabelVersionStats
			hitKnown, hitTrue, use int
			brierSum, avgNet       sql.NullFloat64
		)
		if err := rows.Scan(&h, &s.LabelVersion, &s.SampleCount, &s.ExcludedCount, &hitKnown, &hitTrue,
			&use, &brierSum, &s.OutOfBucketCount,
			&s.BarrierCounts.TP, &s.BarrierCounts.SL, &s.BarrierCounts.Timeout, &avgNet); err != nil {
			return Calibration{}, fmt.Errorf("query: scan jev calibration: %w", err)
		}
		s.HitRate = ratio(hitTrue, hitKnown)
		if brierSum.Valid && use > 0 {
			b := brierSum.Float64 / float64(use)
			s.Brier = &b
		}
		if s.LabelVersion == 2 {
			s.AvgNetReturnBps = floatPtr(avgNet)
		} else {
			s.BarrierCounts = BarrierCounts{}
		}
		s.Buckets = emptyBuckets()
		if n := len(c.Horizons); n == 0 || c.Horizons[n-1].HorizonMinutes != h {
			if n == MaxHorizons {
				return Calibration{}, ErrLimitExceeded
			}
			c.Horizons = append(c.Horizons, Horizon{HorizonMinutes: h})
		}
		last := &c.Horizons[len(c.Horizons)-1]
		last.ByLabelVersion = append(last.ByLabelVersion, s)
	}
	if err := rows.Err(); err != nil {
		return Calibration{}, fmt.Errorf("query: jev calibration: %w", err)
	}
	_ = rows.Close()

	return c, fillBuckets(ctx, q, &c, joined+horizonFilter, args)
}

func emptyBuckets() []Bucket {
	b := make([]Bucket, len(bucketBands))
	for i, band := range bucketBands {
		b[i] = Bucket{Low: band[0], High: band[1]}
	}
	return b
}

// fillBuckets adds the per-band counts and hit rates to c's label versions.
func fillBuckets(ctx context.Context, q Q, c *Calibration, joined string, args []any) error {
	rows, err := q.QueryContext(ctx, `
SELECT co.horizon_minutes, co.label_version, `+bucketCase+` AS band, COUNT(*), SUM(co.was_direction_correct <> 0)`+
		joined+` AND d.confidence IS NOT NULL AND co.was_direction_correct IS NOT NULL
GROUP BY co.horizon_minutes, co.label_version, band`, args...)
	if err != nil {
		return fmt.Errorf("query: jev calibration buckets: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var h, lv, band, n, hits int
		if err := rows.Scan(&h, &lv, &band, &n, &hits); err != nil {
			return fmt.Errorf("query: scan jev calibration buckets: %w", err)
		}
		if band < 0 || band >= len(bucketBands) {
			continue // counted as out_of_bucket_count already
		}
		for i := range c.Horizons {
			if c.Horizons[i].HorizonMinutes != h {
				continue
			}
			for j := range c.Horizons[i].ByLabelVersion {
				if s := &c.Horizons[i].ByLabelVersion[j]; s.LabelVersion == lv {
					s.Buckets[band].SampleCount = n
					s.Buckets[band].HitRate = ratio(hits, n)
				}
			}
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("query: jev calibration buckets: %w", err)
	}
	return nil
}
