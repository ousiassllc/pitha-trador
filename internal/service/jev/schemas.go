package jev

import (
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/rag"
)

// Momentum quality values FR-SCOUT-1's momentum_quality question can
// return (functional.md §4.4).
const (
	MomentumQualityWeak        = "weak"
	MomentumQualityModerate    = "moderate"
	MomentumQualityStrong      = "strong"
	MomentumQualityExceptional = "exceptional"
)

// ScoutState is the structured market state sent to Jev for one Scout
// evaluation (architecture/overview.md §6 "リクエストスキーマ"). It carries
// only Feature Engine-computed values, sanitized of SQLite row ids and
// other DB-internal metadata: Jev never sees primary keys, only the
// market state itself. Pointer fields mirror domain.Feature's own
// nullable fields (a nil value means "not yet computable this cycle",
// not "zero"; docs/architecture/er.md §market_snapshots §型・規約).
type ScoutState struct {
	Symbol             string    `json:"symbol"`
	Timestamp          time.Time `json:"timestamp"`
	Price              float64   `json:"price"`
	Return1m           *float64  `json:"return_1m,omitempty"`
	Return5m           *float64  `json:"return_5m,omitempty"`
	Return15m          *float64  `json:"return_15m,omitempty"`
	VWAP               float64   `json:"vwap"`
	PriceVsVWAPBps     float64   `json:"price_vs_vwap_bps"`
	VolumeRatio5m      *float64  `json:"volume_ratio_5m,omitempty"`
	SpreadBps          *float64  `json:"spread_bps,omitempty"`
	OrderbookImbalance *float64  `json:"orderbook_imbalance,omitempty"`
	RealizedVol5m      *float64  `json:"realized_vol_5m,omitempty"`
	MarketReturn5m     *float64  `json:"market_return_5m,omitempty"`
	SectorReturn5m     *float64  `json:"sector_return_5m,omitempty"`

	// The remaining functional.md §4.1 features (best_bid/best_ask through
	// market_breadth), same nil-means-missing convention as above.
	Return3m                      *float64 `json:"return_3m,omitempty"`
	Return30m                     *float64 `json:"return_30m,omitempty"`
	HighDistance5m                *float64 `json:"high_distance_5m,omitempty"`
	LowDistance5m                 *float64 `json:"low_distance_5m,omitempty"`
	SessionHighDistance           *float64 `json:"session_high_distance,omitempty"`
	SessionLowDistance            *float64 `json:"session_low_distance,omitempty"`
	VWAPSlope                     *float64 `json:"vwap_slope,omitempty"`
	VWAPCrossDirection            *int64   `json:"vwap_cross_direction,omitempty"`
	Volume1m                      *int64   `json:"volume_1m,omitempty"`
	Volume5m                      *int64   `json:"volume_5m,omitempty"`
	VolumeRatio1m                 *float64 `json:"volume_ratio_1m,omitempty"`
	Turnover1m                    *float64 `json:"turnover_1m,omitempty"`
	Turnover5m                    *float64 `json:"turnover_5m,omitempty"`
	ATR1m                         *float64 `json:"atr_1m,omitempty"`
	ATR5m                         *float64 `json:"atr_5m,omitempty"`
	RealizedVol15m                *float64 `json:"realized_vol_15m,omitempty"`
	VolatilityExpansionRatio      *float64 `json:"volatility_expansion_ratio,omitempty"`
	BestBid                       *float64 `json:"best_bid,omitempty"`
	BestAsk                       *float64 `json:"best_ask,omitempty"`
	BidDepth                      *float64 `json:"bid_depth,omitempty"`
	AskDepth                      *float64 `json:"ask_depth,omitempty"`
	BuyTradeRatio                 *float64 `json:"buy_trade_ratio,omitempty"`
	SellTradeRatio                *float64 `json:"sell_trade_ratio,omitempty"`
	TradeFlowImbalance            *float64 `json:"trade_flow_imbalance,omitempty"`
	Microprice                    *float64 `json:"microprice,omitempty"`
	MarketReturn1m                *float64 `json:"market_return_1m,omitempty"`
	StockVsSectorRelativeStrength *float64 `json:"stock_vs_sector_relative_strength,omitempty"`
	MarketBreadth                 *float64 `json:"market_breadth,omitempty"`

	// NewsContext is the symbol's recent Luna-classified news (FR-LUNA-3),
	// injected by Scout/Trader from their NewsSource. nil means no news
	// context - Luna disabled, failing, or nothing recent. It is auxiliary
	// context only; it never overrides Jev's judgment (FR-LUNA-5).
	NewsContext *domain.NewsContext `json:"news_context,omitempty"`
}

