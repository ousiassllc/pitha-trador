package indexdiag

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	rindex "github.com/ousiassllc/pitha-trador/internal/domain/researchindex"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// DefaultStaleness is how old (measured from the minute's start) a value may be
// and still be the match of a decision time: a decision with no value at or before
// it within this bound is unmatched. Outside the session, across the lunch break
// and over a gap in the record there is no value, and that is "unmatched", never
// the last value carried over.
const DefaultStaleness = 5 * time.Minute

// Quote is one recorded minute.
type Quote struct {
	OpenTime time.Time
	// Price is the value the 立花 recording took at the 5th second of the minute.
	Price float64
}

// The latest minute that is known at time t: the value of the minute starting at
// OpenTime was taken rindex.SampleLag after it, so only minutes with
// open_time <= t - SampleLag are visible. A decision never sees a value from after
// its own time.
const atOrBeforeSQL = `SELECT minute_time, price FROM research_index_minutes
WHERE symbol = ? AND minute_time <= ? AND minute_time >= ? ORDER BY minute_time DESC LIMIT 1`

// AtOrBefore returns, for every time in times (in order, any order, repeats
// allowed), the latest recorded value of symbol that was known at it and whose
// minute is not older than staleness (nil, "unmatched", otherwise; a non-positive
// staleness is DefaultStaleness). It only reads (SELECT through q); it never feeds
// a trading decision.
func AtOrBefore(ctx context.Context, q query.Q, symbol string, times []time.Time, staleness time.Duration) ([]*Quote, error) {
	if staleness <= 0 {
		staleness = DefaultStaleness
	}
	out := make([]*Quote, len(times))
	bySecond := map[time.Time]*Quote{} // decisions of the same second share the answer
	for i, t := range times {
		t = t.UTC()
		key := t.Truncate(time.Second)
		if cached, ok := bySecond[key]; ok {
			out[i] = cached
			continue
		}
		known := t.Add(-rindex.SampleLag)
		var (
			open  string
			price float64
		)
		err := q.QueryRowContext(ctx, atOrBeforeSQL, symbol, sqlutil.FormatTime(known), sqlutil.FormatTime(known.Add(-staleness))).Scan(&open, &price)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			bySecond[key] = nil
			continue
		case err != nil:
			return nil, fmt.Errorf("indexdiag: value of %s at or before %s: %w", symbol, sqlutil.FormatTime(t), err)
		}
		ot, err := sqlutil.ParseTime(open)
		if err != nil {
			return nil, err
		}
		qt := &Quote{OpenTime: ot, Price: price}
		bySecond[key], out[i] = qt, qt
	}
	return out, nil
}
