package paperresultsmcp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// Listing limits (API spec §2).
const (
	defaultListLimit = 50
	maxListLimit     = 200
	maxMetalabelList = 50
)

// toolEnv is what a tool needs besides its arguments.
type toolEnv struct {
	q          query.Q
	r          query.Range
	capitalJPY float64
}

// tool is one entry of the dispatch table. The table is the complete set of
// capabilities: there is no other way for a request to reach SQL, and every
// entry is a read.
type tool struct {
	name        string
	description string
	schema      map[string]any
	// decode parses the arguments (rejecting unknown ones) and returns the
	// period arguments plus a run function over the resolved period.
	decode func(raw json.RawMessage) (periodArgs, runFunc, *toolError)
}

// runFunc executes the tool's SQL and returns the data and the row count to
// log.
type runFunc func(ctx context.Context, env toolEnv) (data any, count int, err error)

// strictDecode unmarshals raw into dst, rejecting unknown fields, wrong
// types and trailing data. Empty raw is an empty object.
func strictDecode(raw json.RawMessage, dst any) *toolError {
	if len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		raw = json.RawMessage("{}")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return &toolError{CodeInvalidArgument, err}
	}
	if _, err := dec.Token(); err != io.EOF {
		return fail(CodeInvalidArgument)
	}
	return nil
}

// resolveLimit applies the default and the tool maximum. A limit above the
// maximum is limit_exceeded; below 1 is an invalid argument.
func resolveLimit(requested *int, maxLimit int) (int, *toolError) {
	if requested == nil {
		return min(defaultListLimit, maxLimit), nil
	}
	switch {
	case *requested < 1:
		return 0, fail(CodeInvalidArgument)
	case *requested > maxLimit:
		return 0, fail(CodeLimitExceeded)
	}
	return *requested, nil
}

func periodSchema(extra map[string]any) map[string]any {
	props := map[string]any{
		"from": map[string]any{"type": "string", "description": "JST の開始日 YYYY-MM-DD（含む）。to と両方指定するか、両方省略（直近 30 日）"},
		"to":   map[string]any{"type": "string", "description": "JST の終了日 YYYY-MM-DD（含まない）。期間は最大 366 日"},
	}
	for k, v := range extra {
		props[k] = v
	}
	return map[string]any{"type": "object", "properties": props, "additionalProperties": false}
}

func limitProp(maxLimit int) map[string]any {
	return map[string]any{"type": "integer", "minimum": 1, "maximum": maxLimit, "default": min(defaultListLimit, maxLimit)}
}

