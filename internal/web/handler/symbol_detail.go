package handler

import (
	"context"
	"errors"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/execution"
)

// symbolJevOutput mirrors docs/api/endpoints.md §5's `GET
// /api/v1/symbols/{symbol}` "jev" section.
type symbolJevOutput struct {
	Direction         *string  `json:"direction"`
	Confidence        *float64 `json:"confidence"`
	Regime            *string  `json:"regime"`
	EntryQuality      *string  `json:"entry_quality"`
	ToxicFlow         *float64 `json:"toxic_flow"`
	LiquidityStressed *float64 `json:"liquidity_stressed"`
}

// symbolRiskOutput mirrors the same endpoint's "risk" section.
type symbolRiskOutput struct {
	AllowedPositionPct float64 `json:"allowed_position_pct"`
	StopLossPct        float64 `json:"stop_loss_pct"`
	TakeProfitPct      float64 `json:"take_profit_pct"`
}

// SymbolAPIOutput is the Huma response body for `GET
// /api/v1/symbols/{symbol}`.
type SymbolAPIOutput struct {
	Body struct {
		Symbol          string           `json:"symbol"`
		Price           float64          `json:"price"`
		VWAP            *float64         `json:"vwap"`
		Jev             symbolJevOutput  `json:"jev"`
		Risk            symbolRiskOutput `json:"risk"`
		CurrentPosition *float64         `json:"current_position"`
	}
}

// SymbolPathInput is every Symbol Detail route's path parameter.
type SymbolPathInput struct {
	Symbol string `path:"symbol" minLength:"1" maxLength:"16" pattern:"^[0-9A-Za-z]+$" doc:"Instrument symbol (alphanumeric, e.g. 7203)."`
}

// APISymbol implements `GET /api/v1/symbols/{symbol}` (docs/api/endpoints.md
// §5): the latest price/VWAP, latest Jev Trader decision (parsed via
// execution.EnrichDecision when read back from repository.DecisionRepository
// - see internal/service/execution/state.go), the shared Risk Engine
// parameters, and the currently open position size (signed: positive for
// LONG, negative for SHORT), or nil when flat.
func (h *SymbolHandler) APISymbol(ctx context.Context, in *SymbolPathInput) (*SymbolAPIOutput, error) {
	state, err := h.provider.State(ctx, in.Symbol)
	if err != nil {
		if errors.Is(err, execution.ErrInstrumentUnknown) {
			return nil, huma.Error404NotFound("unknown symbol", err)
		}
		return nil, huma.Error500InternalServerError("read symbol state failed", err)
	}

	out := &SymbolAPIOutput{}
	out.Body.Symbol = state.Symbol
	out.Body.Price = state.LastPrice
	out.Body.Risk = symbolRiskOutput{
		AllowedPositionPct: h.riskParams.AllowedPositionPct,
		StopLossPct:        h.riskParams.StopLossPct,
		TakeProfitPct:      h.riskParams.TakeProfitPct,
	}

	if state.Position != nil {
		size := float64(state.Position.Quantity)
		if state.Position.Side == domain.PositionSideShort {
			size = -size
		}
		out.Body.CurrentPosition = &size
	}

	if state.LastSignal != domain.JevDirectionNone {
		direction := state.LastSignal
		confidence := state.LastSignalConfidence
		out.Body.Jev = symbolJevOutput{Direction: &direction, Confidence: &confidence}
	}

	return out, nil
}

// candleOutput is one `GET /api/v1/symbols/{symbol}/candles` point. Each
// point is a single sampled price (market_snapshots stores one Price per
// 1-minute bar, not intraday tick-level OHLC - functional.md §3's basic
// 1-minute timeframe), so Open/High/Low/Close all equal that same Price
// rather than fabricating intra-bar movement lightweight-charts' input
// shape implies but this schema does not capture.
type candleOutput struct {
	Time   time.Time `json:"time"`
	Open   float64   `json:"open"`
	High   float64   `json:"high"`
	Low    float64   `json:"low"`
	Close  float64   `json:"close"`
	Volume int64     `json:"volume"`
	VWAP   float64   `json:"vwap"`
}

// CandlesAPIOutput is the Huma response body for `GET
// /api/v1/symbols/{symbol}/candles`.
type CandlesAPIOutput struct {
	Body struct {
		Symbol  string         `json:"symbol"`
		Candles []candleOutput `json:"candles"`
	}
}

// defaultCandlesLookback is how far back `from` defaults to when the
// caller omits it (docs/api/endpoints.md §5 lists `from`/`to` as
// optional query params with no stated default).
const defaultCandlesLookback = 6 * time.Hour

// CandlesInput is `GET /api/v1/symbols/{symbol}/candles`'s path+query
// parameters (docs/api/endpoints.md §5). `1m` is this MVP's only
// supported/native granularity (market_snapshots' own bar size), so
// Interval only admits that value. From/To are RFC3339 (`format:
// date-time`) validated by Huma; the zero time means "not provided".
type CandlesInput struct {
	Symbol   string    `path:"symbol" minLength:"1" maxLength:"16" pattern:"^[0-9A-Za-z]+$" doc:"Instrument symbol (alphanumeric, e.g. 7203)."`
	From     time.Time `query:"from" doc:"RFC3339 start time; defaults to 6 hours before to."`
	To       time.Time `query:"to" doc:"RFC3339 end time; defaults to now."`
	Interval string    `query:"interval" enum:"1m" default:"1m" doc:"Fixed at 1m for this MVP."`
}

// APICandles implements `GET /api/v1/symbols/{symbol}/candles`
// (docs/api/endpoints.md §5): `pitha-price-chart`'s initial candlestick+
// VWAP+volume series.
func (h *SymbolHandler) APICandles(ctx context.Context, in *CandlesInput) (*CandlesAPIOutput, error) {
	to := h.now().UTC()
	if !in.To.IsZero() {
		to = in.To
	}
	from := to.Add(-defaultCandlesLookback)
	if !in.From.IsZero() {
		from = in.From
	}

	snapshots, err := h.provider.Candles(ctx, in.Symbol, from, to)
	if err != nil {
		if errors.Is(err, execution.ErrInstrumentUnknown) {
			return nil, huma.Error404NotFound("unknown symbol", err)
		}
		return nil, huma.Error500InternalServerError("read candles failed", err)
	}

	out := &CandlesAPIOutput{}
	out.Body.Symbol = in.Symbol
	out.Body.Candles = make([]candleOutput, len(snapshots))
	for i, s := range snapshots {
		out.Body.Candles[i] = candleOutput{
			Time: s.Timestamp, Open: s.Price, High: s.Price, Low: s.Price, Close: s.Price,
			Volume: s.Volume, VWAP: s.Feature.VWAP,
		}
	}
	return out, nil
}
