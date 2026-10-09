package marketdata

import (
	"context"
	"net/http"
	"net/url"
	"strconv"

	"github.com/ousiassllc/pitha-trador/internal/service/broker"
)

// getRanking is GET /ranking for rankType (種別 1〜15) and exchange
// (ExchangeDivision) decoded into out, through the process-wide
// information-API limiter. It requires a token.
func (c *Client) getRanking(ctx context.Context, rankType int, exchange string, out any) error {
	token, ok := c.Token()
	if !ok {
		return broker.ErrNoSession
	}
	q := url.Values{"Type": {strconv.Itoa(rankType)}, "ExchangeDivision": {exchange}}
	return c.doInfo(ctx, http.MethodGet, "/ranking?"+q.Encode(), token, nil, out)
}

// rankingSymbolsResponse decodes only each row's 銘柄コード: the ranking's
// prices, volumes and names are never decoded, so they cannot reach the
// database or a log line (kabu利用規約, kabusapi#1343, issue #652).
type rankingSymbolsResponse struct {
	Ranking []struct {
		Symbol string `json:"Symbol"`
	} `json:"Ranking"`
}

// RankingSymbols calls GET /ranking for rankType and exchange and returns the
// 銘柄コード of its rows in rank order, without duplicates. The codes are for
// in-memory watch-list selection only (docs FR-SCHED-9). An empty ranking
// (kabu answers an empty one on weekdays from about 7:53 until just after
// 9:00) yields an empty slice and no error.
func (c *Client) RankingSymbols(ctx context.Context, rankType int, exchange string) ([]string, error) {
	var resp rankingSymbolsResponse
	if err := c.getRanking(ctx, rankType, exchange, &resp); err != nil {
		return nil, err
	}
	symbols := make([]string, 0, len(resp.Ranking))
	seen := make(map[string]struct{}, len(resp.Ranking))
	for _, row := range resp.Ranking {
		if row.Symbol == "" {
			continue
		}
		if _, dup := seen[row.Symbol]; dup {
			continue
		}
		seen[row.Symbol] = struct{}{}
		symbols = append(symbols, row.Symbol)
	}
	return symbols, nil
}
