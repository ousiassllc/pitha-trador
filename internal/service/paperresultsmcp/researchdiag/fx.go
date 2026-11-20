package researchdiag

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	rlabel "github.com/ousiassllc/pitha-trador/internal/domain/researchlabel"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/evaldiag"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// The optional fx_symbol argument (issue #1134, the same idea as jev_evaluation's
// fx_symbol of issue #1084) adds fx_context: the rows each horizon's statistics
// are over, split by the sign of the pair's mid-price change over the 60 minutes
// before the label's target time (the snapshot the label enters on, which is the
// decision's own bar in a decision scope). The buckets and the match of a time to
// a candle are evaldiag's (FxTrailingBuckets). It is descriptive only: no
// threshold, nothing here reaches trading, the FX record is only read, and
// a call without fx_symbol is unchanged.

// FXSymbolDescription is the schema text of the fx_symbol argument.
const FXSymbolDescription = "通貨ペア（例: USD_JPY）。指定すると、fx_context を足す: 各ホライズン・各側の集計の行を、ラベルの対象時刻（エントリーのスナップショットの時刻。trader / scout では判断のあった足）の直前 60 分のその通貨ペアの mid 価格（研究用の為替 1 分足の記録。足は対象時刻以前で 5 分以内の最新）の変化の符号（up / flat / down / 足が無く判定できない unmatched）で分け、件数と平均 net bps を返す（記述のみ。しきい値なし・取引には使わない）。省略時は出力はこの引数が無かったときと同じ"

// FXSide is one side's rows at one horizon split by the pair's trailing return.
// The bucket counts add up to the side's rows (those the medians are over when
// the horizon's sample percent is below 100); the means are net of the label's cost.
type FXSide struct {
	Buckets []evaldiag.FxBucket `json:"buckets"`
}

// FXHorizon is one horizon's FXSide of each side; a side the scope has no row of
// is null, as in the horizon's statistics.
type FXHorizon struct {
	Horizon string `json:"horizon"`
	// SamplePercent is the share of the horizon's rows the buckets are over
	// (100: all of them; below that, the same k% sample the medians use).
	SamplePercent int     `json:"sample_percent"`
	Long          *FXSide `json:"long"`
	Short         *FXSide `json:"short"`
}

// FXContext is the rows of every horizon split by the pair's trailing return.
type FXContext struct {
	Symbol           string      `json:"symbol"`
	LookbackMinutes  int         `json:"lookback_minutes"`
	StalenessMinutes int         `json:"staleness_minutes"`
	Return           string      `json:"return"`
	Horizons         []FXHorizon `json:"horizons"`
}

// fxCell is the rows of one (horizon, side) at one snapshot time.
type fxCell struct {
	n   int
	net float64
}

// observeFX adds a row's net return to the cell of its snapshot time (at, as
// stored); it does nothing unless the call named fx_symbol.
func (a *acc) observeFX(k key, at string, net float64) {
	if a.fx == nil {
		return
	}
	if a.fx[k] == nil {
		a.fx[k] = map[string]*fxCell{}
	}
	c := a.fx[k][at]
	if c == nil {
		c = &fxCell{}
		a.fx[k][at] = c
	}
	c.n++
	c.net += net
}

// fxContext splits the observed rows by the trailing return of symbol. Every
// distinct snapshot time is matched once, in time order, so the sums are the
// same for the same data.
func (a *acc) fxContext(ctx context.Context, q query.Q, symbol string) (*FXContext, error) {
	seen := map[string]bool{}
	for _, cells := range a.fx {
		for at := range cells {
			seen[at] = true
		}
	}
	stored := make([]string, 0, len(seen))
	for at := range seen {
		stored = append(stored, at)
	}
	slices.Sort(stored)
	times := make([]time.Time, len(stored))
	for i, at := range stored {
		t, err := sqlutil.ParseTime(at)
		if err != nil {
			return nil, fmt.Errorf("researchdiag: snapshot time of a research label: %w", err)
		}
		times[i] = t
	}
	buckets, err := evaldiag.FxTrailingBuckets(ctx, q, symbol, times)
	if err != nil {
		return nil, err
	}
	out := &FXContext{
		Symbol: symbol, LookbackMinutes: evaldiag.FxLookbackMinutes, StalenessMinutes: evaldiag.FxStalenessMinutes,
		Return: evaldiag.FxReturnDescription, Horizons: []FXHorizon{},
	}
	for _, h := range rlabel.Horizons {
		fh := FXHorizon{Horizon: h, SamplePercent: a.percent[h]}
		for _, side := range sides {
			k := key{h, side}
			if a.total(k).n == 0 {
				continue
			}
			counts, sums := map[string]int{}, map[string]float64{}
			for i, at := range stored {
				if c := a.fx[k][at]; c != nil {
					counts[buckets[i]] += c.n
					sums[buckets[i]] += c.net
				}
			}
			fs := &FXSide{Buckets: evaldiag.FxBucketsOf(counts, sums)}
			if side == domain.JevDirectionLong {
				fh.Long = fs
			} else {
				fh.Short = fs
			}
		}
		out.Horizons = append(out.Horizons, fh)
	}
	return out, nil
}
