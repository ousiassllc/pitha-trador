package paperresultsmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/mcpset"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/proposal"
)

// The order-proposal tools (issue #827; docs/api/endpoints/paper-results-mcp.md
// §3.16-3.18). They accept and record a Paper order proposal; they never place
// an order. Whether the symbol exists, what it costs and whether Risk lets it
// through are the application's questions (issue #828), not the MCP's.

func init() {
	tools = append(tools, orderProposalTools...)
}

var orderProposalTools = []tool{
	{
		name: "order_proposal_submit",
		description: "Paper の注文案を 1 件出す（受け付けて記録するだけ。発注はアプリが Risk check を通してから行う。Live には使えない）。" +
			"買い（open_long: 新規の買い）か決済（close: 保有中ポジションの決済）だけで、売り建ては出せない。" +
			"open_long は quantity か budget_jpy のどちらか 1 つ・stop_loss_price・exit_plan・max_loss_jpy が必須。" +
			"同じ idempotency_key・同じ内容の再送は新しい案を作らず最初の案を返す（replayed: true）。内容が違えば idempotency_conflict。" +
			"受け付けスイッチ（Settings 画面の「外部連携」）がオフのあいだは proposals_disabled。1 日（JST）に受け付けるのは 20 件まで",
		schema: objectSchema(map[string]any{
			"idempotency_key":   map[string]any{"type": "string", "minLength": proposal.MinKeyLen, "maxLength": proposal.MaxKeyLen, "pattern": "^[A-Za-z0-9_-]+$", "description": "同じ案の再送を見分けるキー"},
			"symbol":            map[string]any{"type": "string", "description": "銘柄コード（英大文字と数字、10 文字以内）"},
			"bucket":            map[string]any{"type": "string", "enum": []string{proposal.BucketStock, proposal.BucketBondETF}, "description": "資産ごとの予算枠"},
			"action":            map[string]any{"type": "string", "enum": []string{proposal.ActionOpenLong, proposal.ActionClose}, "description": "open_long（新規の買い）/ close（保有中ポジションの決済）"},
			"quantity":          map[string]any{"type": "integer", "minimum": 1, "description": "株数（open_long のとき budget_jpy とどちらか 1 つ）"},
			"budget_jpy":        map[string]any{"type": "number", "exclusiveMinimum": 0, "description": "使う金額（円）（open_long のとき quantity とどちらか 1 つ）"},
			"order_type":        map[string]any{"type": "string", "enum": []string{proposal.OrderMarket, proposal.OrderLimit}},
			"limit_price":       map[string]any{"type": "number", "exclusiveMinimum": 0, "description": "指値（LIMIT のとき必須）"},
			"stop_loss_price":   map[string]any{"type": "number", "exclusiveMinimum": 0, "description": "損切りの価格（open_long のとき必須。買いなので指値より下）"},
			"take_profit_price": map[string]any{"type": "number", "exclusiveMinimum": 0, "description": "利確の価格（任意）"},
			"exit_plan":         map[string]any{"type": "string", "maxLength": proposal.MaxExitPlanRunes, "description": "手じまいの計画（open_long のとき必須）"},
			"max_loss_jpy":      map[string]any{"type": "number", "exclusiveMinimum": 0, "description": "見込む最大損失（円）（open_long のとき必須）"},
			"rationale":         map[string]any{"type": "string", "maxLength": proposal.MaxRationaleRunes, "description": "判断の理由"},
			"sources": map[string]any{
				"type": "array", "minItems": proposal.MinSources, "maxItems": proposal.MaxSources,
				"items": objectSchema(map[string]any{
					"title":        map[string]any{"type": "string"},
					"url":          map[string]any{"type": "string"},
					"retrieved_at": map[string]any{"type": "string", "description": "RFC3339"},
				}, "title", "url", "retrieved_at"),
				"description": "情報源",
			},
			"expires_at": map[string]any{"type": "string", "description": "有効期限（RFC3339）。受け付け時点より後で 24 時間以内"},
		}, "idempotency_key", "symbol", "bucket", "action", "order_type", "rationale", "sources", "expires_at"),
		writes:   true,
		noPeriod: true,
		decode: func(raw json.RawMessage) (periodArgs, runFunc, *toolError) {
			var a proposal.Args
			if err := strictDecode(raw, &a); err != nil {
				return periodArgs{}, nil, err
			}
			req, err := a.Validate()
			if err != nil {
				return periodArgs{}, nil, invalidProposal(err)
			}
			return periodArgs{}, func(ctx context.Context, e toolEnv) (any, int, error) {
				if err := req.CheckExpiry(e.now); err != nil {
					return nil, 0, invalidProposal(err)
				}
				w, err := e.writer()
				if err != nil {
					return nil, 0, err
				}
				out, err := w.SubmitProposal(ctx, e.client, req)
				if err != nil {
					return nil, 0, submitFailureOf(err)
				}
				return out, 1, nil
			}, nil
		},
	},
	{
		name:        "order_proposal_get",
		description: "出した注文案 1 件の状態（受け付けた内容・状態と理由コード・アプリが埋めた約定や注文への紐付け・状態の変化の履歴）。proposal_id は order_proposal_submit の応答にある（読み取り専用。受け付けスイッチがオフでも使える）",
		schema: objectSchema(map[string]any{
			"proposal_id": map[string]any{"type": "integer", "minimum": 1},
		}, "proposal_id"),
		noPeriod: true,
		decode: func(raw json.RawMessage) (periodArgs, runFunc, *toolError) {
			var a struct {
				ProposalID *int64 `json:"proposal_id"`
			}
			if err := strictDecode(raw, &a); err != nil {
				return periodArgs{}, nil, err
			}
			if a.ProposalID == nil || *a.ProposalID < 1 {
				return periodArgs{}, nil, fail(CodeInvalidArgument)
			}
			id := *a.ProposalID
			return periodArgs{}, func(ctx context.Context, e toolEnv) (any, int, error) {
				p, err := proposal.Get(ctx, e.q, id)
				if errors.Is(err, proposal.ErrNotFound) {
					return nil, 0, fail(CodeProposalNotFound)
				}
				return p, 1, err
			}, nil
		},
	},
	{
		name: "order_proposal_cancel",
		description: "出した注文案の取消を依頼する（依頼を記録するだけ。実際の取消はアプリが行い、order_proposal_get で結果を確かめる）。" +
			"取り消せるのは received / waiting_session / submitted の案だけで、submitted は待っている指値の注文もアプリが取り消す。" +
			"約定済み・終わった案（filled / closed / rejected / expired / cancelled / failed）と、処理中（processing）の案は not_cancellable。" +
			"同じ案への 2 回目以降の依頼は新しく記録せず最初の依頼を返す（replayed: true）。受け付けスイッチがオフでも使える（リスクを減らす操作のため）",
		schema: objectSchema(map[string]any{
			"proposal_id": map[string]any{"type": "integer", "minimum": 1},
		}, "proposal_id"),
		writes:   true,
		noPeriod: true,
		decode: func(raw json.RawMessage) (periodArgs, runFunc, *toolError) {
			var a struct {
				ProposalID *int64 `json:"proposal_id"`
			}
			if err := strictDecode(raw, &a); err != nil {
				return periodArgs{}, nil, err
			}
			if a.ProposalID == nil || *a.ProposalID < 1 {
				return periodArgs{}, nil, fail(CodeInvalidArgument)
			}
			id := *a.ProposalID
			return periodArgs{}, func(ctx context.Context, e toolEnv) (any, int, error) {
				w, err := e.writer()
				if err != nil {
					return nil, 0, err
				}
				out, err := w.RequestCancel(ctx, id)
				switch {
				case errors.Is(err, proposal.ErrNotFound):
					return nil, 0, fail(CodeProposalNotFound)
				case errors.Is(err, mcpset.ErrNotCancellable):
					return nil, 0, fail(CodeNotCancellable)
				case err != nil:
					return nil, 0, err
				}
				return out, 1, nil
			}, nil
		},
	},
	{
		name: "order_proposals_disable",
		description: "注文案の受け付けをオフにする（order_proposal_submit が proposals_disabled になる。すでに出した案には影響しない）。" +
			"オンに戻せるのは Settings 画面の「外部連携」だけで、MCP からはオンにできない。切替は監査ログに残る",
		schema:   objectSchema(map[string]any{}),
		writes:   true,
		noPeriod: true,
		decode: func(raw json.RawMessage) (periodArgs, runFunc, *toolError) {
			if err := noArgs(raw); err != nil {
				return periodArgs{}, nil, err
			}
			return periodArgs{}, func(ctx context.Context, e toolEnv) (any, int, error) {
				w, err := e.writer()
				if err != nil {
					return nil, 0, err
				}
				entry, err := w.DisableOrderProposals(ctx, e.client)
				if err != nil {
					return nil, 0, err
				}
				return map[string]any{"order_proposals_enabled": false, "audit_id": entry.ID, "at": entry.At}, 1, nil
			}, nil
		},
	},
}

// invalidProposal is invalid_argument carrying the validator's reason (it
// names the argument and the rule, never the value).
func invalidProposal(err error) *toolError {
	reason, ok := proposal.IsInvalid(err)
	if !ok {
		return &toolError{code: CodeInvalidArgument, cause: err}
	}
	return failDetail(CodeInvalidArgument, reason)
}

// submitFailureOf maps a SubmitProposal error to the tool's spec error;
// anything else is left to classify (busy, timeout, unavailable, internal).
func submitFailureOf(err error) error {
	switch {
	case errors.Is(err, mcpset.ErrProposalsDisabled):
		return fail(CodeProposalsDisabled)
	case errors.Is(err, mcpset.ErrProposalLimit):
		return failDetail(CodeLimitExceeded, fmt.Sprintf("注文案は JST の 1 日に %d 件までです", proposal.MaxPerDay))
	case errors.Is(err, mcpset.ErrIdempotencyConflict):
		return fail(CodeIdempotencyConflict)
	}
	return err
}
