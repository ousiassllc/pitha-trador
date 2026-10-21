package paperresultsmcp

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/ousiassllc/pitha-trador/internal/config/jevshadowsettings"
	"github.com/ousiassllc/pitha-trador/internal/service/jev"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/mcpset"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query/shadowquery"
)

// The Jev prompt tools (issue #872; docs/architecture/paper-results-mcp.md §5,
// §7.3, docs/api/endpoints/paper-results-mcp.md): two reads of the shadow
// comparison and its cost, and the one explicit write that promotes a
// candidate version to live. No tool turns the shadow on, picks the candidate or
// sets the cost cap: those are the Settings screen's (the cost cap is
// 暫定の案・未承認 until a person decides it).

func init() {
	tools = append(tools, jevShadowTools...)
}

var jevShadowTools = []tool{
	{
		name: "jev_shadow_compare",
		description: "Jev の trader のライブの判断と、同じ入力に対するシャドー（候補のプロンプトの版）の判断を、期間・版を指定して並べる。" +
			"候補の版ごとに、entry_quality の分布（poor〜exceptional の件数と割合）・direction の一致率・シャドーの費用・calibration_outcomes による結果（entry_quality 別の future_return・的中率・barrier_hit・net_return_bps）をライブとシャドーで返す。" +
			"結果はライブの方向に対するラベルなので、方向がライブと食い違う行は採点しない（excluded_disagreement。outcome_note と docs/architecture/paper-results-mcp-jev-prompt.md §5.1）。シャドーは取引に使われない（読み取り専用）",
		schema: periodSchema(map[string]any{
			"prompt_version": map[string]any{"type": "string", "maxLength": jevshadowsettings.MaxVersionLen, "description": "候補の版（例: trader-v4）。省略は存在するすべての版"},
		}),
		decode: func(raw json.RawMessage) (periodArgs, runFunc, *toolError) {
			var a struct {
				periodArgs
				PromptVersion string `json:"prompt_version"`
			}
			if err := strictDecode(raw, &a); err != nil {
				return a.periodArgs, nil, err
			}
			if a.PromptVersion != "" && jevshadowsettings.ValidateVersion(a.PromptVersion) != nil {
				return a.periodArgs, nil, fail(CodeInvalidArgument)
			}
			return a.periodArgs, func(ctx context.Context, e toolEnv) (any, int, error) {
				c, err := shadowquery.Compare(ctx, e.q, e.r, a.PromptVersion)
				return c, len(c.Versions), err
			}, nil
		},
	},
	{
		name: "jev_shadow_cost",
		description: "シャドーの費用と状態: request_cost の合計（今日（JST）・累計・日別）、1 日の費用上限と 1 日の失敗回数の上限（どちらも暫定の案・未承認。cap_source が proposed_unapproved のあいだは保存された値ではない）と、止まった理由（halt_today / last_halt。daily_cost_cap か daily_failure_cap）、" +
			"シャドーの有効・無効と候補の版、ライブの版、直近の昇格の記録（誰が・いつ・前の版・後の版）（読み取り専用）",
		schema:   objectSchema(map[string]any{}),
		noPeriod: true,
		decode: func(raw json.RawMessage) (periodArgs, runFunc, *toolError) {
			if err := noArgs(raw); err != nil {
				return periodArgs{}, nil, err
			}
			return periodArgs{}, func(ctx context.Context, e toolEnv) (any, int, error) {
				s, err := shadowquery.Cost(ctx, e.q, e.now, jev.TraderQuestionVersion, jev.TraderVersions())
				return s, len(s.Daily), err
			}, nil
		},
	},
	{
		name: "jev_prompt_promote",
		description: "ライブの Jev の trader のプロンプトの版を、指定した版に切り替える（昇格）。以後のライブの判断（取引の入力）が新しい版になる明示的な操作で、自動では行われない。" +
			"書き込み許可がオフのときは拒否（settings_write_enable と同じスイッチ）。この版・ライブの版はビルドに登録された版だけで、ライブの版と同じ版は invalid_value（detail に理由）。" +
			"昇格は誰が（MCP の接続元ホスト）・いつ・前の版・後の版を jev_prompt_promotions に残し、jev_shadow_cost で見られる。自己改善ループ（Sol/Opus）からは使えない",
		schema: objectSchema(map[string]any{
			"version": map[string]any{"type": "string", "maxLength": jevshadowsettings.MaxVersionLen, "description": "ライブにする版（jev_shadow_cost の registered_versions の 1 つ）"},
		}, "version"),
		writes:   true,
		noPeriod: true,
		decode: func(raw json.RawMessage) (periodArgs, runFunc, *toolError) {
			var a struct {
				Version string `json:"version"`
			}
			if err := strictDecode(raw, &a); err != nil {
				return periodArgs{}, nil, err
			}
			if jevshadowsettings.ValidateVersion(a.Version) != nil {
				return periodArgs{}, nil, fail(CodeInvalidArgument)
			}
			return periodArgs{}, func(ctx context.Context, e toolEnv) (any, int, error) {
				w, err := e.writer()
				if err != nil {
					return nil, 0, err
				}
				p, err := w.PromotePrompt(ctx, e.client, a.Version)
				switch {
				case errors.Is(err, mcpset.ErrWriteDisabled):
					return nil, 0, fail(CodeWriteDisabled)
				case errors.Is(err, mcpset.ErrPromoteUnknownVersion):
					return nil, 0, failDetail(CodeInvalidValue, "この版はビルドに登録されていません: "+a.Version)
				case errors.Is(err, mcpset.ErrPromoteAlreadyLive):
					return nil, 0, failDetail(CodeInvalidValue, "すでにライブの版です: "+a.Version)
				case err != nil:
					return nil, 0, err
				}
				return map[string]any{
					"promotion_id": p.ID, "from_version": p.FromVersion, "to_version": p.ToVersion, "at": p.At,
				}, 1, nil
			}, nil
		},
	},
}
