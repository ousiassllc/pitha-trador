package query

import (
	"context"
	"database/sql"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/origin"
)

// Trade is one paper_trades row: the whitelisted columns of a closed
// position (API spec §3.3). current_price, unrealized_pnl, slippage_bps and
// order limit prices are not selected.
type Trade struct {
	ID          int64    `json:"id"`
	Symbol      string   `json:"symbol"`
	Side        string   `json:"side"`
	Quantity    int64    `json:"quantity"`
	EntryPrice  float64  `json:"entry_price"`
	OpenedAt    string   `json:"opened_at"`
	ExitPrice   *float64 `json:"exit_price"`
	ClosedAt    string   `json:"closed_at"`
	ExitReason  *string  `json:"exit_reason"`
	RealizedPnl float64  `json:"realized_pnl"`
	FeesJPY     *float64 `json:"fees_jpy"`
	// Origin is how the trade was entered (package origin).
	Origin string `json:"origin"`
}

// Trades returns up to limit closed trades in r, newest first (closed_at,
// then id, descending), starting after cursor ("" for the first page). The
// returned cursor is "" when there is no further page.
func Trades(ctx context.Context, q Q, r Range, limit int, cursor string) ([]Trade, string, error) {
	from, to := r.bounds()
	args := []any{from, to}
	cursorClause := ""
	if cursor != "" {
		closedAt, id, err := DecodeCursor(cursor)
		if err != nil {
			return nil, "", err
		}
		cursorClause = " AND (p.closed_at < ? OR (p.closed_at = ? AND p.id < ?))"
		args = append(args, closedAt, closedAt, id)
	}
	args = append(args, limit+1)

	rows, err := q.QueryContext(ctx, `
SELECT p.id, p.symbol, p.side, p.quantity, p.entry_price, p.opened_at,
	x.filled_price, p.closed_at, p.exit_reason, p.realized_pnl, s.policy_version,
	CASE WHEN e.id IS NULL AND x.id IS NULL THEN NULL
		ELSE COALESCE(e.fees, 0) + COALESCE(x.fees, 0) END
FROM positions p
LEFT JOIN paper_orders e ON e.id = p.entry_order_id
LEFT JOIN paper_orders x ON x.id = p.exit_order_id
LEFT JOIN trade_signals s ON s.id = e.trade_signal_id
WHERE p.closed_at IS NOT NULL AND p.realized_pnl IS NOT NULL
	AND p.closed_at >= ? AND p.closed_at < ?`+cursorClause+`
ORDER BY p.closed_at DESC, p.id DESC
LIMIT ?`, args...)
	if err != nil {
		return nil, "", fmt.Errorf("query: trades: %w", err)
	}
	defer func() { _ = rows.Close() }()

	trades := []Trade{}
	rawClosedAt := ""
	for rows.Next() {
		var (
			t        Trade
			openedAt string
			closedAt string
			exitSell sql.NullFloat64
			reason   sql.NullString
			fees     sql.NullFloat64
			version  sql.NullString
		)
		if err := rows.Scan(&t.ID, &t.Symbol, &t.Side, &t.Quantity, &t.EntryPrice, &openedAt,
			&exitSell, &closedAt, &reason, &t.RealizedPnl, &version, &fees); err != nil {
			return nil, "", fmt.Errorf("query: scan trade: %w", err)
		}
		t.OpenedAt = FormatOut(openedAt)
		t.ClosedAt = FormatOut(closedAt)
		t.ExitPrice = floatPtr(exitSell)
		t.FeesJPY = floatPtr(fees)
		t.Origin = origin.Of(version)
		if reason.Valid {
			t.ExitReason = &reason.String
		}
		if len(trades) == limit {
			// The extra row only proves there is a next page; the cursor
			// resumes after the last row we return.
			next := trades[limit-1]
			return trades, EncodeCursor(rawClosedAt, next.ID), rows.Err()
		}
		rawClosedAt = closedAt
		trades = append(trades, t)
	}
	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("query: trades: %w", err)
	}
	return trades, "", nil
}

// EncodeCursor packs the stored sort timestamp (closed_at for paper_trades,
// received_at for order_proposals_list) and id of the last row. The client
// treats it as opaque.
func EncodeCursor(at string, id int64) string {
	return base64.RawURLEncoding.EncodeToString([]byte(at + "|" + strconv.FormatInt(id, 10)))
}

// DecodeCursor is EncodeCursor's inverse; anything else is ErrBadCursor.
func DecodeCursor(cursor string) (at string, id int64, err error) {
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return "", 0, ErrBadCursor
	}
	text, idText, ok := strings.Cut(string(raw), "|")
	if !ok {
		return "", 0, ErrBadCursor
	}
	if _, err := sqlutil.ParseTime(text); err != nil {
		return "", 0, ErrBadCursor
	}
	id, err = strconv.ParseInt(idText, 10, 64)
	if err != nil {
		return "", 0, ErrBadCursor
	}
	return text, id, nil
}
