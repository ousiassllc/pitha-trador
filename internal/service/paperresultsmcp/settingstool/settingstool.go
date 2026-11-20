// Package settingstool declares the settings tools of the Paper results MCP
// (issues #806, #807; docs/api/endpoints/paper-results-mcp.md §3.9-3.14) as
// tooldef.Defs. They reach only the keys of mcpwritesettings.Keys: reads go
// through the read-only connection, writes through mcpset.Writer.
package settingstool

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/mcpwritesettings"
	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/experimenttool"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/labeltool"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/mcpset"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/tooldef"
)

// Defs are the settings tools.
func Defs() []tooldef.Def {
	return []tooldef.Def{
		readTool("settings_get", "MCP から変更できる基本設定（ホワイトリスト）の現在値の一覧。値・単位・出どころ（stored / default）・許容範囲・書き込み許可・スナップショット ID・LLM cutoff の状態コード llm_cutoff_status（placeholder / unverified / confirmed）を返す（秘密はマスク。読み取り専用）",
			func(ctx context.Context, e tooldef.Env) (any, int, error) {
				st, err := mcpset.Current(ctx, e.Q, e.PaperLimits)
				return st, len(st.Items), err
			}),
		{
			Name: "settings_set",
			Description: "ホワイトリストの設定を 1 つ変更する（Settings 画面と同じ保存先・同じ検証。書き込み許可がオフのときは拒否。変更は監査ログに残る）。" +
				"LLM cutoff を更新するときは 4 キーを 1 回ずつ、metalabel.llm_cutoff → metalabel.llm_cutoff_model → metalabel.llm_cutoff_confirmed_on → metalabel.llm_cutoff_source の順に設定する（公式日付の確認手順は各キーの description）。" +
				"これら 4 キーの応答には llm_cutoff_status が付く。日付だけでは unverified のまま、モデル名まで記録すると confirmed になる。" +
				"予算枠（risk.bucket.stock.budget_jpy / risk.bucket.bond_etf.budget_jpy）は 1 キーずつ保存し、株と債券ETFの合計が運用資金（risk.paper.initial_capital_jpy）を超える保存は拒否される。枠の間で予算を移すときは、減らす方を先に保存してから増やす方を保存する",
			Schema: tooldef.ObjectSchema(map[string]any{
				"key":   map[string]any{"type": "string", "description": "settings_get が返すキー"},
				"value": map[string]any{"type": []string{"string", "number"}, "description": "新しい値（Settings 画面に入れる文字列と同じ。数値キーは数値でも可）"},

				"hypothesis": labeltool.HypothesisProp(),
			}, "key", "value"),
			Writes:   true,
			NoPeriod: true,
			Decode: func(raw json.RawMessage) (tooldef.PeriodArgs, tooldef.Run, *tooldef.Failure) {
				var a struct {
					Key   string          `json:"key"`
					Value json.RawMessage `json:"value"`

					Hypothesis *string `json:"hypothesis"` // optional: also records an experiment (issue #1088)
				}
				if err := tooldef.StrictDecode(raw, &a); err != nil {
					return tooldef.PeriodArgs{}, nil, err
				}
				if a.Key == "" || len(a.Value) == 0 {
					return tooldef.PeriodArgs{}, nil, tooldef.Fail(tooldef.CodeInvalidArgument)
				}
				// The whitelist is checked before anything else, so an unknown or
				// dangerous key never reaches the write connection.
				if !mcpwritesettings.IsAllowed(a.Key) {
					return tooldef.PeriodArgs{}, nil, tooldef.Fail(tooldef.CodeKeyNotAllowed)
				}
				value, ok := tooldef.ValueText(a.Value)
				if !ok {
					return tooldef.PeriodArgs{}, nil, tooldef.Fail(tooldef.CodeInvalidArgument)
				}
				return tooldef.PeriodArgs{}, func(ctx context.Context, e tooldef.Env) (any, int, error) {
					w, err := e.Writer()
					if err != nil {
						return nil, 0, err
					}
					entry, exp, err := labeltool.Set(ctx, w, e.Client, a.Key, value, a.Hypothesis, experimenttool.FailureOf)
					err = writeFailureOf(err)
					if err != nil {
						return nil, 0, err
					}
					spec, _ := mcpwritesettings.Lookup(a.Key)
					out := map[string]any{
						"key": a.Key, "unit": spec.Unit, "value": entry.After, "before": entry.Before,
						"audit_id": entry.ID, "at": entry.At,
					}
					if exp != nil {
						out["experiment"] = exp // only when a hypothesis was given (issue #1088)
					}
					if metalabelsettings.IsCutoffKey(a.Key) {
						// The write is committed; a failed status read only omits the field.
						if status, serr := mcpset.CutoffStatus(ctx, mcpset.ReadStore{Q: e.Q}); serr == nil {
							out["llm_cutoff_status"] = status
						}
					}
					return out, 1, nil
				}, nil
			},
		},
		{
			Name:        "settings_diff",
			Description: "ホワイトリスト設定の差分（いまの値と、直前の保存前 / 指定スナップショットの時点との差）。スナップショットは settings_get の snapshot_id（監査ログ ID）か RFC3339 の時刻。引数なしは直近 1 回の保存の差分（読み取り専用）",
			Schema: tooldef.ObjectSchema(map[string]any{
				"since_id": map[string]any{"type": "integer", "minimum": 0, "description": "監査ログ ID。その行の保存直後の状態と比べる（0 は最初から）"},
				"since_at": map[string]any{"type": "string", "description": "RFC3339 の時刻。その時点の状態と比べる"},
			}),
			NoPeriod: true,
			Decode: func(raw json.RawMessage) (tooldef.PeriodArgs, tooldef.Run, *tooldef.Failure) {
				var a struct {
					SinceID *int64  `json:"since_id"`
					SinceAt *string `json:"since_at"`
				}
				if err := tooldef.StrictDecode(raw, &a); err != nil {
					return tooldef.PeriodArgs{}, nil, err
				}
				snap := mcpset.Snapshot{ID: a.SinceID}
				if a.SinceAt != nil {
					t, err := time.Parse(time.RFC3339, *a.SinceAt)
					if err != nil || a.SinceID != nil {
						return tooldef.PeriodArgs{}, nil, tooldef.Fail(tooldef.CodeInvalidArgument)
					}
					snap.At = &t
				}
				if a.SinceID != nil && *a.SinceID < 0 {
					return tooldef.PeriodArgs{}, nil, tooldef.Fail(tooldef.CodeInvalidArgument)
				}
				return tooldef.PeriodArgs{}, func(ctx context.Context, e tooldef.Env) (any, int, error) {
					d, err := mcpset.DiffSince(ctx, e.Q, e.PaperLimits, snap)
					if errors.Is(err, mcpset.ErrUnknownSnapshot) {
						return nil, 0, tooldef.Fail(tooldef.CodeInvalidArgument)
					}
					return d, len(d.Changes), err
				}, nil
			},
		},
		{
			Name:        "settings_audit",
			Description: "設定変更の監査ログ（いつ・誰が・どのキーを・変更前後の値。MCP と Settings 画面の両方の変更と、書き込み許可の切替。新しい順。読み取り専用）",
			Schema: tooldef.ObjectSchema(map[string]any{
				"limit":     tooldef.LimitProp(mcpset.MaxAuditList),
				"before_id": map[string]any{"type": "integer", "minimum": 1, "description": "前回応答の最後の id（これより古い行を返す）"},
			}),
			NoPeriod: true,
			Decode: func(raw json.RawMessage) (tooldef.PeriodArgs, tooldef.Run, *tooldef.Failure) {
				var a struct {
					Limit    *int   `json:"limit"`
					BeforeID *int64 `json:"before_id"`
				}
				if err := tooldef.StrictDecode(raw, &a); err != nil {
					return tooldef.PeriodArgs{}, nil, err
				}
				limit, terr := tooldef.ResolveLimit(a.Limit, mcpset.MaxAuditList)
				if terr != nil {
					return tooldef.PeriodArgs{}, nil, terr
				}
				var before int64
				if a.BeforeID != nil {
					if *a.BeforeID < 1 {
						return tooldef.PeriodArgs{}, nil, tooldef.Fail(tooldef.CodeInvalidArgument)
					}
					before = *a.BeforeID
				}
				return tooldef.PeriodArgs{}, func(ctx context.Context, e tooldef.Env) (any, int, error) {
					entries, err := mcpset.ListAudit(ctx, e.Q, limit, before)
					return map[string]any{"entries": entries}, len(entries), err
				}, nil
			},
		},
		switchTool("settings_write_enable", true, "設定の書き込みを許可する（settings_set を使えるようにする）。書き込みがオフでも呼べる。切替は監査ログに残る"),
		switchTool("settings_write_disable", false, "設定の書き込みを禁止する（settings_set が拒否される）。読み取りツールと settings_write_enable は使える。切替は監査ログに残る"),
	}
}

