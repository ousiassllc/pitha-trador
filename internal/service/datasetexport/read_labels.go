package datasetexport

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// labelCounts is what attachLabels found beyond the columns.
type labelCounts struct {
	// CalibrationOtherHorizons are the stored calibration labels (label_version 2) of the period at a
	// horizon that has no column.
	CalibrationOtherHorizons int
}

const numOK = `typeof(%s) IN ('integer', 'real')`

func numGuard(col string) string { return fmt.Sprintf(numOK, col) }

func barrierExpr(col string) string {
	quoted := make([]string, len(barrierHits))
	for i, v := range barrierHits {
		quoted[i] = "'" + v + "'"
	}
	return "CASE WHEN " + col + " IN (" + strings.Join(quoted, ", ") + ") THEN " + col + " END"
}

// extendWindow moves the row's label-window end to end when it is later.
func (r *row) extendWindow(end time.Time) {
	if end.After(r.windowEnd) {
		r.windowEnd = end
	}
}

// closeAt is the TSE close (15:30 JST) of the decision's JST day: where a "close" label ends.
func (r *row) closeAt() time.Time {
	y, m, d := r.at.In(query.JST).Date()
	return time.Date(y, m, d, closeHour, closeMinute, 0, 0, query.JST)
}

// attachLabels fills the label columns of rows from the three label stores: the stored
// calibration label, the research horizon label and the counterfactual label of a skipped
// candidate. Each is one batched read of the period (never one read per row) through the
// store's decision index. It also extends every row's windowEnd by the horizon of each label it got.
func attachLabels(ctx context.Context, q query.Q, from, to string, byID map[int64]*row) (labelCounts, error) {
	var counts labelCounts
	if err := attachCalibration(ctx, q, from, to, byID, &counts); err != nil {
		return counts, err
	}
	if err := attachResearch(ctx, q, from, to, byID); err != nil {
		return counts, err
	}
	return counts, attachCounterfactual(ctx, q, from, to, byID)
}

func horizonList() string {
	parts := make([]string, len(CalibrationHorizons))
	for i, h := range CalibrationHorizons {
		parts[i] = strconv.Itoa(h)
	}
	return strings.Join(parts, ", ")
}

func attachCalibration(ctx context.Context, q query.Q, from, to string, byID map[int64]*row, counts *labelCounts) error {
	rows, err := q.QueryContext(ctx, `
SELECT o.jev_decision_id, o.horizon_minutes, o.net_return_bps, `+barrierExpr("o.barrier_hit")+`
FROM jev_decisions d
JOIN calibration_outcomes o ON o.jev_decision_id = d.id
WHERE`+scope+` AND o.label_version = ? AND o.horizon_minutes IN (`+horizonList()+`) AND `+numGuard("o.net_return_bps"),
		from, to, CalibrationLabelVersion)
	if err != nil {
		return fmt.Errorf("datasetexport: read calibration labels: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var (
			id      int64
			horizon int
			net     float64
			barrier sql.NullString
		)
		if err := rows.Scan(&id, &horizon, &net, &barrier); err != nil {
			return fmt.Errorf("datasetexport: scan calibration label: %w", err)
		}
		r, pos := byID[id], slices.Index(CalibrationHorizons, horizon)
		if r == nil || pos < 0 {
			continue
		}
		r.cells[off.calibration+2*pos] = strconv.FormatFloat(net, 'f', -1, 64)
		r.cells[off.calibration+2*pos+1] = barrier.String
		r.extendWindow(r.at.Add(time.Duration(horizon) * time.Minute))
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("datasetexport: read calibration labels: %w", err)
	}
	// Labels at a horizon without a column are not exported; say how many.
	err = q.QueryRowContext(ctx, `
SELECT COUNT(*) FROM jev_decisions d
JOIN calibration_outcomes o ON o.jev_decision_id = d.id
WHERE`+scope+` AND o.label_version = ? AND o.horizon_minutes NOT IN (`+horizonList()+`)`,
		from, to, CalibrationLabelVersion).Scan(&counts.CalibrationOtherHorizons)
	if err != nil {
		return fmt.Errorf("datasetexport: count calibration labels at other horizons: %w", err)
	}
	return nil
}

