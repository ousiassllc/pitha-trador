package evaldiag

import (
	"context"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/indexdiag"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// The optional index_symbol argument (issue #1084) adds index_context to every
// cohort: the LLM arm's rows split by the sign of the index's change over the
// IndexLookbackMinutes before the decision. It is descriptive only: no threshold,
// nothing here reaches trading, and a call without index_symbol is unchanged. The
// buckets are the same as fx_context's (FxUp / FxFlat / FxDown / FxUnmatched).
const (
	// IndexLookbackMinutes is the fixed trailing window of the index return. It is
	// shorter than the FX one because the index has values only in the session: a
	// 60-minute window would leave most morning decisions unmatched.
	IndexLookbackMinutes = 30
	// IndexStalenessMinutes is how old the value matched to a time may be.
	IndexStalenessMinutes = 5
)

// IndexSymbolDescription is the schema text of the index_symbol argument.
const IndexSymbolDescription = "指数コード（立花の指数コード。101＝日経平均、151＝TOPIX）。指定すると、各 cohort に index_context を足す: llm アームの行を、判断の時刻の直前 30 分のその指数の値（研究用の指数の 1 分値の記録。値は判断の時刻の時点で取得済みの最新で、その分の開始から 5 分以内）の変化の符号（up / flat / down / 値が無く判定できない unmatched。立会外・昼休み・欠けは unmatched）で分け、件数と平均 net bps を返す（記述のみ。しきい値なし・取引には使わない）。省略時は出力はこの引数が無かったときと同じ"

// IndexContext is one cohort's llm-arm rows split by the index's trailing return.
type IndexContext struct {
	Symbol           string     `json:"symbol"`
	LookbackMinutes  int        `json:"lookback_minutes"`
	StalenessMinutes int        `json:"staleness_minutes"`
	Return           string     `json:"return"`
	Buckets          []FxBucket `json:"buckets"`
}

// indexContext splits rows' llm arm by the sign of the index's change (the latest
// value known at the decision time against the one IndexLookbackMinutes earlier,
// both within the staleness).
func indexContext(ctx context.Context, q query.Q, symbol string, rows []evalRow) (*IndexContext, error) {
	lookback := IndexLookbackMinutes * time.Minute
	staleness := IndexStalenessMinutes * time.Minute
	buckets, err := splitByReturn(rows, lookback, func(times []time.Time) ([]*float64, error) {
		quotes, err := indexdiag.AtOrBefore(ctx, q, symbol, times, staleness)
		if err != nil {
			return nil, err
		}
		prices := make([]*float64, len(quotes))
		for i, qt := range quotes {
			if qt != nil {
				p := qt.Price
				prices[i] = &p
			}
		}
		return prices, nil
	})
	if err != nil {
		return nil, err
	}
	return &IndexContext{
		Symbol: symbol, LookbackMinutes: IndexLookbackMinutes, StalenessMinutes: IndexStalenessMinutes,
		Return: "判断の時刻の時点で取得済みの最新の値の、30 分前の時刻の時点で取得済みの最新の値に対する変化の符号（up＝上昇、flat＝変化なし、down＝下落）", Buckets: buckets,
	}, nil
}