// ScoutRequest is the JSON body POSTed to the Jev Scout endpoint: the
// current market state, which question-set version to evaluate it
// against (prompt_version.go), and the RAG few-shot context (§7,
// functional.md FR-RAG-3) found for that state.
type ScoutRequest struct {
	QuestionVersion string      `json:"question_version"`
	State           ScoutState  `json:"state"`
	RAGContext      rag.Context `json:"rag_context"`
}

// ScoutResponse is Jev's raw answer to the FR-SCOUT-1 question group.
//
// InterestingNow, LiquidityOk and AbnormalActivity are each a yes-
// probability in [0, 1]: functional.md §4.4 describes them as "yes/no型"
// questions, but FR-SCOUT-2's pass condition thresholds them numerically
// (e.g. "interesting_now >= 0.65"), so Jev answers with a confidence
// score for "yes" rather than a boolean - the same convention
// FR-TRADER-2 documents for Jev Trader's yes/no questions.
type ScoutResponse struct {
	InterestingNow   float64  `json:"interesting_now"`
	MomentumQuality  string   `json:"momentum_quality"`
	LiquidityOk      float64  `json:"liquidity_ok"`
	AbnormalActivity float64  `json:"abnormal_activity"`
	ModelID          string   `json:"model_id"`
	RequestCost      *float64 `json:"request_cost,omitempty"`
}

// TraderRequest is the JSON body POSTed to the Jev Trader endpoint: the
// same market state format ScoutRequest sends (a Scout-passed candidate
// is re-evaluated against its current state), which question-set
// version to evaluate it against (prompt_version.go), and the RAG
// few-shot context (FR-RAG-3) found for that state.
type TraderRequest struct {
	QuestionVersion string      `json:"question_version"`
	State           ScoutState  `json:"state"`
	RAGContext      rag.Context `json:"rag_context"`
}

// TraderResponse is Jev's raw answer to the FR-TRADER-1 question group.
//
// Direction is one of domain.JevDirection{Long,Short,None}. Regime is
// one of domain.JevRegime{Trend,Range,Breakout,Chaotic}. EntryQuality is
// one of domain.JevEntryQuality{Poor,Fair,Good,Strong,Exceptional}
// ("poor〜exceptional", functional.md §4.5).
//
// ToxicFlow, LiquidityStressed and ContinuationProbability are each a
// yes-probability in [0, 1], the same "yes/no型だがJevは確信度で答える"
// convention ScoutResponse's doc comment describes for FR-SCOUT-1.
//
// Confidence is Jev's own confidence in Direction. FR-TRADER-2: it must
// never be treated as a verified probability of an actual price move -
// only Calibration (functional.md §4.9) may draw that conclusion, by
// comparing it against calibration_outcomes after the fact. No logic in
// this package (or its callers, e.g. a later Policy Engine sub-scope)
// may substitute Confidence for a real outcome probability.
type TraderResponse struct {
	Direction               string   `json:"direction"`
	Regime                  string   `json:"regime"`
	EntryQuality            string   `json:"entry_quality"`
	ToxicFlow               float64  `json:"toxic_flow"`
	LiquidityStressed       float64  `json:"liquidity_stressed"`
	ContinuationProbability float64  `json:"continuation_probability"`
	Confidence              float64  `json:"confidence"`
	ModelID                 string   `json:"model_id"`
	RequestCost             *float64 `json:"request_cost,omitempty"`
}
