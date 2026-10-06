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

// selfRows names the rows of one table that are the subject's own (or too
// close to it) and must not come back as similar past cases: the rows of
// table matching cond (bound with args). The zero value excludes nothing.
//
// The exclusion is applied in Go after the KNN search (searchExcluding), not
// as a vec0 id constraint: sqlite-vec only accepts equality/IN on the id
// column during a KNN scan, so the only SQL form is a positive
// "id IN (SELECT id FROM ... WHERE NOT self)" whose subquery materializes
// nearly every row of the history on each call (issue #528). The excluded
// set is only the subject symbol's last few rows, so over-fetching a small
// margin and dropping them afterwards is cheap.
type selfRows struct {
	table string
	cond  string
	args  []any
}

func (r selfRows) active() bool { return r.table != "" }

// decisionSelf selects the subject symbol's decisions at or after the
// subject timestamp (the current state's own Scout decision when Trader
// runs). Earlier decisions stay: only the supplementary snapshot search
// carries the recency guard.
func (sub Subject) decisionSelf() selfRows {
	if sub.Symbol == "" {
		return selfRows{}
	}
	return selfRows{
		table: "jev_decisions",
		cond:  "symbol = ? AND timestamp >= ?",
		args:  []any{sub.Symbol, sqlutil.FormatTime(sub.Timestamp)},
	}
}

// snapshotSelf selects the subject symbol's snapshots newer than
// Timestamp - SnapshotRecencyGuard.
func (sub Subject) snapshotSelf() selfRows {
	if sub.Symbol == "" {
		return selfRows{}
	}
	return selfRows{
		table: "market_snapshots",
		cond:  "symbol = ? AND timestamp > ?",
		args:  []any{sub.Symbol, sqlutil.FormatTime(sub.Timestamp.Add(-SnapshotRecencyGuard))},
	}
}
