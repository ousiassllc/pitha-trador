package jevreplay

import (
	"context"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain/jevreplay"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

// SavedDecision is the saved input of one live trader decision: exactly what
// the live Jev was sent (jev_decisions.state_json) and when it was decided.
type SavedDecision struct {
	ID        int64
	Timestamp time.Time
	// StateJSON is state_json as stored, byte for byte.
	StateJSON string
}

// DecisionQuery selects the live decisions a run replays.
type DecisionQuery struct {
	// From and To bound the decision's timestamp, [From, To).
	From, To time.Time
	// NotBefore is the earliest timestamp any chosen arm may be asked about
	// (the earliest training-cutoff day over the arms; zero: no limit).
	NotBefore time.Time
	// MaxID, when above 0, excludes decisions saved after the estimate.
	MaxID int64
	// Limit is the most decisions returned (oldest id first).
	Limit int
}

// SavedDecisions reads the live trader decisions of q, oldest id first. It
// returns the stored state as it is: a replay adds nothing to it. Rows with an
// empty state are skipped (nothing to send).
func SavedDecisions(ctx context.Context, db Querier, q DecisionQuery) ([]SavedDecision, error) {
	from := q.From
	if q.NotBefore.After(from) {
		from = q.NotBefore
	}
	rows, err := db.QueryContext(ctx, `
SELECT id, timestamp, state_json FROM jev_decisions
WHERE decision_type = ? AND timestamp >= ? AND timestamp < ? AND (? = 0 OR id <= ?) AND state_json <> ''
ORDER BY id LIMIT ?`,
		jevreplay.DecisionTypeTrader, sqlutil.FormatTime(from), sqlutil.FormatTime(q.To), q.MaxID, q.MaxID, q.Limit)
	if err != nil {
		return nil, fmt.Errorf("repository: read saved jev decisions for replay: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []SavedDecision
	for rows.Next() {
		var (
			d  SavedDecision
			ts string
		)
		if err := rows.Scan(&d.ID, &ts, &d.StateJSON); err != nil {
			return nil, fmt.Errorf("repository: scan saved jev decision: %w", err)
		}
		if d.Timestamp, err = sqlutil.ParseTime(ts); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repository: read saved jev decisions for replay: %w", err)
	}
	return out, nil
}
