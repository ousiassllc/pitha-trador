package featuremodel

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

// TrainRow is one training example: a market snapshot with its 30-minute
// label. X is aligned with Features (NaN: missing). HasLong/HasShort tell which
// sides had an executable quote, so which of LongNet/ShortNet exist.
type TrainRow struct {
	SnapshotID        int64
	Day               string
	X                 []float64
	LongNet, ShortNet float64
	HasLong, HasShort bool
}

// Decision is one trader LONG/SHORT Jev decision with the features of its own bar.
type Decision struct {
	ID, InstrumentID int64
	At               time.Time
	X                []float64
}

// TrainingDays returns the up-to-limit most recent JST days strictly before the
// day `before` that research_label_days marks complete and that hold 30-minute rows
// (labeled_30m > 0), oldest first. A day without the mark (the
// recorder stopped midway) is never returned.
func (r *Repository) TrainingDays(ctx context.Context, before string, limit int) ([]string, error) {
	if limit <= 0 {
		return nil, nil
	}
	rows, err := r.db.QueryContext(ctx, `SELECT day FROM research_label_days WHERE day < ? AND labeled_30m > 0 ORDER BY day DESC LIMIT ?`, before, limit)
	if err != nil {
		return nil, fmt.Errorf("featuremodel: list training days: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var days []string
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			return nil, fmt.Errorf("featuremodel: scan training day: %w", err)
		}
		days = append(days, d)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i, j := 0, len(days)-1; i < j; i, j = i+1, j-1 {
		days[i], days[j] = days[j], days[i]
	}
	return days, nil
}

// ScanTraining calls fn for every training row of the given days, in the fixed
// order (day, snapshot_id): the 30-minute rows of the label table with
// sample_bucket < buckets whose label is fully known before notBefore (exit_at <
// the start of the predicted day: the purge), joined to the snapshot's features.
// A row whose snapshot has been purged from market_snapshots has no features and
// is not a row. purged is how many rows the exit_at rule dropped. The fixed order
// makes every sum over the rows reproducible bit for bit.
func (r *Repository) ScanTraining(ctx context.Context, days []string, buckets int, predictDay string, fn func(TrainRow) error) (purged int, err error) {
	cutoff, err := formatDayStart(predictDay)
	if err != nil {
		return 0, err
	}
	for _, day := range days {
		if day >= predictDay {
			return purged, fmt.Errorf("featuremodel: training day %s is not before the predicted day %s", day, predictDay)
		}
		if err := r.scanDay(ctx, day, buckets, cutoff, fn); err != nil {
			return purged, err
		}
		var dropped int
		if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM research_horizon_labels l JOIN market_snapshots m ON m.id = l.snapshot_id
WHERE l.day = ? AND l.horizon = '30m' AND l.sample_bucket < ? AND l.exit_at >= ?`, day, buckets, cutoff).Scan(&dropped); err != nil {
			return purged, fmt.Errorf("featuremodel: count purged rows of %s: %w", day, err)
		}
		purged += dropped
	}
	return purged, nil
}

func (r *Repository) scanDay(ctx context.Context, day string, buckets int, cutoff string, fn func(TrainRow) error) error {
	rows, err := r.db.QueryContext(ctx, `SELECT l.snapshot_id, l.long_net_bps, l.short_net_bps, `+featureSelect+`
FROM research_horizon_labels l JOIN market_snapshots m ON m.id = l.snapshot_id
WHERE l.day = ? AND l.horizon = '30m' AND l.sample_bucket < ? AND l.exit_at < ?
ORDER BY l.snapshot_id`, day, buckets, cutoff)
	if err != nil {
		return fmt.Errorf("featuremodel: read training rows of %s: %w", day, err)
	}
	defer func() { _ = rows.Close() }()
	n := 0
	raw := make([]sql.NullFloat64, len(Features))
	for rows.Next() {
		row := TrainRow{Day: day}
		var long, short sql.NullFloat64
		if err := rows.Scan(append([]any{&row.SnapshotID, &long, &short}, featureDests(raw)...)...); err != nil {
			return fmt.Errorf("featuremodel: scan training row of %s: %w", day, err)
		}
		row.HasLong, row.LongNet = long.Valid, long.Float64
		row.HasShort, row.ShortNet = short.Valid, short.Float64
		row.X = scanFeatures(raw)
		if err := fn(row); err != nil {
			return err
		}
		if n++; n%4096 == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
	}
	return rows.Err()
}

// PendingDays returns, oldest first, the JST days from `from` to `to` inclusive on
// which the trader made a LONG/SHORT decision and for which no run exists yet
// (version(day) names the run). At most one probe per day of the range.
func (r *Repository) PendingDays(ctx context.Context, from, to string, version func(day string) string) ([]string, error) {
	first, err := time.ParseInLocation(time.DateOnly, from, jst)
	if err != nil {
		return nil, fmt.Errorf("featuremodel: day %q: %w", from, err)
	}
	var days []string
	for d := first; d.Format(time.DateOnly) <= to; d = d.AddDate(0, 0, 1) {
		day := d.Format(time.DateOnly)
		var one int
		err := r.db.QueryRowContext(ctx, `SELECT 1 FROM research_model_runs WHERE version = ?`, version(day)).Scan(&one)
		if err == nil {
			continue
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("featuremodel: probe run of %s: %w", day, err)
		}
		ok, err := r.hasDecisions(ctx, d)
		if err != nil {
			return nil, err
		}
		if ok {
			days = append(days, day)
		}
	}
	return days, nil
}

func (r *Repository) hasDecisions(ctx context.Context, dayStart time.Time) (bool, error) {
	var one int
	err := r.db.QueryRowContext(ctx, `SELECT 1 FROM jev_decisions WHERE decision_type = 'trader' AND direction IN ('LONG', 'SHORT')
AND timestamp >= ? AND timestamp < ? LIMIT 1`, sqlutil.FormatTime(dayStart), sqlutil.FormatTime(dayStart.AddDate(0, 0, 1))).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("featuremodel: probe decisions: %w", err)
	}
	return true, nil
}

// Decisions returns the trader LONG/SHORT decisions of the JST day whose own bar
// exists in market_snapshots (same instrument and timestamp, the join the Jev
// evaluation uses), with that bar's features, ordered by decision id. A decision
// without a bar is not returned: it gets no prediction.
func (r *Repository) Decisions(ctx context.Context, day string) ([]Decision, error) {
	from, to, err := DayBounds(day)
	if err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT d.id, d.instrument_id, d.timestamp, `+featureSelect+`
FROM jev_decisions d JOIN market_snapshots m ON m.instrument_id = d.instrument_id AND m.timestamp = d.timestamp
WHERE d.decision_type = 'trader' AND d.direction IN ('LONG', 'SHORT') AND d.timestamp >= ? AND d.timestamp < ?
ORDER BY d.id`, sqlutil.FormatTime(from), sqlutil.FormatTime(to))
	if err != nil {
		return nil, fmt.Errorf("featuremodel: read decisions of %s: %w", day, err)
	}
	defer func() { _ = rows.Close() }()
	raw := make([]sql.NullFloat64, len(Features))
	var out []Decision
	for rows.Next() {
		var (
			d  Decision
			at string
		)
		if err := rows.Scan(append([]any{&d.ID, &d.InstrumentID, &at}, featureDests(raw)...)...); err != nil {
			return nil, fmt.Errorf("featuremodel: scan decision of %s: %w", day, err)
		}
		if d.At, err = sqlutil.ParseTime(at); err != nil {
			return nil, err
		}
		d.X = scanFeatures(raw)
		out = append(out, d)
	}
	return out, rows.Err()
}
