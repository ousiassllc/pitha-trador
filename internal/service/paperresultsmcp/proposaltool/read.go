package proposaltool

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/ousiassllc/pitha-trador/internal/domain/orderproposal"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/proposal"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/tooldef"
)

// The order-proposal read tools (issues #827, #829; docs/api/endpoints/
// paper-results-mcp.md §3.17, §3.20, §3.21): what became of the proposals, from
// the proposal through the trade signal and the paper order to the position's
// realized pnl. They read through the read-only connection and write nothing,
// so they work with the intake switch off.

// maxListLimit is the limit of order_proposals_list (API spec §2).
const maxListLimit = 200

func readDefs() []tooldef.Def {
	return []tooldef.Def{
		{
			Name: "order_proposals_list",
			Description: "出した注文案の一覧（受け付けた日 received_at が from〜to の案。新しい順）。状態（status）と枠（bucket）で絞り込める。" +
				"各行は案の概要と、紐づいたポジションが決済済みなら実現損益。理由・手じまいの計画・情報源・状態の履歴・紐づいた注文などの詳細は order_proposal_get（読み取り専用）",
			Schema: tooldef.PeriodSchema(map[string]any{
				"status": map[string]any{"type": "string", "enum": orderproposal.Statuses(), "description": "状態で絞り込む"},
				"bucket": map[string]any{"type": "string", "enum": []string{proposal.BucketStock, proposal.BucketBondETF}, "description": "枠で絞り込む"},
				"limit":  tooldef.LimitProp(maxListLimit),
				"cursor": map[string]any{"type": "string", "description": "前回応答の next_cursor"},
			}),
			Decode: func(raw json.RawMessage) (tooldef.PeriodArgs, tooldef.Run, *tooldef.Failure) {
				var a struct {
					tooldef.PeriodArgs
					Status string `json:"status"`
					Bucket string `json:"bucket"`
					Limit  *int   `json:"limit"`
					Cursor string `json:"cursor"`
				}
				if err := tooldef.StrictDecode(raw, &a); err != nil {
					return a.PeriodArgs, nil, err
				}
				limit, err := tooldef.ResolveLimit(a.Limit, maxListLimit)
				if err != nil {
					return a.PeriodArgs, nil, err
				}
				if (a.Status != "" && !proposal.ValidStatus(a.Status)) ||
					(a.Bucket != "" && a.Bucket != proposal.BucketStock && a.Bucket != proposal.BucketBondETF) {
					return a.PeriodArgs, nil, tooldef.Fail(tooldef.CodeInvalidArgument)
				}
				return a.PeriodArgs, func(ctx context.Context, e tooldef.Env) (any, int, error) {
					items, next, err := proposal.List(ctx, e.Q, proposal.ListFilter{
						Range: e.Range, Status: a.Status, Bucket: a.Bucket, Limit: limit, Cursor: a.Cursor,
					})
					var nextCursor *string
					if next != "" {
						nextCursor = &next
					}
					return map[string]any{"proposals": items, "next_cursor": nextCursor}, len(items), err
				}, nil
			},
		},
		{
			Name: "order_proposal_get",
			Description: "出した注文案 1 件の全体: 受け付けた内容（理由・損切りと手じまいの計画・最大損失・情報源）、状態と理由コード、状態の変化の履歴（補足つき）、" +
				"紐づいたトレードシグナル・注文・ポジション（result: 約定価格・数量・手数料・決済理由・実現損益・保有時間）、" +
				"案の損切り／利確に決済の約定が届いたか、最大損失（あいりの見込み・アプリの見積もり・実際の損失）。" +
				"proposal_id は order_proposal_submit の応答か order_proposals_list にある（読み取り専用。受け付けスイッチがオフでも使える）",
			Schema: tooldef.ObjectSchema(map[string]any{
				"proposal_id": map[string]any{"type": "integer", "minimum": 1},
			}, "proposal_id"),
			NoPeriod: true,
			Decode: func(raw json.RawMessage) (tooldef.PeriodArgs, tooldef.Run, *tooldef.Failure) {
				var a struct {
					ProposalID *int64 `json:"proposal_id"`
				}
				if err := tooldef.StrictDecode(raw, &a); err != nil {
					return tooldef.PeriodArgs{}, nil, err
				}
				if a.ProposalID == nil || *a.ProposalID < 1 {
					return tooldef.PeriodArgs{}, nil, tooldef.Fail(tooldef.CodeInvalidArgument)
				}
				id := *a.ProposalID
				return tooldef.PeriodArgs{}, func(ctx context.Context, e tooldef.Env) (any, int, error) {
					p, err := proposal.Get(ctx, e.Q, id)
					if errors.Is(err, proposal.ErrNotFound) {
						return nil, 0, tooldef.Fail(tooldef.CodeProposalNotFound)
					}
					return p, 1, err
				}, nil
			},
		},
		{
			Name: "order_proposal_summary",
			Description: "期間内（受け付けた日が from〜to）の注文案の件数（状態別・拒否理由別・枠別）と、約定した買い案（open_long）の" +
				"決済済み件数・勝率・期待値・合計損益（positions.realized_pnl の集計。読み取り専用）",
			Schema: tooldef.PeriodSchema(nil),
			Decode: func(raw json.RawMessage) (tooldef.PeriodArgs, tooldef.Run, *tooldef.Failure) {
				var a tooldef.PeriodArgs
				if err := tooldef.StrictDecode(raw, &a); err != nil {
					return a, nil, err
				}
				return a, func(ctx context.Context, e tooldef.Env) (any, int, error) {
					s, err := proposal.Summarize(ctx, e.Q, e.Range)
					return s, s.TotalCount, err
				}, nil
			},
		},
	}
}