// readTool is a read-only tool without arguments and without a period.
func readTool(name, description string, run tooldef.Run) tooldef.Def {
	return tooldef.Def{Name: name, Description: description, Schema: tooldef.ObjectSchema(map[string]any{}), NoPeriod: true,
		Decode: func(raw json.RawMessage) (tooldef.PeriodArgs, tooldef.Run, *tooldef.Failure) {
			return tooldef.PeriodArgs{}, run, tooldef.NoArgs(raw)
		}}
}

// switchTool is settings_write_enable / settings_write_disable. They work
// while writes are off: that is how writes are turned back on.
func switchTool(name string, enable bool, description string) tooldef.Def {
	return tooldef.Def{
		Name:        name,
		Description: description,
		Schema:      tooldef.ObjectSchema(map[string]any{}),
		Writes:      true,
		NoPeriod:    true,
		Decode: func(raw json.RawMessage) (tooldef.PeriodArgs, tooldef.Run, *tooldef.Failure) {
			if err := tooldef.NoArgs(raw); err != nil {
				return tooldef.PeriodArgs{}, nil, err
			}
			return tooldef.PeriodArgs{}, func(ctx context.Context, e tooldef.Env) (any, int, error) {
				w, err := e.Writer()
				if err != nil {
					return nil, 0, err
				}
				entry, err := w.SetEnabled(ctx, e.Client, enable)
				if err != nil {
					return nil, 0, err
				}
				return map[string]any{"write_enabled": enable, "audit_id": entry.ID, "at": entry.At}, 1, nil
			}, nil
		},
	}
}

// writeFailureOf maps a Writer error to the tool's spec error; anything else
// is left to classify (busy, timeout, unavailable, internal).
func writeFailureOf(err error) error {
	if reason, ok := mcpset.IsInvalidValue(err); ok {
		return tooldef.FailDetail(tooldef.CodeInvalidValue, reason)
	}
	switch {
	case errors.Is(err, mcpset.ErrWriteDisabled):
		return tooldef.Fail(tooldef.CodeWriteDisabled)
	case errors.Is(err, mcpset.ErrKeyNotAllowed):
		return tooldef.Fail(tooldef.CodeKeyNotAllowed)
	}
	return err
}
