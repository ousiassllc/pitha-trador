// Package virtualpapertool is the virtual Paper tools of the Paper results MCP
// (issue #1087), declared with tooldef.
package virtualpapertool

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	repo "github.com/ousiassllc/pitha-trador/internal/repository/virtualpaper"
	"github.com/ousiassllc/pitha-trador/internal/service/backtest/variant"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/mcpset"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/mcpset/researchwrite"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/tooldef"
	"github.com/ousiassllc/pitha-trador/internal/service/virtualpaper"
)

// The virtual Paper tools (issue #1087; docs/api/endpoints/paper-results-mcp-virtual-paper.md):
// virtual_paper_profile_create (also the clone) and virtual_paper_profile_stop
// write one virtual_paper_profiles row each (mcpset.Writer, behind the write
// switch); virtual_paper_compare only reads. The virtual trades are recorded by
// the application's daily maintenance task, never by a tool, and none of this
// changes a setting, an order, a position or the capital.

// virtualPaperParamsSchema is variant.Params as a JSON schema (all optional).
func virtualPaperParamsSchema() map[string]any {
	num := func(desc string) map[string]any { return map[string]any{"type": "number", "description": desc} }
	str := func(desc string, enum ...string) map[string]any {
		s := map[string]any{"type": "string", "description": desc}
		if len(enum) > 0 {
			s["enum"] = enum
		}
		return s
	}
	quality := []string{"poor", "fair", "good", "strong", "exceptional"}
	s := tooldef.ObjectSchema(map[string]any{
		"entry_rule":                         str("エントリーの規則（meta = 本体と同じ / legacy_confidence = Jev の確信度）", variant.EntryRuleMeta, variant.EntryRuleLegacyConfidence),
		"min_ev_bps":                         num("meta 規則の最低期待値（bps。0〜1000）"),
		"min_entry_quality_long":             str("LONG の最低エントリー品質", quality...),
		"min_entry_quality_short":            str("SHORT の最低エントリー品質", quality...),
		"min_probability_long":               num("legacy_confidence の LONG の最低確率（0 より大きく 1 以下）"),
		"min_probability_short":              num("legacy_confidence の SHORT の最低確率（0 より大きく 1 以下）"),
		"min_continuation_probability_long":  num("LONG の最低継続確率（0〜1）"),
		"min_continuation_probability_short": num("SHORT の最低継続確率（0〜1）"),
		"stop_loss_pct":                      num("損切り（%。0 より大きく 100 以下）"),
		"take_profit_pct":                    num("利確（%。0 より大きく 100 以下）"),
		"trailing_stop_pct":                  num("トレーリングストップ（%。0 より大きく 100 以下）"),
		"max_holding_minutes":                map[string]any{"type": "integer", "description": "最大保有時間（分。0〜1440）"},
		"slippage_bps":                       num("スリッページ（bps。0〜200）"),
		"fee_bps":                            num("手数料（bps。0〜200）"),
		"max_trade_loss_pct":                 num("1 取引の最大損失（資金の %。0.1〜3）"),
		"max_position_per_symbol_pct":        num("1 銘柄あたりの最大ポジション（%。0 より大きく 100 以下）"),
	})
	s["description"] = "本物の Paper との差分だけを入れる。省略した項目は本物の Paper と同じ（その日の設定）"
	return s
}

