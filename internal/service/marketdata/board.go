package marketdata

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/marketdata/infolimit"
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
	if err := c.doInfo(ctx, http.MethodPut, "/register", token, registerRequest{Symbols: symbols}, &resp); err != nil {
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

	// BidSign/AskSign are the 気配フラグ of the BidPrice/AskPrice quotes
	// (BoardSuccess.BidSign/AskSign, e.g. "0101" 一般気配, "0102" 特別気配);
	// empty when the API reports none. See IsSpecialQuote.
	BidSign string `json:"BidSign"`
	AskSign string `json:"AskSign"`

	// HighPrice/LowPrice are the session (当日) high/low; nil when the API
	// has not resolved them yet.
	HighPrice *float64 `json:"HighPrice"`
	LowPrice  *float64 `json:"LowPrice"`

	// Sell1..Sell10 / Buy1..Buy10 are the ten displayed book levels.
	// Sell levels sit on the same side as BidPrice/BidQty (the best sell
	// quote) and Buy levels on the AskPrice/AskQty (best buy quote) side
	// (see the swapped-naming note above).
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

// 気配フラグ (BidSign/AskSign) values that mean the quote is a special quote
// (kabu_STATION_API.yaml BoardSuccess.BidSign).
const (
	quoteSignSpecial        = "0102" // 特別気配
	quoteSignSpecialPreHalt = "0108" // 停止前特別気配
)

// IsSpecialQuote reports whether either side of the book is a 特別気配
// (including 停止前特別気配): a price-discovery quote that does not trade
// at the displayed price, so the instrument cannot be entered reliably
// (issue #511).
func (b Board) IsSpecialQuote() bool {
	for _, sign := range [...]string{b.BidSign, b.AskSign} {
		if sign == quoteSignSpecial || sign == quoteSignSpecialPreHalt {
			return true
		}
	}
	return false
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
// Only feed-level errors extend the market_data_down streak; per-symbol
// 4xx such as 4002001 do not (countsAsFeedFailure).
func (c *Client) GetBoard(ctx context.Context, symbol string, exchange int) (Board, error) {
	token, ok := c.Token()
	if !ok {
		c.boardFailures.Fail()
		return Board{}, ErrNoToken
	}

	var board Board
	path := fmt.Sprintf("/board/%s@%d", symbol, exchange)
	if err := c.doInfo(ctx, http.MethodGet, path, token, nil, &board); err != nil {
		if !errors.Is(err, ErrRateLimited) {
			c.status.MarkStale(symbol, err)
			if countsAsFeedFailure(err) {
				c.boardFailures.Fail()
			}
		}
		return Board{}, err
	}
	c.status.MarkFresh(symbol, time.Now().UTC())
	c.boardFailures.Succeed()
	return board, nil
}

const (
	infoAPIRateLimitAttempts = 4 // initial + 3 retries
	infoAPIRateLimitBackoff  = time.Second
)

// doInfoOnce is do plus the process-wide information/register API limiter
// and 429 / 4001006 retry (issue #514). Token issuance stays on do.
func (c *Client) doInfoOnce(ctx context.Context, method, path, token string, body, out any) error {
	var last error
	for attempt := 1; attempt <= infoAPIRateLimitAttempts; attempt++ {
		if err := c.limiter.Wait(ctx); err != nil {
			return err
		}
		err := c.do(ctx, method, path, token, body, out)
		if !IsRateLimit(err) {
			return err
		}
		last = err
		c.limiter.NoteOverflow()
		slog.Warn("marketdata: kabu info api rate limited, backing off",
			"path", path, "attempt", attempt)
		if attempt == infoAPIRateLimitAttempts {
			break
		}
		if err := infolimit.Sleep(ctx, c.limiter.Clock(), infoAPIRateLimitBackoff); err != nil {
			return err
		}
	}
	return fmt.Errorf("%w: %w", ErrRateLimited, last)
}

// RateLimitStats reports the process-wide information-API limiter.
func (c *Client) RateLimitStats() infolimit.Stats { return c.limiter.Stats() }
