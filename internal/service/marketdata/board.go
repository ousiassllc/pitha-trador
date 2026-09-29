package marketdata

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// RegisterSymbol identifies one instrument for PUSH registration
// (kabu_STATION_API.yaml components.schemas.RequestRegister).
type RegisterSymbol struct {
	Symbol   string `json:"Symbol"`
	Exchange int    `json:"Exchange"`
}

type registerRequest struct {
	Symbols []RegisterSymbol `json:"Symbols"`
}

// RegisterSuccess lists the symbols currently registered for PUSH after a
// RegisterSymbols call.
type RegisterSuccess struct {
	RegistList []RegisterSymbol `json:"RegistList"`
}

// RegisterSymbols registers symbols with kabuステーションAPI so their
// price/board updates are delivered over the PUSH WebSocket
// (overview.md §5 "銘柄登録・PUSH購読"). It requires a token (IssueToken
// or Start must be called first).
func (c *Client) RegisterSymbols(ctx context.Context, symbols []RegisterSymbol) (RegisterSuccess, error) {
	token, ok := c.Token()
	if !ok {
		return RegisterSuccess{}, ErrNoToken
	}

	var resp RegisterSuccess
	if err := c.do(ctx, http.MethodPut, "/register", token, registerRequest{Symbols: symbols}, &resp); err != nil {
		return RegisterSuccess{}, err
	}
	return resp, nil
}

// Board is the 時価情報・板情報 response for one symbol
// (kabu_STATION_API.yaml components.schemas.BoardSuccess). Bid/Ask
// price/quantity fields are nil when kabuステーションAPI has not yet
// resolved a value for a newly-registered symbol (FR-FE-2 欠損値扱い).
//
// Note: kabuステーションAPI's "Bid"/"Ask" field names are reported
// swapped from their conventional meaning (BoardSuccess's description:
// BidPrice is actually the best sell/offer quote, AskPrice the best
// buy/bid quote). This struct keeps the raw field names as documented by
// the API so callers can cross-reference the official reference.
type Board struct {
	Symbol        string   `json:"Symbol"`
	SymbolName    string   `json:"SymbolName"`
	CurrentPrice  float64  `json:"CurrentPrice"`
	VWAP          float64  `json:"VWAP"`
	TradingVolume float64  `json:"TradingVolume"`
	TradingValue  float64  `json:"TradingValue"`
	BidPrice      *float64 `json:"BidPrice"`
	BidQty        *float64 `json:"BidQty"`
	AskPrice      *float64 `json:"AskPrice"`
	AskQty        *float64 `json:"AskQty"`
}

// GetBoard fetches 時価情報・板情報 (current price, session VWAP,
// cumulative volume/turnover, and best bid/ask) for symbol@exchange over
// REST (overview.md §4 Market Data Client, §5). It records the result
// with the Client's StatusTracker: fresh on success, stale on any error.
func (c *Client) GetBoard(ctx context.Context, symbol string, exchange int) (Board, error) {
	token, ok := c.Token()
	if !ok {
		c.boardFailures.Fail()
		return Board{}, ErrNoToken
	}

	var board Board
	path := fmt.Sprintf("/board/%s@%d", symbol, exchange)
	if err := c.do(ctx, http.MethodGet, path, token, nil, &board); err != nil {
		c.status.MarkStale(symbol, err)
		c.boardFailures.Fail()
		return Board{}, err
	}
	c.status.MarkFresh(symbol, time.Now().UTC())
	c.boardFailures.Succeed()
	return board, nil
}