func attachResearch(ctx context.Context, q query.Q, from, to string, byID map[int64]*row) error {
	// The research label of a decision is the one of its own bar: (instrument_id, snapshot_at = timestamp).
	rows, err := q.QueryContext(ctx, `
SELECT d.id, r.horizon,
	CASE WHEN `+numGuard("r.long_net_bps")+` THEN r.long_net_bps END,
	CASE WHEN `+numGuard("r.short_net_bps")+` THEN r.short_net_bps END
FROM jev_decisions d
JOIN research_horizon_labels r ON r.instrument_id = d.instrument_id AND r.snapshot_at = d.timestamp
WHERE`+scope+` AND r.label_version = ?`, from, to, ResearchLabelVersion)
	if err != nil {
		return fmt.Errorf("datasetexport: read research labels: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var (
			id          int64
			horizon     string
			long, short sql.NullFloat64
		)
		if err := rows.Scan(&id, &horizon, &long, &short); err != nil {
			return fmt.Errorf("datasetexport: scan research label: %w", err)
		}
		r, pos := byID[id], slices.Index(ResearchHorizons, horizon)
		if r == nil || pos < 0 {
			continue
		}
		r.cells[off.research+2*pos] = cellOf(&long)
		r.cells[off.research+2*pos+1] = cellOf(&short)
		switch horizon {
		case "30m":
			r.extendWindow(r.at.Add(30 * time.Minute))
		case "60m":
			r.extendWindow(r.at.Add(60 * time.Minute))
		default:
			r.extendWindow(r.closeAt())
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("datasetexport: read research labels: %w", err)
	}
	return nil
}

// cfLabel is the newest counterfactual label of a decision.
type cfLabel struct {
	id      int64
	horizon int
	net     float64
	barrier sql.NullString
	kind    string
	unver   bool
}

func attachCounterfactual(ctx context.Context, q query.Q, from, to string, byID map[int64]*row) error {
	rows, err := q.QueryContext(ctx, `
SELECT c.jev_decision_id, c.id, c.horizon_minutes, c.net_return_bps, `+barrierExpr("c.barrier_hit")+`,
	CASE WHEN c.skip_kind IN ('risk_rejected', 'guard_skipped', 'no_signal') THEN c.skip_kind ELSE '' END, c.cutoff_unverified = 1
FROM jev_decisions d
JOIN counterfactual_labels c ON c.jev_decision_id = d.id
WHERE`+scope+` AND c.label_version = ? AND `+numGuard("c.net_return_bps"), from, to, CounterfactualVersion)
	if err != nil {
		return fmt.Errorf("datasetexport: read counterfactual labels: %w", err)
	}
	defer func() { _ = rows.Close() }()
	newest := map[int64]cfLabel{}
	for rows.Next() {
		var (
			id int64
			c  cfLabel
		)
		if err := rows.Scan(&id, &c.id, &c.horizon, &c.net, &c.barrier, &c.kind, &c.unver); err != nil {
			return fmt.Errorf("datasetexport: scan counterfactual label: %w", err)
		}
		if cur, ok := newest[id]; !ok || c.id > cur.id { // the latest row of a decision, as the retraining reads it
			newest[id] = c
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("datasetexport: read counterfactual labels: %w", err)
	}
	for id, c := range newest {
		r := byID[id]
		if r == nil {
			continue
		}
		p := off.counterfactual
		r.cells[p+cfHorizon] = strconv.Itoa(c.horizon)
		r.cells[p+cfNetBps] = strconv.FormatFloat(c.net, 'f', -1, 64)
		r.cells[p+cfBarrier] = c.barrier.String
		r.cells[p+cfSkipKind] = c.kind
		r.cells[p+cfCutoffUnverified] = boolCell(c.unver)
		r.extendWindow(r.at.Add(time.Duration(c.horizon) * time.Minute))
	}
	return nil
}
