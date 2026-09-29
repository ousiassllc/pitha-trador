package marketdata

import (
	"context"
	"fmt"
	"math"
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

	// HighPrice/LowPrice are the session (当日) high/low; nil when the API
	// has not resolved them yet.
	HighPrice *float64 `json:"HighPrice"`
	LowPrice  *float64 `json:"LowPrice"`

	// Sell1..Sell10 / Buy1..Buy10 are the ten displayed book levels.
	// Sell levels sit on the same side as BidPrice/BidQty and Buy levels
	// on the AskPrice/AskQty side (see the swapped-naming note above).
	Sell1  *BoardLevel `json:"Sell1"`
	Sell2  *BoardLevel `json:"Sell2"`
	Sell3  *BoardLevel `json:"Sell3"`
	Sell4  *BoardLevel `json:"Sell4"`
	Sell5  *BoardLevel `json:"Sell5"`
	Sell6  *BoardLevel `json:"Sell6"`
	Sell7  *BoardLevel `json:"Sell7"`
	Sell8  *BoardLevel `json:"Sell8"`
	Sell9  *BoardLevel `json:"Sell9"`
	Sell10 *BoardLevel `json:"Sell10"`
	Buy1   *BoardLevel `json:"Buy1"`
	Buy2   *BoardLevel `json:"Buy2"`
	Buy3   *BoardLevel `json:"Buy3"`
	Buy4   *BoardLevel `json:"Buy4"`
	Buy5   *BoardLevel `json:"Buy5"`
	Buy6   *BoardLevel `json:"Buy6"`
	Buy7   *BoardLevel `json:"Buy7"`
	Buy8   *BoardLevel `json:"Buy8"`
	Buy9   *BoardLevel `json:"Buy9"`
	Buy10  *BoardLevel `json:"Buy10"`
}

// BoardLevel is one displayed book level
// (kabu_STATION_API.yaml BoardSuccess.Sell1..Sell10/Buy1..Buy10).
type BoardLevel struct {
	Price float64 `json:"Price"`
	Qty   float64 `json:"Qty"`
}

// HasPrice reports whether CurrentPrice is a usable last price. kabuステーション
// API leaves it 0 (decoded from 0 or null) while it is unresolved (寄り付き前・
// 未約定銘柄), so anything not finite and positive means "missing", never a
// price of 0 (issue #173).
func (b Board) HasPrice() bool {
	return b.CurrentPrice > 0 && !math.IsInf(b.CurrentPrice, 0)
}

// SellDepth is the total quantity across the reported Sell levels, and
// false when the API reported none.
func (b Board) SellDepth() (float64, bool) {
	return levelDepth(b.Sell1, b.Sell2, b.Sell3, b.Sell4, b.Sell5, b.Sell6, b.Sell7, b.Sell8, b.Sell9, b.Sell10)
}

// BuyDepth is the total quantity across the reported Buy levels, and
// false when the API reported none.
func (b Board) BuyDepth() (float64, bool) {
	return levelDepth(b.Buy1, b.Buy2, b.Buy3, b.Buy4, b.Buy5, b.Buy6, b.Buy7, b.Buy8, b.Buy9, b.Buy10)
}

func levelDepth(levels ...*BoardLevel) (float64, bool) {
	var total float64
	found := false
	for _, l := range levels {
		if l != nil {
			total += l.Qty
			found = true
		}
	}
	return total, found
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