// Defs are virtual_paper_profile_create, virtual_paper_profile_stop and virtual_paper_compare.
func Defs() []tooldef.Def {
	return []tooldef.Def{
		{
			Name: "virtual_paper_profile_create",
			Description: "「仮想 Paper」のプロファイルを 1 件作る（記録専用。複製も兼ねる）。プロファイルは、本物の Paper と同じ判断（jev_decisions）・同じ相場（market_snapshots）に対して、方針・出口の設定の差分 params だけを変えたら何が起きたかを記録する。" +
				"記録は本体の日次の保守タスクが、終わった JST の日ごとに再生して書く（16:00 JST 以降。結果は取引日の翌日までに出る。作成した日は途中なので入れず、翌日から）。基準になる設定は、その日を処理した時点で本体が使っている設定（Paper の最低エントリー品質を含む）。" +
				"約定は本体の約定モデル（fillmodel）の仮定。paper_orders・positions・資金・設定には書かず、取引・Risk・発注にも使われない。" +
				"name は 1〜40 文字で、停止したものも含めて重複不可。clone_from に profile_id を渡すと、そのプロファイルの params を写してから params で上書きする。" +
				"動かせるのは同時に 5 件、合計 20 件（停止したものを含む）まで（超えると limit_exceeded）。params に範囲外の値があると invalid_value（detail に項目名）。書き込み許可がオフのときは write_disabled",
			Schema: tooldef.ObjectSchema(map[string]any{
				"name":       map[string]any{"type": "string", "minLength": 1, "maxLength": virtualpaper.MaxNameRunes, "description": "プロファイルの名前（重複不可）"},
				"params":     virtualPaperParamsSchema(),
				"clone_from": map[string]any{"type": "integer", "minimum": 1, "description": "params の元にするプロファイルの profile_id"},
			}, "name"),
			Writes:   true,
			NoPeriod: true,
			Decode: func(raw json.RawMessage) (tooldef.PeriodArgs, tooldef.Run, *tooldef.Failure) {
				var a struct {
					Name      *string        `json:"name"`
					Params    variant.Params `json:"params"`
					CloneFrom *int64         `json:"clone_from"`
				}
				if err := tooldef.StrictDecode(raw, &a); err != nil {
					return tooldef.PeriodArgs{}, nil, err
				}
				if a.Name == nil || (a.CloneFrom != nil && *a.CloneFrom < 1) {
					return tooldef.PeriodArgs{}, nil, tooldef.Fail(tooldef.CodeInvalidArgument)
				}
				return tooldef.PeriodArgs{}, func(ctx context.Context, e tooldef.Env) (any, int, error) {
					w, err := e.Writer()
					if err != nil {
						return nil, 0, err
					}
					p, err := researchwrite.CreateVirtualPaperProfile(ctx, w, e.Client, virtualpaper.CreateArgs{Name: *a.Name, Params: a.Params, CloneFrom: a.CloneFrom})
					if err != nil {
						return nil, 0, virtualPaperFailureOf(err)
					}
					v, err := virtualpaper.ViewOf(p)
					return map[string]any{"profile": v}, 1, err
				}, nil
			},
		},
		{
			Name: "virtual_paper_profile_stop",
			Description: "仮想 Paper のプロファイルを止める（以後の日を記録しない。記録済みの取引・日はそのまま残り、比較にも使える。止めたあとは動かせる件数（5）に数えない）。" +
				"止めた時点で未処理の日（終わっていても）は記録されない。すでに止まっているプロファイルを止めても失敗にはせず、応答の stopped が false（already_stopped が true）になる。" +
				"存在しない profile_id は invalid_argument。書き込み許可がオフのときは write_disabled",
			Schema: tooldef.ObjectSchema(map[string]any{
				"profile_id": map[string]any{"type": "integer", "minimum": 1},
			}, "profile_id"),
			Writes:   true,
			NoPeriod: true,
			Decode: func(raw json.RawMessage) (tooldef.PeriodArgs, tooldef.Run, *tooldef.Failure) {
				var a struct {
					ProfileID *int64 `json:"profile_id"`
				}
				if err := tooldef.StrictDecode(raw, &a); err != nil {
					return tooldef.PeriodArgs{}, nil, err
				}
				if a.ProfileID == nil || *a.ProfileID < 1 {
					return tooldef.PeriodArgs{}, nil, tooldef.Fail(tooldef.CodeInvalidArgument)
				}
				id := *a.ProfileID
				return tooldef.PeriodArgs{}, func(ctx context.Context, e tooldef.Env) (any, int, error) {
					w, err := e.Writer()
					if err != nil {
						return nil, 0, err
					}
					p, changed, err := researchwrite.StopVirtualPaperProfile(ctx, w, id)
					if err != nil {
						return nil, 0, virtualPaperFailureOf(err)
					}
					v, err := virtualpaper.ViewOf(p)
					return map[string]any{"profile": v, "stopped": changed, "already_stopped": !changed}, 1, err
				}, nil
			},
		},
		{
			Name: "virtual_paper_compare",
			Description: "仮想 Paper のプロファイルを、同じ期間・同じ判断の範囲（全員の記録が終わった日だけ）で比べる（読み取り専用）。" +
				"from / to は JST の日付（to は含まない。省略は直近 30 日）。profile_ids は 2〜5 件（先頭が比較の基準。省略は期間にデータのあるプロファイルすべて。6 件以上あると limit_exceeded）。データが無いと profiles は空配列。" +
				"days は比べた日（全員が記録済みの日）、excluded_days は誰かの記録が無くて外した日（missing_profile_ids）。" +
				"プロファイルごとに、取引件数 trade_count・勝率 win_rate・1 取引あたりの純損益（コスト控除後。%）の平均 expectancy_pct とその 95% 信頼区間（日ごとのまとまりで計算。日数が 2 日未満だと区間なし）・円建ての損益の合計 total_pnl_jpy・最大ドローダウン max_drawdown_pct（取引を決済順に並べた累積純損益の山からの最大の落ち込み）を返す。" +
				"vs_first は、先頭のプロファイルとの日ごとの純損益（%の合計）の差の平均と、その日単位の 95% 信頼区間。データ終了で閉じた取引（unfinished）は指標に入れない。" +
				"記録専用の研究データで、標本が小さい間・何通りも比べた中の 1 つは偶然で良く見える（caution）。設定・取引には何も反映しない。期間の取引が 1 プロファイル 20,000 件を超えると limit_exceeded",
			Schema: tooldef.PeriodSchema(map[string]any{
				"profile_ids": map[string]any{"type": "array", "items": map[string]any{"type": "integer", "minimum": 1}, "minItems": 2, "maxItems": virtualpaper.MaxCompareProfiles,
					"description": "比べるプロファイルの profile_id（2〜5 件。先頭が基準）。省略は期間にデータのあるすべて"},
			}),
			Decode: func(raw json.RawMessage) (tooldef.PeriodArgs, tooldef.Run, *tooldef.Failure) {
				var a struct {
					From       *string `json:"from"`
					To         *string `json:"to"`
					ProfileIDs []int64 `json:"profile_ids"`
				}
				if err := tooldef.StrictDecode(raw, &a); err != nil {
					return tooldef.PeriodArgs{}, nil, err
				}
				for _, id := range a.ProfileIDs {
					if id < 1 {
						return tooldef.PeriodArgs{}, nil, tooldef.Fail(tooldef.CodeInvalidArgument)
					}
				}
				return tooldef.PeriodArgs{From: a.From, To: a.To}, func(ctx context.Context, e tooldef.Env) (any, int, error) {
					from, to := e.Range.From.In(query.JST).Format(time.DateOnly), e.Range.To.In(query.JST).Format(time.DateOnly)
					c, err := virtualpaper.Compare(ctx, e.Q, from, to, a.ProfileIDs)
					if err != nil {
						return nil, 0, virtualPaperFailureOf(err)
					}
					return c, len(c.Profiles), nil
				}, nil
			},
		},
	}
}