// tools is the dispatch table: exactly the eight tools of the API spec §3.
var tools = []tool{
	{
		name:        "paper_performance_summary",
		description: "期間内にクローズした Paper 取引の件数・勝率・期待値・ドローダウン（読み取り専用）",
		schema:      periodSchema(nil),
		decode: func(raw json.RawMessage) (periodArgs, runFunc, *toolError) {
			var a periodArgs
			if err := strictDecode(raw, &a); err != nil {
				return a, nil, err
			}
			return a, func(ctx context.Context, e toolEnv) (any, int, error) {
				p, n, err := query.PerformanceSummary(ctx, e.q, e.r, e.capitalJPY)
				return p, n, err
			}, nil
		},
	},
	{
		name:        "paper_daily_pnl",
		description: "JST の決済日ごとの Paper 損益と件数（読み取り専用）",
		schema:      periodSchema(nil),
		decode: func(raw json.RawMessage) (periodArgs, runFunc, *toolError) {
			var a periodArgs
			if err := strictDecode(raw, &a); err != nil {
				return a, nil, err
			}
			return a, func(ctx context.Context, e toolEnv) (any, int, error) {
				days, err := query.DailyPnL(ctx, e.q, e.r)
				return map[string]any{"days": days}, len(days), err
			}, nil
		},
	},
	{
		name:        "paper_trades",
		description: "クローズ済み Paper 取引の一覧（ホワイトリスト列のみ。新しい順）",
		schema: periodSchema(map[string]any{
			"limit":  limitProp(maxListLimit),
			"cursor": map[string]any{"type": "string", "description": "前回応答の next_cursor"},
		}),
		decode: func(raw json.RawMessage) (periodArgs, runFunc, *toolError) {
			var a struct {
				periodArgs
				Limit  *int   `json:"limit"`
				Cursor string `json:"cursor"`
			}
			if err := strictDecode(raw, &a); err != nil {
				return a.periodArgs, nil, err
			}
			limit, err := resolveLimit(a.Limit, maxListLimit)
			if err != nil {
				return a.periodArgs, nil, err
			}
			return a.periodArgs, func(ctx context.Context, e toolEnv) (any, int, error) {
				trades, next, err := query.Trades(ctx, e.q, e.r, limit, a.Cursor)
				var nextCursor *string
				if next != "" {
					nextCursor = &next
				}
				return map[string]any{"trades": trades, "next_cursor": nextCursor}, len(trades), err
			}, nil
		},
	},
	{
		name:        "jev_calibration",
		description: "Jev 判断の件数と、ラベル版・horizon 別の的中率・Brier・信頼度帯（読み取り専用）",
		schema: periodSchema(map[string]any{
			"horizon_minutes": map[string]any{"type": "integer", "minimum": 1},
		}),
		decode: func(raw json.RawMessage) (periodArgs, runFunc, *toolError) {
			var a struct {
				periodArgs
				HorizonMinutes *int `json:"horizon_minutes"`
			}
			if err := strictDecode(raw, &a); err != nil {
				return a.periodArgs, nil, err
			}
			horizon := 0
			if a.HorizonMinutes != nil {
				if *a.HorizonMinutes < 1 {
					return a.periodArgs, nil, fail(CodeInvalidArgument)
				}
				horizon = *a.HorizonMinutes
			}
			return a.periodArgs, func(ctx context.Context, e toolEnv) (any, int, error) {
				c, err := query.JevCalibration(ctx, e.q, e.r, horizon)
				return c, len(c.Horizons), err
			}, nil
		},
	},
	{
		name:        "metalabel_history",
		description: "メタモデルの採用/却下履歴と OOS AUC/EV、予測の take/skip 件数、データ不足による再学習の見送り（固定コードと必要件数中の件数。読み取り専用）",
		schema:      periodSchema(map[string]any{"limit": limitProp(maxMetalabelList)}),
		decode: func(raw json.RawMessage) (periodArgs, runFunc, *toolError) {
			var a struct {
				periodArgs
				Limit *int `json:"limit"`
			}
			if err := strictDecode(raw, &a); err != nil {
				return a.periodArgs, nil, err
			}
			limit, err := resolveLimit(a.Limit, maxMetalabelList)
			if err != nil {
				return a.periodArgs, nil, err
			}
			return a.periodArgs, func(ctx context.Context, e toolEnv) (any, int, error) {
				h, err := query.Metalabel(ctx, e.q, e.r, limit)
				return h, len(h.Models), err
			}, nil
		},
	},
	{
		name:        "risk_rejections",
		description: "Risk Engine 拒否の理由コード別件数（理由文は返さない。読み取り専用）",
		schema:      periodSchema(nil),
		decode: func(raw json.RawMessage) (periodArgs, runFunc, *toolError) {
			var a periodArgs
			if err := strictDecode(raw, &a); err != nil {
				return a, nil, err
			}
			return a, func(ctx context.Context, e toolEnv) (any, int, error) {
				rr, err := query.Rejections(ctx, e.q, e.r)
				return rr, len(rr.Buckets), err
			}, nil
		},
	},
	{
		name:        "kill_switch_history",
		description: "Kill Switch の発動と解除のラベル履歴（新しい順。読み取り専用）",
		schema:      periodSchema(map[string]any{"limit": limitProp(maxListLimit)}),
		decode: func(raw json.RawMessage) (periodArgs, runFunc, *toolError) {
			var a struct {
				periodArgs
				Limit *int `json:"limit"`
			}
			if err := strictDecode(raw, &a); err != nil {
				return a.periodArgs, nil, err
			}
			limit, err := resolveLimit(a.Limit, maxListLimit)
			if err != nil {
				return a.periodArgs, nil, err
			}
			return a.periodArgs, func(ctx context.Context, e toolEnv) (any, int, error) {
				events, err := query.KillSwitchHistory(ctx, e.q, e.r, limit)
				return map[string]any{"events": events}, len(events), err
			}, nil
		},
	},
	{
		name:        "collection_status",
		description: "データ収集の状態: スナップショットの鮮度と欠損日、ジョブ件数、日足失敗、ラベルスキップ（読み取り専用）",
		schema:      periodSchema(nil),
		decode: func(raw json.RawMessage) (periodArgs, runFunc, *toolError) {
			var a periodArgs
			if err := strictDecode(raw, &a); err != nil {
				return a, nil, err
			}
			return a, func(ctx context.Context, e toolEnv) (any, int, error) {
				c, err := query.CollectionStatus(ctx, e.q, e.r)
				return c, len(c.MissingSessionDates), err
			}, nil
		},
	},
}

// findTool returns the table entry for name.
func findTool(name string) (tool, bool) {
	for _, t := range tools {
		if t.name == name {
			return t, true
		}
	}
	return tool{}, false
}

// ToolNames returns the dispatch table's tool names, in table order.
func ToolNames() []string {
	names := make([]string, len(tools))
	for i, t := range tools {
		names[i] = t.name
	}
	return names
}
