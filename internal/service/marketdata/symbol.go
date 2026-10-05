package marketdata

import (
	"context"
	"fmt"
	"net/http"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// SymbolInfo is the subset of 銘柄情報 (kabu_STATION_API.yaml
// components.schemas.SymbolSuccess) the entry-eligibility checks need
// (issue #511). MarginSell/UpperLimit/LowerLimit are nil when the API
// reports null (not a stock, e.g. an index).
type SymbolInfo struct {
	Symbol string `json:"Symbol"`
	// MarginSell is 制度信用売建フラグ: true when 制度信用 short selling is
	// possible, which holds exactly for 貸借銘柄.
	MarginSell *bool `json:"MarginSell"`
	// UpperLimit/LowerLimit are the day's 値幅上限/値幅下限 (the stop-high /
	// stop-low prices).
	UpperLimit *float64 `json:"UpperLimit"`
	LowerLimit *float64 `json:"LowerLimit"`
}

// PriceLimit reports whether price sits at the day's upper limit
// (ストップ高) or lower limit (ストップ安), or domain.PriceLimitNone when
// it is neither or the limits are unknown.
func (s SymbolInfo) PriceLimit(price float64) domain.PriceLimit {
	switch {
	case s.UpperLimit != nil && price >= *s.UpperLimit:
		return domain.PriceLimitUp
	case s.LowerLimit != nil && price <= *s.LowerLimit:
		return domain.PriceLimitDown
	}
	return domain.PriceLimitNone
}

// GetSymbol fetches 銘柄情報 for symbol@exchange over REST
// (GET /symbol/{symbol}@{exchange}).
func (c *Client) GetSymbol(ctx context.Context, symbol string, exchange int) (SymbolInfo, error) {
	token, ok := c.Token()
	if !ok {
		return SymbolInfo{}, ErrNoToken
	}
	var info SymbolInfo
	if err := c.doInfo(ctx, http.MethodGet, fmt.Sprintf("/symbol/%s@%d", symbol, exchange), token, nil, &info); err != nil {
		return SymbolInfo{}, err
	}
	return info, nil
}
