package disclosureskipdiag

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/ousiassllc/pitha-trador/internal/config/jevshadowsettings"
	"github.com/ousiassllc/pitha-trador/internal/service/jev"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query/shadowquery"
)

// Description is the tool's description (docs/api/endpoints/paper-results-mcp-disclosure-skip.md §3.31).
const Description = "適時開示による見送りルールの記録の集計（記録だけで、取引・Policy・Risk・発注には使っていない。読み取り専用）: 期間内の Jev のエントリー候補" +
	"（ライブの trader 判断のうち LONG / SHORT）について、ルール（判断時刻以前の、判断日の当日か直前の東証営業日 15:00 以降の決算短信・業績予想の修正）に" +
	"「該当（skip）」「非該当（no_skip）」「不明（unknown。やのしんの取得の失敗・欠け・件数上限で窓を覆えず、該当も見つからなかった）」" +
	"それぞれの件数と、結果ラベル（label_version=2 の net_return_bps、コスト控除後）の件数・勝率（net_return_bps > 0）・平均・合計。" +
	"まだ記録がない判断は not_recorded に数える。集計はライブの版と同じコホートの行だけで、prompt_version を渡すとその版だけ。" +
	"開示の本文・URL・判断の応答は返さない"

// SchemaProperties are the tool's own arguments besides the period.
func SchemaProperties() map[string]any {
	return map[string]any{
		"prompt_version": map[string]any{"type": "string", "maxLength": jevshadowsettings.MaxVersionLen, "description": "この版の trader 判断だけを数える（例: trader-v4）。省略はライブの版と同じコホート"},
	}
}

// Request is the tool's arguments as decoded from the call: the period (From,
// To, as every period tool takes it) and the tool's own. Decoding rejects an
// unknown field and an argument Validate refuses.
type Request struct {
	From          *string `json:"from"`
	To            *string `json:"to"`
	PromptVersion string  `json:"prompt_version"`
}

// UnmarshalJSON decodes strictly and validates.
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
	if r.PromptVersion != "" {
		if err := jevshadowsettings.ValidateVersion(r.PromptVersion); err != nil {
			return fmt.Errorf("disclosureskipdiag: prompt_version: %w", err)
		}
	}
	return nil
}

// Run resolves the prompt cohort of req (the live version's unless one version
// is asked for) and summarizes [r.From, r.To).
func Run(ctx context.Context, q query.Q, r query.Range, req Request) (any, int, error) {
	a := Args{PromptVersion: req.PromptVersion}
	if req.PromptVersion == "" {
		foreign, err := jev.ForeignVersionsOfLive(ctx, shadowquery.SettingsStore(q))
		if err != nil {
			return nil, 0, err
		}
		a.Foreign = foreign
	}
	d, err := Analyze(ctx, q, r, a)
	return d, d.Population.Recorded, err
}
