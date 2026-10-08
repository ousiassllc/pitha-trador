package kabu

import (
	"context"
	"fmt"
)

// DefaultRankingTypes are the GET /ranking 種別 fetched: 1 値上がり率, 2 値下がり率,
// 3 売買高上位, 4 売買代金上位, 5 TICK回数, 6 売買高急増, 7 売買代金急増.
var DefaultRankingTypes = []int{1, 2, 3, 4, 5, 6, 7}

// DefaultRankingExchange is the ExchangeDivision fetched: 東証全体, matching
// the exchange code symbols are registered and polled with.
const DefaultRankingExchange = "T"

// Candidates implements broker.CandidateSource: it requests every ranking
// type once and returns the symbols interleaved rank by rank (every type's
// 1st, then every type's 2nd, ...) without duplicates, so each type is
// represented near the top of the merged order. The first failure ends the
// fetch and is returned: a ranking assembled from some of the types is never
// used (the caller then publishes only the held symbols).
func (a *Adapter) Candidates(ctx context.Context) ([]string, error) {
	perType := make([][]string, 0, len(DefaultRankingTypes))
	for _, rankType := range DefaultRankingTypes {
		symbols, err := a.client.RankingSymbols(ctx, rankType, DefaultRankingExchange)
		if err != nil {
			return nil, fmt.Errorf("ranking type %d: %w", rankType, err)
		}
		perType = append(perType, symbols)
	}
	return interleave(perType), nil
}

// interleave merges the per-type rankings rank by rank, keeping a symbol's
// first appearance only.
func interleave(perType [][]string) []string {
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
			out = append(out, sym)
		}
		if !more {
			return out
		}
	}
}
