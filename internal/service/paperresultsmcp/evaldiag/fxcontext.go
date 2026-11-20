package evaldiag

import (
	"context"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/researchfxsettings"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/fxdiag"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// The optional fx_symbol argument (issue #1084) adds fx_context to every cohort:
// the LLM arm's rows split by the sign of the pair's mid-price change over the
// FxLookbackMinutes before the decision. It is descriptive only: no threshold,
// nothing here reaches trading, and a call without fx_symbol is unchanged.
const (
	// FxLookbackMinutes is the fixed trailing window of the mid-price return.
	FxLookbackMinutes = 60
	// FxStalenessMinutes is how old the candle matched to a time may be.
	FxStalenessMinutes = 5
)

// Buckets of the trailing return's sign. Unmatched: no candle within the
// staleness bound at the decision time or at the start of the window.
const (
	FxUp        = "up"
	FxFlat      = "flat"
	FxDown      = "down"
	FxUnmatched = "unmatched"
)

// FxSymbolDescription is the schema text of the fx_symbol argument.
const FxSymbolDescription = "通貨ペア（例: USD_JPY）。指定すると、各 cohort に fx_context を足す: llm アームの行を、判断の時刻の直前 60 分のその通貨ペアの mid 価格（研究用の為替 1 分足の記録。足は判断の時刻以前で 5 分以内の最新）の変化の符号（up / flat / down / 足が無く判定できない unmatched）で分け、件数と平均 net bps を返す（記述のみ。しきい値なし・取引には使わない）。省略時は出力はこの引数が無かったときと同じ"

// ValidFxSymbol reports whether s is a currency pair the fx_symbol argument
// accepts (like USD_JPY); jev_evaluation and research_horizon_summary share it.
func ValidFxSymbol(s string) bool { return researchfxsettings.ValidSymbol(s) }

// FxReturnDescription says what the bucket is (the return field of fx_context).
const FxReturnDescription = "判断の時刻以前の最新の足の始値の BID/ASK 平均（mid）の、60 分前の時刻以前の最新の足に対する変化の符号（up＝上昇、flat＝変化なし、down＝下落）"

var fxBuckets = []string{FxUp, FxFlat, FxDown, FxUnmatched}

// FxBucket is the LLM arm's net return (bps, after the label's cost) over the
// rows of one bucket. MeanNetBps is null while N is 0.
type FxBucket struct {
	Bucket     string   `json:"bucket"`
	N          int      `json:"n"`
	MeanNetBps *float64 `json:"mean_net_bps"`
}

// FxContext is one cohort's llm-arm rows split by the pair's trailing return.
type FxContext struct {
	Symbol           string     `json:"symbol"`
	LookbackMinutes  int        `json:"lookback_minutes"`
	StalenessMinutes int        `json:"staleness_minutes"`
	Return           string     `json:"return"`
	Buckets          []FxBucket `json:"buckets"`
}

// fxContext splits rows' llm arm by the sign of the symbol's mid-price change
// (candle open mid at the decision time against the one FxLookbackMinutes
// earlier, both the latest candle at or before the time within the staleness).
func fxContext(ctx context.Context, q query.Q, symbol string, rows []evalRow) (*FxContext, error) {
	buckets, err := splitByReturn(rows, FxLookbackMinutes*time.Minute, fxMid(ctx, q, symbol))
	if err != nil {
		return nil, err
	}
	return &FxContext{
		Symbol: symbol, LookbackMinutes: FxLookbackMinutes, StalenessMinutes: FxStalenessMinutes,
		Return: FxReturnDescription, Buckets: buckets,
	}, nil
}

// FxTrailingBuckets is the bucket of each time: the sign of the symbol's mid-price
// change (candle open mid at the time against the one FxLookbackMinutes earlier,
// both the latest candle at or before the time within the staleness). It only
// reads research_fx_candles, and the result never reaches trading.
func FxTrailingBuckets(ctx context.Context, q query.Q, symbol string, times []time.Time) ([]string, error) {
	return trailingBuckets(times, FxLookbackMinutes*time.Minute, fxMid(ctx, q, symbol))
}

// fxMid is the price function of the FX series: the candle open mid at or before
// each time within the staleness.
func fxMid(ctx context.Context, q query.Q, symbol string) func(times []time.Time) ([]*float64, error) {
	staleness := FxStalenessMinutes * time.Minute
	return func(times []time.Time) ([]*float64, error) {
		quotes, err := fxdiag.AtOrBefore(ctx, q, symbol, times, staleness)
		if err != nil {
			return nil, err
		}
		mids := make([]*float64, len(quotes))
		for i, qt := range quotes {
			if qt != nil {
				mid := qt.MidOpen()
				mids[i] = &mid
			}
		}
		return mids, nil
	}
}

// trailingBuckets is the bucket of each time by the sign of the change of a price
// series over lookback before it. price returns, for the given times, the series'
// price known at each (nil: unmatched). A time is unmatched when either the price
// at it or the one lookback earlier is.
func trailingBuckets(times []time.Time, lookback time.Duration, price func(times []time.Time) ([]*float64, error)) ([]string, error) {
	start := make([]time.Time, len(times))
	for i, t := range times {
		start[i] = t.Add(-lookback)
	}
	now, err := price(times)
	if err != nil {
		return nil, err
	}
	before, err := price(start)
	if err != nil {
		return nil, err
	}
	out := make([]string, len(times))
	for i := range times {
		out[i] = FxUnmatched
		if now[i] != nil && before[i] != nil {
			switch d := *now[i] - *before[i]; {
			case d > 0:
				out[i] = FxUp
			case d < 0:
				out[i] = FxDown
			default:
				out[i] = FxFlat
			}
		}
	}
	return out, nil
}

// FxBucketsOf lists the four buckets in order with their row count and mean net
// (sum over count; null while the count is 0).
func FxBucketsOf(counts map[string]int, sums map[string]float64) []FxBucket {
	out := []FxBucket{}
	for _, b := range fxBuckets {
		fb := FxBucket{Bucket: b, N: counts[b]}
		if counts[b] > 0 {
			mean := sums[b] / float64(counts[b])
			fb.MeanNetBps = &mean
		}
		out = append(out, fb)
	}
	return out
}

// splitByReturn splits the llm arm of rows by the sign of the change of a price
// series over lookback before each decision (trailingBuckets at each decision time).
func splitByReturn(rows []evalRow, lookback time.Duration, price func(times []time.Time) ([]*float64, error)) ([]FxBucket, error) {
	at := make([]time.Time, len(rows))
	for i, r := range rows {
		at[i] = r.at
	}
	buckets, err := trailingBuckets(at, lookback, price)
	if err != nil {
		return nil, err
	}
	sums := map[string]float64{}
	counts := map[string]int{}
	for i, r := range rows {
		counts[buckets[i]]++
		sums[buckets[i]] += r.net[iLLM]
	}
	return FxBucketsOf(counts, sums), nil
}
