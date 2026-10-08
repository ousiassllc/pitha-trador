package rankingwatch

import (
	"context"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/safego"
)

// rankingResult is what one cycle learned from the candidate source.
type rankingResult struct {
	// symbols are the candidates that are in the universe, best first.
	// Empty when the ranking is empty or failed.
	symbols []string
	// rows is how many candidates the source returned before the universe filter.
	rows int
	// err is the request failure (HTTP/broker error, session, rate limit,
	// decode error, panic); when set, symbols is empty: a half-fetched
	// ranking is never used (the adapter fails the whole fetch on its first
	// error).
	err error
	// offSession is true when no ranking was requested (outside the session).
	offSession bool
}

// fetchRanking asks the candidate source once. A failure ends the fetch and
// the result is zero candidates: the cycle publishes only the held symbols
// instead of a stale or partial ranking.
func (w *Watcher) fetchRanking(ctx context.Context, universe map[string]domain.Instrument) rankingResult {
	var candidates []string
	var err error
	panicked := safego.Run("ranking watch fetch", func() {
		candidates, err = w.Source.Candidates(ctx)
	})
	if panicked {
		err = errPanicked
	}
	if err != nil {
		return rankingResult{err: err}
	}
	res := rankingResult{rows: len(candidates)}
	for _, sym := range candidates {
		if _, ok := universe[sym]; ok {
			res.symbols = append(res.symbols, sym)
		}
	}
	return res
}
