package researchdiag

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	rlabel "github.com/ousiassllc/pitha-trador/internal/domain/researchlabel"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

var sides = []string{domain.JevDirectionLong, domain.JevDirectionShort}

// sampleBuckets is the number of research_horizon_labels.sample_bucket values
// (migration 000055).
const sampleBuckets = 100

// Analyze computes the summary of [r.From, r.To) and the number of rows the
// statistics are over. Only JST days the recorder completed (research_label_days)
// count: a day it stopped in the middle of is left out until it is redone.
func Analyze(ctx context.Context, q query.Q, r query.Range, req Request, now time.Time) (Data, int, error) {
	if err := req.Validate(); err != nil {
		return Data{}, 0, err
	}
	data := Data{
		Scope: req.scope(), LabelVersion: rlabel.LabelVersion, Horizons: []HorizonStats{},
		Definitions: definitions(), Notice: Notice,
	}
	if req.Direction != "" {
		d := req.Direction
		data.Direction = &d
	}
	recorded, err := recordedDays(ctx, q, r)
	if err != nil {
		return Data{}, 0, err
	}
	var a *acc
	if req.scope() == ScopeSnapshots {
		a, err = snapshotsAcc(ctx, q, r, recorded)
	} else {
		a, err = decisionsAcc(ctx, q, r, req, recorded)
	}
	if err != nil {
		return Data{}, 0, err
	}
	total := 0
	for _, h := range rlabel.Horizons {
		hs := HorizonStats{Horizon: h}
		for _, side := range sides {
			k := key{h, side}
			if a.total(k).n == 0 {
				continue
			}
			stat, cost := sideStats(a, k)
			total += stat.Rows
			if side == domain.JevDirectionLong {
				hs.Long, hs.CostVsSpread.Long = &stat, &cost
			} else {
				hs.Short, hs.CostVsSpread.Short = &stat, &cost
			}
		}
		data.Horizons = append(data.Horizons, hs)
	}
	if data.RecordedThrough, err = recordedThrough(ctx, q); err != nil {
		return Data{}, 0, err
	}
	if data.Coverage, err = coverage(ctx, q, req, r, now); err != nil {
		return Data{}, 0, err
	}
	return data, total, nil
}

// recordedDays is the set of JST days of r the recorder completed.
func recordedDays(ctx context.Context, q query.Q, r query.Range) (map[string]bool, error) {
	fromDay, toDay := dayBounds(r)
	rows, err := q.QueryContext(ctx, `SELECT day FROM research_label_days WHERE day >= ? AND day < ?`, fromDay, toDay)
	if err != nil {
		return nil, fmt.Errorf("researchdiag: list recorded days: %w", err)
	}
	defer func() { _ = rows.Close() }()
	set := map[string]bool{}
	for rows.Next() {
		var day string
		if err := rows.Scan(&day); err != nil {
			return nil, fmt.Errorf("researchdiag: scan recorded day: %w", err)
		}
		set[day] = true
	}
	return set, rows.Err()
}

func dayBounds(r query.Range) (from, to string) {
	return r.From.In(query.JST).Format(time.DateOnly), r.To.In(query.JST).Format(time.DateOnly)
}

// decisionWhere selects the decisions of a decision scope in [from, to).
func (req Request) decisionWhere(from, to string) (string, []any) {
	where := "d.decision_type = ? AND d.direction IN ('LONG', 'SHORT') AND d.timestamp >= ? AND d.timestamp < ?"
	args := []any{req.scope(), from, to}
	if req.Direction != "" {
		where += " AND d.direction = ?"
		args = append(args, req.Direction)
	}
	return where, args
}

// median is the middle of values (the mean of the two middle ones for an even
// count); values is reordered.
func median(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	slices.Sort(values)
	mid := len(values) / 2
	if len(values)%2 == 1 {
		return values[mid]
	}
	return (values[mid-1] + values[mid]) / 2
}

// normalQuantile is the inverse of the standard normal CDF for p in (0, 1).
func normalQuantile(p float64) float64 { return math.Sqrt2 * math.Erfinv(2*p-1) }

// recordedThrough is the latest JST day the recorder completed.
func recordedThrough(ctx context.Context, q query.Q) (*string, error) {
	var day sql.NullString
	if err := q.QueryRowContext(ctx, `SELECT MAX(day) FROM research_label_days`).Scan(&day); err != nil {
		return nil, fmt.Errorf("researchdiag: recorded through: %w", err)
	}
	if !day.Valid {
		return nil, nil
	}
	return &day.String, nil
}
