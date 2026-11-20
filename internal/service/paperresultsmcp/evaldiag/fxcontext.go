package evaldiag

import (
	"context"
	"time"

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
	lookback := FxLookbackMinutes * time.Minute
	staleness := FxStalenessMinutes * time.Minute
	buckets, err := splitByReturn(rows, lookback, func(times []time.Time) ([]*float64, error) {
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
	})
	if err != nil {
		return nil, err
	}
	return &FxContext{
		Symbol: symbol, LookbackMinutes: FxLookbackMinutes, StalenessMinutes: FxStalenessMinutes,
		Return: "判断の時刻以前の最新の足の始値の BID/ASK 平均（mid）の、60 分前の時刻以前の最新の足に対する変化の符号（up＝上昇、flat＝変化なし、down＝下落）", Buckets: buckets,
	}, nil
}

// splitByReturn splits the llm arm of rows by the sign of the change of a price
// series over lookback before each decision. price returns, for the given times,
// the series' price known at each (nil: unmatched). A row is unmatched when either
// the price at its time or the one lookback earlier is.
func splitByReturn(rows []evalRow, lookback time.Duration, price func(times []time.Time) ([]*float64, error)) ([]FxBucket, error) {
	at := make([]time.Time, len(rows))
	start := make([]time.Time, len(rows))
	for i, r := range rows {
		at[i], start[i] = r.at, r.at.Add(-lookback)
	}
	now, err := price(at)
	if err != nil {
		return nil, err
	}
	before, err := price(start)
	if err != nil {
		return nil, err
	}
	sums := map[string]float64{}
	counts := map[string]int{}
	for i, r := range rows {
		bucket := FxUnmatched
		if now[i] != nil && before[i] != nil {
			switch d := *now[i] - *before[i]; {
			case d > 0:
				bucket = FxUp
			case d < 0:
				bucket = FxDown
			default:
				bucket = FxFlat
			}
		}
		counts[bucket]++
		sums[bucket] += r.net[iLLM]
	}
	out := []FxBucket{}
	for _, b := range fxBuckets {
		fb := FxBucket{Bucket: b, N: counts[b]}
		if counts[b] > 0 {
			mean := sums[b] / float64(counts[b])
			fb.MeanNetBps = &mean
		}
		out = append(out, fb)
	}
	return out, nil
}
