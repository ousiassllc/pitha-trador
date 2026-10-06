package rankingwatch

import (
	"context"
	"fmt"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/safego"
)

// rankingResult is what one cycle learned from the rankings.
type rankingResult struct {
	// symbols are the ranked symbols that are in the universe, best first
	// (the types interleaved rank by rank). Empty when the ranking is empty or
	// failed.
	symbols []string
	// rows is how many symbols the rankings returned before the universe filter.
	rows int
	// err is the first request failure (HTTP/kabu error, token, rate limit,
	// decode error, panic); when set, symbols is empty: a half-fetched
	// ranking is never used.
	err error
	// offSession is true when no ranking was requested (outside the session).
	offSession bool
}

func (w *Watcher) types() []int {
	if len(w.Types) > 0 {
		return w.Types
	}
	return DefaultTypes
}

func (w *Watcher) exchange() string {
	if w.Exchange != "" {
		return w.Exchange
	}
	return DefaultExchange
}

// fetchRanking requests every ranking type once. The first failure ends the
// fetch and the result is zero candidates: the cycle publishes only the held
// symbols instead of a ranking assembled from some of the types.
func (w *Watcher) fetchRanking(ctx context.Context, universe map[string]domain.Instrument) rankingResult {
	var res rankingResult
	perType := make([][]string, 0, len(w.types()))
	for _, rankType := range w.types() {
		var symbols []string
		var err error
		panicked := safego.Run("ranking watch fetch", func() {
			symbols, err = w.Source.RankingSymbols(ctx, rankType, w.exchange())
		})
		if panicked {
			err = errPanicked
		}
		if err != nil {
			return rankingResult{err: fmt.Errorf("ranking type %d: %w", rankType, err)}
		}
		res.rows += len(symbols)
		perType = append(perType, symbols)
	}
	res.symbols = interleave(perType, universe)
	return res
}

// interleave merges the per-type rankings rank by rank (every type's 1st,
// then every type's 2nd, ...), keeping only symbols in universe, so each
// ranking type is represented near the top of the merged order.
func interleave(perType [][]string, universe map[string]domain.Instrument) []string {
	var out []string
	seen := make(map[string]struct{})
	for rank := 0; ; rank++ {
		more := false
		for _, symbols := range perType {
			if rank >= len(symbols) {
				continue
			}
			more = true
			sym := symbols[rank]
			if _, dup := seen[sym]; dup {
				continue
			}
			seen[sym] = struct{}{}
			if _, ok := universe[sym]; ok {
				out = append(out, sym)
			}
		}
		if !more {
			return out
		}
	}
}
