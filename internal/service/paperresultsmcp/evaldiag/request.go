package evaldiag

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/ousiassllc/pitha-trador/internal/config/barriersettings"
	"github.com/ousiassllc/pitha-trador/internal/config/jevshadowsettings"
)

// Description is the tool's description (docs/api/endpoints/paper-results-mcp-jev-evaluation.md §3.33).
const Description = "Jev（AI）の LONG/SHORT 判断を、エントリー判定・Risk を通ったかどうかに関係なく全件、同じ行の単純なベースラインと比べる読み取り専用の集計（研究用。採用基準ではなく、しきい値も当てない）。" +
	"対象は期間内の decision_type=trader かつ direction が LONG/SHORT の全判断（trade_signals・Risk では絞らない）。結果ラベルは既存のコスト控除後の triple barrier（calibration.LabelWindow、net_return_bps）を、" +
	"いまの設定（calibration.barrier_*・cost.*）で全アームに同じ式で付け直す。アームは llm（その判断の方向）・long_leg・short_leg・momentum（判断の足の return_5m の符号）・contrarian（その逆）・" +
	"none（見送り＝0 bps）・random（買いと売りのラベルの平均＝期待値。乱数を使わない）。買い・売り・全体ごとに、件数・日数・平均 net bps（日クラスタのブートストラップ 95% 区間）・勝率（net>0）・日別平均、" +
	"llm から各ベースラインを引いた差（同じ行・日クラスタの区間）、「いまの件数で検出できる最小の差」（alpha=0.05 両側・検出力 0.8）と +3 / +5 bps を判断できるまでにあと何営業日か（SE が 1/√日数で縮む前提）を返す。" +
	"(model_id, question_version) ごとに別々に集計する（版が変わると数え直し）。学習の cutoff（metalabel.llm_cutoff）より前の日は除外し、cutoff が仮置きなら除外せずフラグを立てる。" +
	"同じ行で計算できない判断（約定できる気配なし・窓がホライズンに届かない・足なし）は理由別に除外件数を出す。" +
	"保存済みのラベル（calibration_outcomes の label_version=2 の net_return_bps）の件数と平均、見送り（entered / skipped）の内訳も別に返す。書き込みはしない"

// SchemaProperties are the tool's own arguments besides the period.
func SchemaProperties() map[string]any {
	return map[string]any{
		"horizon_minutes": map[string]any{
			"type": "integer", "minimum": barriersettings.MinTimeoutMinutes, "maximum": barriersettings.MaxTimeoutMinutes,
			"description": "ラベルの保有時間（分）。省略はいまの calibration.barrier_timeout_minutes（保存済みラベルの突き合わせも同じ値）",
		},
		"prompt_version": map[string]any{"type": "string", "maxLength": jevshadowsettings.MaxVersionLen, "description": "この版（question_version。例: trader-v4）の trader 判断だけを数える。省略は全版（版ごとに別集計）"},
		"model_id":       map[string]any{"type": "string", "maxLength": MaxModelIDLen, "description": "この model_id の判断だけを数える。省略は全モデル（モデルごとに別集計）"},
	}
}

// Request is the tool's arguments as decoded from the call: the period (From,
// To, as every period tool takes it) and the tool's own. Decoding rejects an
// unknown field and an argument Validate refuses.
type Request struct {
	From           *string `json:"from"`
	To             *string `json:"to"`
	HorizonMinutes *int    `json:"horizon_minutes"`
	PromptVersion  string  `json:"prompt_version"`
	ModelID        string  `json:"model_id"`
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
	if h := r.HorizonMinutes; h != nil && (*h < barriersettings.MinTimeoutMinutes || *h > barriersettings.MaxTimeoutMinutes) {
		return fmt.Errorf("evaldiag: horizon_minutes must be %d..%d", barriersettings.MinTimeoutMinutes, barriersettings.MaxTimeoutMinutes)
	}
	if r.PromptVersion != "" {
		if err := jevshadowsettings.ValidateVersion(r.PromptVersion); err != nil {
			return fmt.Errorf("evaldiag: prompt_version: %w", err)
		}
	}
	if len(r.ModelID) > MaxModelIDLen {
		return fmt.Errorf("evaldiag: model_id is at most %d characters", MaxModelIDLen)
	}
	return nil
}
