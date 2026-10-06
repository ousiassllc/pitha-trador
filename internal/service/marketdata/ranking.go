package marketdata

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
)

// RankingMeasurement is what GET /ranking (詳細ランキング) measurement keeps
// of a response: counts and the 時刻 only. Prices, volumes and symbols of the
// ranking are deliberately never decoded, stored or logged: kabu担当者は取得した
// 時価の保存を利用規約上許容していない (kabusapi#1343, issue #652).
type RankingMeasurement struct {
	// Count is the number of ranking rows returned.
	Count int
	// DuplicateRanks is the number of rows whose 順位 (No) repeats the one of
	// an earlier row (kabu returns 同順位, e.g. two 10th places).
	DuplicateRanks int
	// LatestPriceTime is the latest CurrentPriceTime ("HH:mm", no date) among
	// the rows; empty when the ranking is empty or carries none.
	LatestPriceTime string
}

// rankingResponse decodes only the fields RankingMeasurement needs.
type rankingResponse struct {
	Ranking []struct {
		No               int    `json:"No"`
		CurrentPriceTime string `json:"CurrentPriceTime"`
	} `json:"Ranking"`
}

// MeasureRanking calls GET /ranking for rankType (種別 1〜15) and exchange
// (ExchangeDivision: ALL/T/TP/TS/TG/M/FK/S) and reduces the response to a
// RankingMeasurement. It goes through the same process-wide information-API
// limiter as every other info call. It requires a token.
func (c *Client) MeasureRanking(ctx context.Context, rankType int, exchange string) (RankingMeasurement, error) {
	token, ok := c.Token()
	if !ok {
		return RankingMeasurement{}, ErrNoToken
	}
	q := url.Values{"Type": {strconv.Itoa(rankType)}, "ExchangeDivision": {exchange}}
	var resp rankingResponse
	if err := c.doInfo(ctx, http.MethodGet, "/ranking?"+q.Encode(), token, nil, &resp); err != nil {
		return RankingMeasurement{}, err
	}
	m := RankingMeasurement{Count: len(resp.Ranking)}
	seen := make(map[int]struct{}, len(resp.Ranking))
	for _, row := range resp.Ranking {
		if row.No > 0 {
			if _, dup := seen[row.No]; dup {
				m.DuplicateRanks++
			}
			seen[row.No] = struct{}{}
		}
		if row.CurrentPriceTime > m.LatestPriceTime { // "HH:mm" sorts chronologically
			m.LatestPriceTime = row.CurrentPriceTime
		}
	}
	return m, nil
}