// virtualPaperFailureOf maps the virtual Paper errors to the tool error codes:
// a refused value is invalid_value (its detail names the argument and the rule,
// never a stored value), an unknown id or a repeated name invalid_argument /
// invalid_value, a cap limit_exceeded. Anything else is returned as is.
func virtualPaperFailureOf(err error) error {
	var (
		vi *variant.InvalidError
		ie *virtualpaper.InvalidError
		le *virtualpaper.LimitError
	)
	switch {
	case errors.As(err, &vi):
		return tooldef.FailDetail(tooldef.CodeInvalidValue, vi.Field+": "+vi.Reason)
	case errors.As(err, &ie):
		code := tooldef.CodeInvalidArgument
		if ie.Field == "name" {
			code = tooldef.CodeInvalidValue
		}
		return tooldef.FailDetail(code, ie.Field+": "+ie.Reason)
	case errors.As(err, &le):
		return tooldef.FailDetail(tooldef.CodeLimitExceeded, le.Reason)
	case errors.Is(err, virtualpaper.ErrTooManyTrades):
		return tooldef.FailDetail(tooldef.CodeLimitExceeded, "期間の取引が 1 プロファイルあたり 20,000 件を超えています。期間を狭めてください")
	case errors.Is(err, repo.ErrNameTaken):
		return tooldef.FailDetail(tooldef.CodeInvalidValue, "name: 同じ名前のプロファイルがあります（停止したものも含めて重複できません）")
	case errors.Is(err, repo.ErrNotFound):
		return tooldef.FailDetail(tooldef.CodeInvalidArgument, "profile_id: その profile_id のプロファイルはありません")
	case errors.Is(err, mcpset.ErrWriteDisabled):
		return tooldef.Fail(tooldef.CodeWriteDisabled)
	}
	return err
}
