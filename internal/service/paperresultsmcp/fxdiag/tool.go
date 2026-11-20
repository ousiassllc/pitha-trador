package fxdiag

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/researchfxsettings"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// Description is the tool's description (docs/api/endpoints/research-tools/paper-results-mcp-research-fx.md §3.46).
const Description = "研究用の為替 1 分足の記録の読み取り専用の集計（取引の入力ではない）: 通貨ペア（系列）ごとに、期間（JST の GMO 取引日。06:00 JST 切り替え）内の足の件数・" +
	"最初と最後の足の開始時刻（UTC）・売値（BID）だけ／買値（ASK）だけの分の数、取引日の内訳（recorded＝足のある日、market_closed＝GMO が答えたが足が 0 本の日＝土日・休日など市場休みで欠けではない、" +
	"missing＝記録を始めた日以降で取引日が終わっているのに印の無い日＝まだ取っていない・取得失敗・取得期間（直近の数日）切れで、欠け）、足のある日の最初と最後の足の間の欠け（interior_gaps: 連続した欠けの start・end・minutes。件数は上限つき）。" +
	"recording に記録のオン・オフ（research.fx.enabled。既定オフ）と通貨ペアの設定。symbol で 1 ペアに絞れる。記録・設定・取引は変えない。しきい値の判定は返さない"

// SchemaProperties are the tool's own arguments besides the period.
func SchemaProperties() map[string]any {
	return map[string]any{
		"symbol": map[string]any{
			"type": "string", "pattern": "^[A-Z]{3}_[A-Z]{3}$", "maxLength": MaxSymbolLen,
			"description": "この通貨ペア（例: USD_JPY）だけを返す。省略は記録のある全ペアと設定中のペア",
		},
	}
}

// Request is the tool's arguments as decoded from the call: the period (From,
// To, as every period tool takes it) and the tool's own. Decoding rejects an
// unknown field and an argument Validate refuses.
type Request struct {
	From   *string `json:"from"`
	To     *string `json:"to"`
	Symbol string  `json:"symbol"`
}

// UnmarshalJSON decodes strictly and validates, so the dispatcher's decode of a
// bad call is an invalid_argument without a check of its own.
func (r *Request) UnmarshalJSON(b []byte) error {
	type plain Request
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode((*plain)(r)); err != nil {
		return err
	}
	return r.Validate()
}

// Validate reports an argument the tool does not accept.
func (r Request) Validate() error {
	if r.Symbol != "" && !researchfxsettings.ValidSymbol(r.Symbol) {
		return fmt.Errorf("fxdiag: symbol %q is not a currency pair like USD_JPY", r.Symbol)
	}
	return nil
}

// Run summarizes the research FX record of [r.From, r.To) and returns the data
// and the number of series.
func Run(ctx context.Context, q query.Q, r query.Range, now time.Time, req Request) (any, int, error) {
	d, n, err := Summarize(ctx, q, r, now, req.Symbol)
	return d, n, err
}
