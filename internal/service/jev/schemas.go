package jev

import "time"

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
}

// ScoutRequest is the JSON body POSTed to the Jev Scout endpoint: the
// current market state plus which question-set version to evaluate it
// against (prompt_version.go).
type ScoutRequest struct {
	QuestionVersion string     `json:"question_version"`
	State           ScoutState `json:"state"`
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
