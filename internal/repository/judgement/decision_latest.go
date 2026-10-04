package judgement

import (
	"context"
	"fmt"
	"strings"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// LatestTraderByInstruments returns each instrumentID's most recent Jev
// Trader jev_decisions row (newest timestamp, ties broken by id), keyed by
// instrument ID, in a single query. It is THE definition of "the latest
// Jev Trader decision" shared by every consumer - the Scanner
// Dashboard/API/WebSocket candidates, Symbol Detail (SSR page,
// GET /api/v1/symbols/{symbol}, /ws/symbols/{symbol}), execution.Engine.State
// and Paper Trading's exit evaluation (issues #496/#497/#499): the newest
// decision_type='trader' row regardless of how many Scout rows were written
// since or how old it is - there is deliberately no row-count window and no
// upper age limit (functional.md §5.1). Instruments with no Trader decision
// are absent from the map; an empty instrumentIDs returns an empty map
// without querying.
//
// The query is instrument-driven: the outer side is the requested instrument
// set and each one seeks jev_decisions_instrument_type_timestamp_idx
// (instrument_id, decision_type, timestamp DESC, id DESC) for its newest
// row, so the cost is independent of the table's Trader row count (issue
// #498; decision_plan_test.go pins the plan).
func (r *DecisionRepository) LatestTraderByInstruments(ctx context.Context, instrumentIDs []int64) (map[int64]domain.JevDecision, error) {
	return r.latestByInstruments(ctx, domain.JevDecisionTypeTrader, instrumentIDs)
}

// LatestTrader returns instrumentID's most recent Jev Trader decision (see
// LatestTraderByInstruments for the definition), or ErrDecisionNotFound
// when Jev Trader has not evaluated it yet.
func (r *DecisionRepository) LatestTrader(ctx context.Context, instrumentID int64) (domain.JevDecision, error) {
	return r.latestOne(ctx, domain.JevDecisionTypeTrader, instrumentID)
}

// LatestScout returns instrumentID's most recent Jev Scout decision, or
// ErrDecisionNotFound when none exists. Same index, same ordering and no
// window as LatestTrader.
func (r *DecisionRepository) LatestScout(ctx context.Context, instrumentID int64) (domain.JevDecision, error) {
	return r.latestOne(ctx, domain.JevDecisionTypeScout, instrumentID)
}

func (r *DecisionRepository) latestOne(ctx context.Context, decisionType string, instrumentID int64) (domain.JevDecision, error) {
	byInstrument, err := r.latestByInstruments(ctx, decisionType, []int64{instrumentID})
	if err != nil {
		return domain.JevDecision{}, err
	}
	d, ok := byInstrument[instrumentID]
	if !ok {
		return domain.JevDecision{}, ErrDecisionNotFound
	}
	return d, nil
}

// latestByInstrumentsQuery is latestByInstruments' statement with %s as the
// "(?),(?)..." VALUES list of instrument IDs; the final bound parameter is
// decision_type.
const latestByInstrumentsQuery = `WITH ids(iid) AS (VALUES %s)` + decisionSelectColumns + `
FROM ids JOIN jev_decisions ON jev_decisions.id = (
	SELECT latest.id FROM jev_decisions AS latest
	WHERE latest.instrument_id = ids.iid AND latest.decision_type = ?
	ORDER BY latest.timestamp DESC, latest.id DESC LIMIT 1)`

func (r *DecisionRepository) latestByInstruments(ctx context.Context, decisionType string, instrumentIDs []int64) (map[int64]domain.JevDecision, error) {
	out := make(map[int64]domain.JevDecision, len(instrumentIDs))
	if len(instrumentIDs) == 0 {
		return out, nil
	}

	args := make([]any, 0, len(instrumentIDs)+1)
	for _, id := range instrumentIDs {
		args = append(args, id)
	}
	args = append(args, decisionType)
	values := strings.TrimSuffix(strings.Repeat("(?),", len(instrumentIDs)), ",")

	rows, err := r.db.QueryContext(ctx, fmt.Sprintf(latestByInstrumentsQuery, values), args...) //nolint:gosec // G201: values is only "(?)" markers; ids are bound parameters
	if err != nil {
		return nil, fmt.Errorf("repository: latest %s jev decisions for %d instruments: %w", decisionType, len(instrumentIDs), err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		d, err := scanDecision(rows)
		if err != nil {
			return nil, err
		}
		out[d.InstrumentID] = d
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repository: iterate latest %s jev decisions: %w", decisionType, err)
	}
	return out, nil
}
