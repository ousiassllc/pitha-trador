// Package fxdiag is the read-only analysis behind the Paper results MCP's
// fx_series_summary tool and the FX join of jev_evaluation (issue #1084,
// docs/api/endpoints/paper-results-mcp-research-fx.md): what the research FX
// 1-minute candle record holds per currency pair (count, first and last time, the
// trading days recorded / market closed / missing, interior gaps), and the
// at-or-before join of those candles to decision times.
//
// It reads research_fx_candles, research_fx_days and the two research.fx.*
// runtime_settings keys through the read-only connection and writes nothing. Its
// numbers are descriptive research output, never an input of trading.
package fxdiag

import "time"

// Limits of one call.
const (
	// MaxGapRuns is the most interior-gap runs listed per series; the totals
	// (runs, missing_minutes) always cover every run.
	MaxGapRuns = 50
	// MaxSymbolLen is the width of a pair name (USD_JPY).
	MaxSymbolLen = 7
)

// Notice is returned with every answer.
const Notice = "研究専用の記録の集計であり、取引の入力ではない。Feature Engine・Policy・Risk・発注・Paper ガード・Live・Jev のプロンプト・メタモデルの学習はこの値を読まない。" +
	"GMOコインの公開 API の値で、研究用にのみ使い、再配布しない"

// Definitions are the terms of the answer.
var Definitions = map[string]string{
	"trading_day":         "GMO の取引日（JST 06:00 切り替え。その日の 06:00 JST から翌 06:00 JST）の日付。期間（from, to）はこの日付で絞る",
	"recorded_days":       "足を 1 本以上記録した取引日の数",
	"market_closed_days":  "GMO が答えたが足が 0 本だった取引日（土日・休日など市場が閉まっている日）。欠けではない",
	"missing_days":        "記録を始めた取引日（最初の印の日）以降で、取引日が終わっているのに印の無い日。まだ取っていない・取得に失敗した・取得期間（直近の数日）を過ぎて取れなかった日で、欠け",
	"interior_gaps":       "足のある取引日の中で、その日の最初の足と最後の足の間に足が 1 本も無い分の連続（start〜end の分数）。市場が閉まっている日（足が 0 本の日）や日をまたぐ区間は含まない。取引の少ない分の欠落（GMO が足を返さなかった分）の可能性もある",
	"bid_only_minutes":    "売値（BID）の足だけがある分（買値 ASK は NULL）",
	"ask_only_minutes":    "買値（ASK）の足だけがある分（売値 BID は NULL）",
	"before_recording":    "最初の印の日より前の取引日は、記録を始める前なので欠けに数えない",
	"in_progress_day":     "いまの取引日（まだ 06:00 JST の切り替えが来ていない日）は終わっていないので、欠けにも数えない",
	"series_not_recorded": "設定の通貨ペアで、印がまだ 1 つも無いものは、件数 0・日数 0 で返す",
}

// Recording is the recorder's state, read from runtime_settings.
type Recording struct {
	Enabled      bool     `json:"enabled"`
	Symbols      []string `json:"symbols"`
	LookbackDays int      `json:"lookback_days"`
}

// Days counts the trading days of a series in the period.
type Days struct {
	Recorded     int      `json:"recorded"`
	MarketClosed int      `json:"market_closed"`
	Missing      int      `json:"missing"`
	ClosedDays   []string `json:"market_closed_days"`
	MissingDays  []string `json:"missing_days"`
	// FirstMarkedDay is the first day the series has a marker (over all time:
	// before it, days are not missing); null without a marker.
	FirstMarkedDay *string `json:"first_marked_day"`
	// RecordedThrough is the latest marked trading day in the period.
	RecordedThrough *string `json:"recorded_through"`
}

// GapRun is one run of consecutive minutes without a candle inside a recorded day.
type GapRun struct {
	TradingDay string `json:"trading_day"`
	Start      string `json:"start"`
	End        string `json:"end"`
	Minutes    int    `json:"minutes"`
}

// InteriorGaps lists the gap runs (at most MaxGapRuns, oldest first); Runs and
// MissingMinutes cover all of them.
type InteriorGaps struct {
	Runs           int      `json:"runs"`
	MissingMinutes int      `json:"missing_minutes"`
	List           []GapRun `json:"list"`
	Truncated      bool     `json:"truncated"`
}

// Series is one currency pair's record in the period.
type Series struct {
	Symbol         string       `json:"symbol"`
	Configured     bool         `json:"configured"`
	Candles        int          `json:"candles"`
	BidOnlyMinutes int          `json:"bid_only_minutes"`
	AskOnlyMinutes int          `json:"ask_only_minutes"`
	FirstOpenTime  *string      `json:"first_open_time"`
	LastOpenTime   *string      `json:"last_open_time"`
	Days           Days         `json:"days"`
	InteriorGaps   InteriorGaps `json:"interior_gaps"`
}

// Data is fx_series_summary's data.
type Data struct {
	From        string            `json:"from"`
	To          string            `json:"to"`
	Notice      string            `json:"notice"`
	Recording   Recording         `json:"recording"`
	Series      []Series          `json:"series"`
	Definitions map[string]string `json:"definitions"`
}

// minute is the candle width.
const minute = time.Minute
