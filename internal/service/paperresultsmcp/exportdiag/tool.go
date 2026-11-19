// Package exportdiag is the Paper results MCP's training_dataset_export tool (issue #1094): a thin
// adapter over internal/service/datasetexport. The tool writes files only (into the local exports
// folder next to the database, a path no argument can change) and returns the manifest, never a row.
package exportdiag

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/datasetexport"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// Description is the tool's description (docs/api/endpoints/research-tools/paper-results-mcp-dataset-export.md).
const Description = "学習用データセットの書き出し（研究用。取引には使わない）。期間内の decision_type=trader の判断を 1 判断 = 1 行の CSV と Parquet（同じ行・同じ列）にして、" +
	"この PC のローカルフォルダ（DB ファイルと同じ場所の exports/<dataset_id>/。引数では変えられない）に書き出し、応答には manifest だけを返す（行の中身・秘密は返さない）。" +
	"列は固定の許可リスト: 判断の識別子・Jev の構造化された答え（direction・confidence・regime・entry_quality・toxic_flow・liquidity_stressed・continuation_probability のみ）・" +
	"判断の足の派生特徴量（板・気配の生の値と価格水準は含まない）・ゲート結果（取引候補の有無・Risk 通過・理由は固定コード）・" +
	"ラベル（保存済み calibration の label_version 2・研究用 30m/60m/close・見送り候補の反実仮想）・シャドー（最大 3 版）とお試し（最大 4 アーム）の答え。" +
	"split_at（JST の日付。期間の内側）を渡すと、判断の日が split_at より前を train、split_at + embargo_days 以降を eval に分け（dataset_train.csv / dataset_eval.csv）、" +
	"間の日は書き出さず rows_in_embargo に数え、train のラベルの窓が eval の開始に届く行は rows_purged として除く。Parquet は同名の .parquet（dataset_train.parquet / dataset_eval.parquet）。分割なしは dataset.csv と dataset.parquet。" +
	"manifest に schema_version・ラベルの版・件数・列・除外した列・ファイルごとの sha256（CSV・Parquet）・期間と分け方・全体の dataset_sha256（期間 from / to・split_at・embargo_days とファイルの sha256 から決まる。dataset_id はその先頭 12 桁）・save_dir を含む。" +
	"同じ期間・同じ分け方・同じ内容は同じバイト列・同じ dataset_id になり（期間か分け方が違えば中身が同じでも別の dataset_id）、内容の違う同じ名前のフォルダは上書きしない。Settings の research.export.enabled が 0（初期値）のあいだは何も書き出さない（フォルダも作らない）。" +
	"1 回に書き出せる判断は 20000 件まで（超えたら limit_exceeded。期間を狭める）。DB は書き換えない"

// SchemaProperties are the tool's own arguments besides the period.
func SchemaProperties() map[string]any {
	return map[string]any{
		"split_at": map[string]any{
			"type": "string", "pattern": `^\d{4}-\d{2}-\d{2}$`,
			"description": "分割の境界（JST の日付 YYYY-MM-DD）。from より後・to より前。判断の日が split_at より前は train、split_at + embargo_days 以降は eval。省略は分割なし（from と to の指定が必要）",
		},
		"embargo_days": map[string]any{
			"type": "integer", "minimum": 0, "maximum": datasetexport.MaxEmbargoDays, "default": 0,
			"description": "train と eval の間に空ける日数（0〜30。省略は 0）。その日の判断は書き出さず rows_in_embargo に数える。split_at が必要",
		},
	}
}

// Request is the tool's arguments as decoded from the call: the period (From, To) and the split.
// There is deliberately no path argument. Decoding rejects an unknown field and an argument
// Validate refuses.
type Request struct {
	From        *string `json:"from"`
	To          *string `json:"to"`
	SplitAt     string  `json:"split_at"`
	EmbargoDays int     `json:"embargo_days"`
}

// UnmarshalJSON decodes strictly and validates, so the dispatcher's decode of a bad call is an
// invalid_argument without a check of its own.
func (r *Request) UnmarshalJSON(b []byte) error {
	type plain Request
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode((*plain)(r)); err != nil {
		return err
	}
	return r.Validate()
}

// Validate reports an argument the tool does not accept. A split needs the period spelled out
// (from and to), so the dataset a split_at names never depends on today's date.
func (r Request) Validate() error {
	if r.SplitAt == "" {
		if r.EmbargoDays != 0 {
			return fmt.Errorf("exportdiag: embargo_days needs split_at")
		}
		return nil
	}
	if r.From == nil || r.To == nil {
		return fmt.Errorf("exportdiag: split_at needs both from and to")
	}
	from, err := time.ParseInLocation(time.DateOnly, *r.From, query.JST)
	if err != nil {
		return fmt.Errorf("exportdiag: from must be a date YYYY-MM-DD")
	}
	to, err := time.ParseInLocation(time.DateOnly, *r.To, query.JST)
	if err != nil {
		return fmt.Errorf("exportdiag: to must be a date YYYY-MM-DD")
	}
	if err := datasetexport.CheckSplit(from, to, r.SplitAt, r.EmbargoDays); err != nil {
		return fmt.Errorf("exportdiag: %w", err)
	}
	return nil
}

// Run exports the trader decisions of [r.From, r.To) and returns the response (the manifest, never a
// row) and the number of decisions read.
func Run(ctx context.Context, q query.Q, r query.Range, now time.Time, req Request) (any, int, error) {
	resp, n, err := datasetexport.Run(ctx, q, datasetexport.Params{
		From: r.From, To: r.To, SplitAt: req.SplitAt, EmbargoDays: req.EmbargoDays, Now: now,
	})
	if err != nil {
		return nil, 0, err
	}
	return resp, n, nil
}
