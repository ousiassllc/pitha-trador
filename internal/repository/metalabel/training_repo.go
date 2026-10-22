package metalabel

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

// TrainingRow is one labeled Jev trader decision the secondary model learns
// from: what the decision looked like when it was made (the stored order-flow
// features, state and Jev answer) and its triple-barrier outcome.
type TrainingRow struct {
	JevDecisionID int64
	Symbol        string
	// JevModelID is the Jev model that answered (jev_decisions.model_id), the
	// model the LLM cutoff has to describe (issue #776).
	JevModelID string
	// DecidedAt is the decision's timestamp (jev_decisions.timestamp).
	DecidedAt time.Time
	// Direction is the primary signal's side, LONG or SHORT.
	Direction string
	// OrderFlow are the order-flow features the decision was made with (nil
	// fields for a decision before they were recorded).
	OrderFlow domain.OrderFlowFeatures
	// StateJSON is the Jev input (jev_decisions.state_json: spread_bps,
	// return_1m and the news_context of FR-LUNA-3).
	StateJSON string
	// ResponseJSON is Jev's raw answer (jev_decisions.response_json), the
	// source of the Jev score.
	ResponseJSON string
	// HorizonMinutes is the label window: the outcome is known at DecidedAt
	// plus this.
	HorizonMinutes int
	// NetReturnBps is the cost-adjusted return of the triple-barrier label.
	NetReturnBps float64
}

// LabelEnd is the instant the row's label is fully determined: the end of the
// window the label looked at. A row can only be trained on once now has passed
// it, and a model evaluated from instant t must not train on a row whose
// LabelEnd is later than t (purging).
func (r TrainingRow) LabelEnd() time.Time {
	return r.DecidedAt.Add(time.Duration(r.HorizonMinutes) * time.Minute)
}

// Querier is the read handle ListTrainingRowsFrom needs: a *sql.DB, a *sql.Tx
// or the Paper results MCP's read-only query handle.
type Querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// ListTrainingRows returns the triple-barrier-labeled (label_version 2,
// net_return_bps set) LONG/SHORT trader decisions made in [from, to), oldest
// first. A decision with several label rows (the timeout setting changed
// between labelings) yields only its newest one.
func (r *Repository) ListTrainingRows(ctx context.Context, from, to time.Time) ([]TrainingRow, error) {
	return ListTrainingRowsFrom(ctx, r.db, from, to)
}

// ListTrainingRowsFrom is ListTrainingRows over any read handle, so a
// read-only reader (the Paper results MCP) selects exactly the rows the
// nightly retraining trains on.
func ListTrainingRowsFrom(ctx context.Context, q Querier, from, to time.Time) ([]TrainingRow, error) {
	rows, err := q.QueryContext(ctx, `
SELECT d.id, d.symbol, d.model_id, d.timestamp, d.direction, d.state_json, d.response_json,
	d.ofi_short, d.ofi_mid, d.ofi_long, d.microprice_dev_bps,
	o.horizon_minutes, o.net_return_bps
FROM calibration_outcomes o
JOIN jev_decisions d ON d.id = o.jev_decision_id
WHERE d.decision_type = 'trader' AND d.direction IN ('LONG', 'SHORT')
	AND o.label_version = ? AND o.net_return_bps IS NOT NULL
	AND d.timestamp >= ? AND d.timestamp < ?
	AND o.id = (SELECT MAX(o2.id) FROM calibration_outcomes o2
		WHERE o2.jev_decision_id = d.id AND o2.label_version = ? AND o2.net_return_bps IS NOT NULL)
ORDER BY d.timestamp, d.id`,
		domain.CalibrationLabelVersionTripleBarrier, sqlutil.FormatTime(from), sqlutil.FormatTime(to), domain.CalibrationLabelVersionTripleBarrier)
	if err != nil {
		return nil, fmt.Errorf("repository: list meta-label training rows in [%s, %s): %w", from, to, err)
	}
	defer func() { _ = rows.Close() }()

	var out []TrainingRow
	for rows.Next() {
		var (
			tr        TrainingRow
			timestamp string
			net       sql.NullFloat64
		)
		if err := rows.Scan(&tr.JevDecisionID, &tr.Symbol, &tr.JevModelID, &timestamp, &tr.Direction, &tr.StateJSON, &tr.ResponseJSON,
			sqlutil.NullFloat(&tr.OrderFlow.OFIShort), sqlutil.NullFloat(&tr.OrderFlow.OFIMid), sqlutil.NullFloat(&tr.OrderFlow.OFILong),
			sqlutil.NullFloat(&tr.OrderFlow.MicropriceDevBps), &tr.HorizonMinutes, &net); err != nil {
			return nil, fmt.Errorf("repository: scan meta-label training row: %w", err)
		}
		if tr.DecidedAt, err = sqlutil.ParseTime(timestamp); err != nil {
			return nil, err
		}
		tr.NetReturnBps = net.Float64
		out = append(out, tr)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repository: list meta-label training rows: %w", err)
	}
	return out, nil
}

// LabelVersionCounts is how many labeled decisions exist per label_version:
// V1 the close-only label (no order-flow features, never a training row) and V2
// the triple-barrier label the model learns from.
type LabelVersionCounts struct{ V1, V2 int }

// CountLabeledDecisions counts the LONG/SHORT trader decisions made in
// [from, to) that have a label, per label_version (a decision counts once per
// version). A version 1 label has no net return by design (migration 000033), so
// only version 2 labels are required to have one: the V2 count is the labeled
// decisions ListTrainingRows reads before BuildRows drops the incomplete ones.
// It is what a skipped retraining reports next to the rows it could use (issue
// #862).
func (r *Repository) CountLabeledDecisions(ctx context.Context, from, to time.Time) (LabelVersionCounts, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT o.label_version, COUNT(DISTINCT o.jev_decision_id)
FROM calibration_outcomes o
JOIN jev_decisions d ON d.id = o.jev_decision_id
WHERE d.decision_type = 'trader' AND d.direction IN ('LONG', 'SHORT')
	AND (o.label_version = ? OR o.net_return_bps IS NOT NULL) AND d.timestamp >= ? AND d.timestamp < ?
GROUP BY o.label_version`, domain.CalibrationLabelVersionClose, sqlutil.FormatTime(from), sqlutil.FormatTime(to))
	if err != nil {
		return LabelVersionCounts{}, fmt.Errorf("repository: count labeled decisions in [%s, %s): %w", from, to, err)
	}
	defer func() { _ = rows.Close() }()

	var out LabelVersionCounts
	for rows.Next() {
		var version, n int
		if err := rows.Scan(&version, &n); err != nil {
			return LabelVersionCounts{}, fmt.Errorf("repository: scan labeled decision count: %w", err)
		}
		switch version {
		case domain.CalibrationLabelVersionClose:
			out.V1 = n
		case domain.CalibrationLabelVersionTripleBarrier:
			out.V2 = n
		}
	}
	if err := rows.Err(); err != nil {
		return LabelVersionCounts{}, fmt.Errorf("repository: count labeled decisions: %w", err)
	}
	return out, nil
}
