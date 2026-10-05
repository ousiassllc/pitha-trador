package rag

import (
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

// SnapshotRecencyGuard is how far back from the queried state same-symbol
// market_snapshots are excluded from the supplementary snapshot search
// (FR-RAG-4). 15 minutes is the longest look-back window baked into the
// feature vector (return_15m, realized_vol_15m): a bar inside it shares
// most of its inputs with the current state, so it is the same situation,
// not a similar past one.
const SnapshotRecencyGuard = 15 * time.Minute

// Subject identifies the state a Context query is built for, so the
// search never hands the state back to Jev as a "similar past case"
// (FR-RAG-2/4). The zero Subject (empty Symbol) excludes nothing.
type Subject struct {
	Symbol    string
	Timestamp time.Time
}

// decisionFilter and snapshotFilter return the vec0 id constraint (plus
// its bound args) that drops the subject's own rows. sqlite-vec accepts
// only equality/IN constraints on the id column during a KNN scan, so the
// exclusion is expressed as a positive IN over the allowed ids rather than
// NOT IN or a range on the key.
//
//   - decisions: same-symbol decisions at or after the subject timestamp
//     are excluded (the current state's own Scout decision when Trader
//     runs). Earlier decisions stay: only the supplementary snapshot
//     search carries the recency guard.
//   - snapshots: same-symbol snapshots newer than
//     Timestamp - SnapshotRecencyGuard are excluded.
//
// An empty filter means no restriction.
func (sub Subject) decisionFilter(labeledOnly bool) (string, []any) {
	var filter string
	var args []any
	switch {
	case sub.Symbol == "" && !labeledOnly:
		return "", nil
	case sub.Symbol == "":
		filter = labeledDecisionFilter
	default:
		allowed := `SELECT id FROM jev_decisions WHERE symbol <> ? OR timestamp < ?`
		args = []any{sub.Symbol, sqlutil.FormatTime(sub.Timestamp)}
		filter = `decision_id IN (` + allowed + `)`
		if labeledOnly {
			filter = `decision_id IN (SELECT jev_decision_id FROM calibration_outcomes WHERE jev_decision_id IN (` + allowed + `))`
		}
	}
	return filter, args
}

func (sub Subject) snapshotFilter() (string, []any) {
	if sub.Symbol == "" {
		return "", nil
	}
	return `snapshot_id IN (SELECT id FROM market_snapshots WHERE symbol <> ? OR timestamp <= ?)`,
		[]any{sub.Symbol, sqlutil.FormatTime(sub.Timestamp.Add(-SnapshotRecencyGuard))}
}
