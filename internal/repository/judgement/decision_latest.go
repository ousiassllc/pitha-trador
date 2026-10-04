package judgement

import (
	"context"
	"fmt"
	"strings"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// LatestTraderByInstruments returns each instrumentID's most recent Jev
// Trader jev_decisions row (newest timestamp, ties broken by id), keyed by
// instrument ID, in a single query (jev_decisions_instrument_timestamp_idx
// serves the per-instrument newest-row subquery). Instruments with no
// Trader decision are absent from the map; an empty instrumentIDs returns
// an empty map without querying. It lets the Scanner Dashboard's candidate
// refresh cycle (15-30s) read every candidate's latest decision without a
// query per candidate.
func (r *DecisionRepository) LatestTraderByInstruments(ctx context.Context, instrumentIDs []int64) (map[int64]domain.JevDecision, error) {
	out := make(map[int64]domain.JevDecision, len(instrumentIDs))
	if len(instrumentIDs) == 0 {
		return out, nil
	}

	args := make([]any, 0, len(instrumentIDs)+2)
	args = append(args, domain.JevDecisionTypeTrader)
	for _, id := range instrumentIDs {
		args = append(args, id)
	}
	args = append(args, domain.JevDecisionTypeTrader)
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(instrumentIDs)), ",")

	rows, err := r.db.QueryContext(ctx,
		decisionSelectColumns+` FROM jev_decisions
WHERE decision_type = ? AND instrument_id IN (`+placeholders+`) AND id = (
	SELECT latest.id FROM jev_decisions AS latest
	WHERE latest.instrument_id = jev_decisions.instrument_id AND latest.decision_type = ?
	ORDER BY latest.timestamp DESC, latest.id DESC LIMIT 1)`, //nolint:gosec // G202: placeholders is only "?" markers; ids are bound parameters
		args...)
	if err != nil {
		return nil, fmt.Errorf("repository: latest trader jev decisions for %d instruments: %w", len(instrumentIDs), err)
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
		return nil, fmt.Errorf("repository: iterate latest trader jev decisions: %w", err)
	}
	return out, nil
}
