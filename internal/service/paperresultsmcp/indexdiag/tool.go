package indexdiag

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/researchindexsettings"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// Description is the tool's description (docs/api/endpoints/research-tools/paper-results-mcp-research-index.md §3.47).
const Description = "研究用の指数（日経平均・TOPIX など）の 1 分値の記録の読み取り専用の集計（取引の入力ではない）: 指数コード（系列。101＝日経平均、151＝TOPIX）ごとに、期間（JST の立会日）内の値の件数・" +
	"最初と最後の分の開始時刻（UTC）・立花の時価の記録に残っている行数（source_samples）、立会日の内訳（recorded＝値のある日、unavailable＝確認したが立花の記録に値が 1 件も無かった日＝取れていない、" +
	"missing＝記録を始めた日以降で立会日が終わっているのに印の無い日＝まだ確認していない・確認失敗・取得期間（直近の数立会日）切れで、欠け。土日・祝日は立会が無いので数えない）、" +
	"値のある日の最初と最後の値の間の、立会の分の欠け（interior_gaps: 連続した欠けの start・end・minutes。昼休みは数えない。件数は上限つき）。" +
	"recording に記録のオン・オフ（research.index.enabled。既定オフ）と指数コードの設定。symbol で 1 系列に絞れる。記録・設定・取引は変えない。しきい値の判定は返さない"

// SchemaProperties are the tool's own arguments besides the period.
func SchemaProperties() map[string]any {
	return map[string]any{
		"symbol": map[string]any{
			"type": "string", "pattern": "^[0-9A-Za-z]{1,8}$", "maxLength": MaxSymbolLen,
			"description": "この指数コード（例: 101）だけを返す。省略は記録のある全系列と設定中のもの",
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
	if r.Symbol != "" && !researchindexsettings.ValidSymbol(r.Symbol) {
		return fmt.Errorf("indexdiag: symbol %q is not an index code like 101", r.Symbol)
	}
	return nil
}

// Run summarizes the research index record of [r.From, r.To) and returns the data
// and the number of series.
func Run(ctx context.Context, q query.Q, r query.Range, now time.Time, req Request) (any, int, error) {
	d, n, err := Summarize(ctx, q, r, now, req.Symbol)
	return d, n, err
}
