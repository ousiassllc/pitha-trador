package entryqualitydiag

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/config/jevshadowsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/jev"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query/shadowquery"
)

// Description is the tool's description (docs/api/endpoints/paper-results-mcp-entry-quality.md §3.30).
const Description = "min_entry_quality を仮に変えたら見送りがどう変わるかの集計（設定は変えない。読み取り専用）: LONG/SHORT の trader 判断の entry_quality 分布（プロンプトの版別）、" +
	"エントリー判定の手前で落ちた件数（理由別）・VWAP 逆張りで判定対象外の件数・貸借なしの SHORT の件数、判定に届いた判断について min_entry_quality ごとの" +
	"「entry_quality で落ちる件数」「通過して継続確率・toxic_flow・流動性ストレスで落ちる件数」「全部通る件数」（買い・売り別を含む）。" +
	"判定は Policy Engine と同じ関数（policy.ThresholdReject）で、継続確率・toxic_flow・流動性ストレスのしきい値は今の policy.*（strategy.yaml と runtime_settings の上書き）。" +
	"メタモデルの判定と確率しきい値は含まない。集計はライブの版と同じコホートの行だけで、prompt_version を渡すとその版だけ"

// SchemaProperties are the tool's own arguments besides the period.
func SchemaProperties() map[string]any {
	return map[string]any{
		"min_entry_quality": map[string]any{
			"type": "array", "maxItems": MaxScenarios, "uniqueItems": true,
			"items": map[string]any{"type": "string", "enum": []string{
				domain.JevEntryQualityPoor, domain.JevEntryQualityFair, domain.JevEntryQualityGood, domain.JevEntryQualityStrong, domain.JevEntryQualityExceptional,
			}},
			"description": "試す最低エントリー品質（買い・売りに同じ値）。省略は fair / good / strong",
		},
		"prompt_version": map[string]any{"type": "string", "maxLength": jevshadowsettings.MaxVersionLen, "description": "この版の trader 判断だけを数える（例: trader-v4）。省略はライブの版と同じコホート"},
	}
}

// Request is the tool's arguments as decoded from the call: the period (From,
// To, as every period tool takes it) and the tool's own. Decoding rejects an
// unknown field and an argument Validate refuses.
type Request struct {
	From            *string  `json:"from"`
	To              *string  `json:"to"`
	MinEntryQuality []string `json:"min_entry_quality"`
	PromptVersion   string   `json:"prompt_version"`
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
	if _, err := scenarioLevels(r.MinEntryQuality); err != nil {
		return err
	}
	if r.PromptVersion != "" {
		if err := jevshadowsettings.ValidateVersion(r.PromptVersion); err != nil {
			return fmt.Errorf("entryqualitydiag: prompt_version: %w", err)
		}
	}
	return nil
}

// Run resolves the prompt cohort of req (the live version's unless one version
// is asked for) and analyzes [r.From, r.To) against baseline, strategy.yaml's
// policy section (nil: the build's default).
func Run(ctx context.Context, q query.Q, r query.Range, req Request, baseline *config.PolicyConfig) (any, int, error) {
	a := Args{MinEntryQuality: req.MinEntryQuality, PromptVersion: req.PromptVersion, Baseline: baseline}
	if req.PromptVersion == "" {
		foreign, err := jev.ForeignVersionsOfLive(ctx, shadowquery.SettingsStore(q))
		if err != nil {
			return nil, 0, err
		}
		a.Foreign = foreign
	}
	d, err := Analyze(ctx, q, r, a)
	return d, d.Population.LongShort, err
}
