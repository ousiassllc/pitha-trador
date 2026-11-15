package fxdiag

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// DefaultStaleness is how old (in time) a candle may be, measured from its open
// time, and still be the match of a decision time: a decision with no candle at
// or before it within this bound is unmatched. FX trades around the clock, so a
// longer silence means the market was closed or the record has a gap.
const DefaultStaleness = 5 * time.Minute

// Quote is one recorded minute with both sides present.
type Quote struct {
	OpenTime time.Time
	// BidOpen / AskOpen are the sides' opening prices, known from OpenTime on:
	// the join uses them (not the close, which is known only a minute later), so
	// a decision never sees a price from after its own time.
	BidOpen, AskOpen   float64
	BidClose, AskClose float64
}

// MidOpen is the mid price at the candle's open.
func (q Quote) MidOpen() float64 { return (q.BidOpen + q.AskOpen) / 2 }

const atOrBeforeSQL = `SELECT open_time, bid_open, bid_close, ask_open, ask_close FROM research_fx_candles
WHERE symbol = ? AND open_time <= ? AND open_time >= ? AND bid_open IS NOT NULL AND ask_open IS NOT NULL
ORDER BY open_time DESC LIMIT 1`

// AtOrBefore returns, for every time in times (in order, any order, repeats
// allowed), the latest recorded candle of symbol whose open time is at or before
// it and not older than staleness (the result is nil, "unmatched", otherwise; a
// non-positive staleness is DefaultStaleness). Only minutes with both the BID and
// the ASK candle count: a one-sided minute is skipped, so the match may be an
// earlier minute within the bound. It only reads (SELECT through q); it never
// feeds a trading decision.
func AtOrBefore(ctx context.Context, q query.Q, symbol string, times []time.Time, staleness time.Duration) ([]*Quote, error) {
	if staleness <= 0 {
		staleness = DefaultStaleness
	}
	out := make([]*Quote, len(times))
	byMinute := map[time.Time]*Quote{} // decisions of one minute share the answer
	for i, t := range times {
		t = t.UTC()
		key := t.Truncate(time.Second)
		if cached, ok := byMinute[key]; ok {
			out[i] = cached
			continue
		}
		var (
			open                                 string
			bidOpen, bidClose, askOpen, askClose float64
		)
		err := q.QueryRowContext(ctx, atOrBeforeSQL, symbol, sqlutil.FormatTime(t), sqlutil.FormatTime(t.Add(-staleness))).
			Scan(&open, &bidOpen, &bidClose, &askOpen, &askClose)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			byMinute[key] = nil
			continue
		case err != nil:
			return nil, fmt.Errorf("fxdiag: candle of %s at or before %s: %w", symbol, sqlutil.FormatTime(t), err)
		}
		ot, err := sqlutil.ParseTime(open)
		if err != nil {
			return nil, err
		}
		qt := &Quote{OpenTime: ot, BidOpen: bidOpen, BidClose: bidClose, AskOpen: askOpen, AskClose: askClose}
		byMinute[key], out[i] = qt, qt
	}
	return out, nil
}
