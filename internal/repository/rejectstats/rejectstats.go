package rejectstats

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

// Unknown is the head reported for a NONE signal that carries no reject_reason.
const Unknown = "unknown"

// Querier is satisfied by *sql.DB and *sql.Tx.
type Querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// Count is how many NONE signals carry one reject_reason head.
type Count struct {
	// Reason is the text of reject_reason before its first ':' (trimmed): the
	// Policy Engine's fixed codes such as entry_quality_below_threshold,
	// risk_engine_rejected or meta_skip. The detail after ':' (figures, error
	// text) never leaves the database.
	Reason string
	Count  int
}

// TodayCounts counts the NONE trade_signals decided in [from, to) by
// reject_reason head, most frequent first (ties by reason). A missing or empty
// reason is Unknown.
func TodayCounts(ctx context.Context, q Querier, from, to time.Time) ([]Count, error) {
	rows, err := q.QueryContext(ctx, `
SELECT CASE WHEN head = '' THEN ? ELSE head END, COUNT(*)
FROM (
	SELECT trim(CASE WHEN instr(reason, ':') > 0 THEN substr(reason, 1, instr(reason, ':') - 1) ELSE reason END,
			' '||char(9)||char(10)||char(13)) AS head
	FROM (
		SELECT COALESCE(reject_reason, '') AS reason
		FROM trade_signals
		WHERE direction = 'NONE' AND timestamp >= ? AND timestamp < ?
	)
)
GROUP BY 1
ORDER BY COUNT(*) DESC, 1 ASC`, Unknown, sqlutil.FormatTime(from), sqlutil.FormatTime(to))
	if err != nil {
		return nil, fmt.Errorf("rejectstats: count NONE signals by reason: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := []Count{}
	for rows.Next() {
		var c Count
		if err := rows.Scan(&c.Reason, &c.Count); err != nil {
			return nil, fmt.Errorf("rejectstats: scan reason count: %w", err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rejectstats: count NONE signals by reason: %w", err)
	}
	return out, nil
}
