package paperresultsmcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/mcpwritesettings"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/mcpset"
)

// The settings tools (issues #806, #807; docs/api/endpoints/paper-results-mcp.md
// §3.9-3.14). They reach only the keys of mcpwritesettings.Keys: reads go
// through the read-only connection, writes through mcpset.Writer.

func init() {
	tools = append(tools, settingsTools...)
}

// paperCapital is the Paper capital for the return-rate calculations: the
// Settings value (risk.paper.initial_capital_jpy in runtime_settings, read
// through the read-only connection) when one is stored, otherwise the
// risk.yaml value the process started with.
func (e toolEnv) paperCapital(ctx context.Context) (float64, error) {
	return mcpset.PaperCapitalJPY(ctx, e.q, e.capitalJPY)
}

// noArgs decodes the (empty) arguments of a tool that takes none.
func noArgs(raw json.RawMessage) *toolError {
	var a struct{}
	return strictDecode(raw, &a)
}

func objectSchema(props map[string]any, required ...string) map[string]any {
	s := map[string]any{"type": "object", "properties": props, "additionalProperties": false}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

var settingsTools = []tool{
	{
		name:        "settings_get",
		description: "MCP から変更できる基本設定（ホワイトリスト）の現在値の一覧。値・単位・出どころ（stored / default）・許容範囲・書き込み許可・スナップショット ID を返す（秘密はマスク。読み取り専用）",
		schema:      objectSchema(map[string]any{}),
		noPeriod:    true,
		decode: func(raw json.RawMessage) (periodArgs, runFunc, *toolError) {
			if err := noArgs(raw); err != nil {
				return periodArgs{}, nil, err
			}
			return periodArgs{}, func(ctx context.Context, e toolEnv) (any, int, error) {
				st, err := mcpset.Current(ctx, e.q, e.paperLimits)
				return st, len(st.Items), err
			}, nil
		},
	},
	{
		name:        "settings_set",
		description: "ホワイトリストの設定を 1 つ変更する（Settings 画面と同じ保存先・同じ検証。書き込み許可がオフのときは拒否。変更は監査ログに残る）",
		schema: objectSchema(map[string]any{
			"key":   map[string]any{"type": "string", "description": "settings_get が返すキー"},
			"value": map[string]any{"type": []string{"string", "number"}, "description": "新しい値（Settings 画面に入れる文字列と同じ。数値キーは数値でも可）"},
		}, "key", "value"),
		writes:   true,
		noPeriod: true,
		decode: func(raw json.RawMessage) (periodArgs, runFunc, *toolError) {
			var a struct {
				Key   string          `json:"key"`
				Value json.RawMessage `json:"value"`
			}
			if err := strictDecode(raw, &a); err != nil {
				return periodArgs{}, nil, err
			}
			if a.Key == "" || len(a.Value) == 0 {
				return periodArgs{}, nil, fail(CodeInvalidArgument)
			}
			// The whitelist is checked before anything else, so an unknown or
			// dangerous key never reaches the write connection.
			if !mcpwritesettings.IsAllowed(a.Key) {
				return periodArgs{}, nil, fail(CodeKeyNotAllowed)
			}
			value, ok := valueText(a.Value)
			if !ok {
				return periodArgs{}, nil, fail(CodeInvalidArgument)
			}
			return periodArgs{}, func(ctx context.Context, e toolEnv) (any, int, error) {
				w, err := e.writer()
				if err != nil {
					return nil, 0, err
				}
				entry, err := w.Set(ctx, e.client, a.Key, value)
				if err != nil {
					return nil, 0, writeFailureOf(err)
				}
				spec, _ := mcpwritesettings.Lookup(a.Key)
				return map[string]any{
					"key": a.Key, "unit": spec.Unit, "value": entry.After, "before": entry.Before,
					"audit_id": entry.ID, "at": entry.At,
				}, 1, nil
			}, nil
		},
	},
	{
		name:        "settings_diff",
		description: "ホワイトリスト設定の差分（いまの値と、直前の保存前 / 指定スナップショットの時点との差）。スナップショットは settings_get の snapshot_id（監査ログ ID）か RFC3339 の時刻。引数なしは直近 1 回の保存の差分（読み取り専用）",
		schema: objectSchema(map[string]any{
			"since_id": map[string]any{"type": "integer", "minimum": 0, "description": "監査ログ ID。その行の保存直後の状態と比べる（0 は最初から）"},
			"since_at": map[string]any{"type": "string", "description": "RFC3339 の時刻。その時点の状態と比べる"},
		}),
		noPeriod: true,
		decode: func(raw json.RawMessage) (periodArgs, runFunc, *toolError) {
			var a struct {
				SinceID *int64  `json:"since_id"`
				SinceAt *string `json:"since_at"`
			}
			if err := strictDecode(raw, &a); err != nil {
				return periodArgs{}, nil, err
			}
			snap := mcpset.Snapshot{ID: a.SinceID}
			if a.SinceAt != nil {
				t, err := time.Parse(time.RFC3339, *a.SinceAt)
				if err != nil || a.SinceID != nil {
					return periodArgs{}, nil, fail(CodeInvalidArgument)
				}
				snap.At = &t
			}
			if a.SinceID != nil && *a.SinceID < 0 {
				return periodArgs{}, nil, fail(CodeInvalidArgument)
			}
			return periodArgs{}, func(ctx context.Context, e toolEnv) (any, int, error) {
				d, err := mcpset.DiffSince(ctx, e.q, e.paperLimits, snap)
				if errors.Is(err, mcpset.ErrUnknownSnapshot) {
					return nil, 0, fail(CodeInvalidArgument)
				}
				return d, len(d.Changes), err
			}, nil
		},
	},
	{
		name:        "settings_audit",
		description: "設定変更の監査ログ（いつ・誰が・どのキーを・変更前後の値。MCP と Settings 画面の両方の変更と、書き込み許可の切替。新しい順。読み取り専用）",
		schema: objectSchema(map[string]any{
			"limit":     limitProp(mcpset.MaxAuditList),
			"before_id": map[string]any{"type": "integer", "minimum": 1, "description": "前回応答の最後の id（これより古い行を返す）"},
		}),
		noPeriod: true,
		decode: func(raw json.RawMessage) (periodArgs, runFunc, *toolError) {
			var a struct {
				Limit    *int   `json:"limit"`
				BeforeID *int64 `json:"before_id"`
			}
			if err := strictDecode(raw, &a); err != nil {
				return periodArgs{}, nil, err
			}
			limit, terr := resolveLimit(a.Limit, mcpset.MaxAuditList)
			if terr != nil {
				return periodArgs{}, nil, terr
			}
			var before int64
			if a.BeforeID != nil {
				if *a.BeforeID < 1 {
					return periodArgs{}, nil, fail(CodeInvalidArgument)
				}
				before = *a.BeforeID
			}
			return periodArgs{}, func(ctx context.Context, e toolEnv) (any, int, error) {
				entries, err := mcpset.ListAudit(ctx, e.q, limit, before)
				return map[string]any{"entries": entries}, len(entries), err
			}, nil
		},
	},
	switchTool("settings_write_enable", true, "設定の書き込みを許可する（settings_set を使えるようにする）。書き込みがオフでも呼べる。切替は監査ログに残る"),
	switchTool("settings_write_disable", false, "設定の書き込みを禁止する（settings_set が拒否される）。読み取りツールと settings_write_enable は使える。切替は監査ログに残る"),
}

// switchTool is settings_write_enable / settings_write_disable. They work
// while writes are off: that is how writes are turned back on.
func switchTool(name string, enable bool, description string) tool {
	return tool{
		name:        name,
		description: description,
		schema:      objectSchema(map[string]any{}),
		writes:      true,
		noPeriod:    true,
		decode: func(raw json.RawMessage) (periodArgs, runFunc, *toolError) {
			if err := noArgs(raw); err != nil {
				return periodArgs{}, nil, err
			}
			return periodArgs{}, func(ctx context.Context, e toolEnv) (any, int, error) {
				w, err := e.writer()
				if err != nil {
					return nil, 0, err
				}
				entry, err := w.SetEnabled(ctx, e.client, enable)
				if err != nil {
					return nil, 0, err
				}
				return map[string]any{"write_enabled": enable, "audit_id": entry.ID, "at": entry.At}, 1, nil
			}, nil
		},
	}
}

// valueText turns a JSON string or number into the text the Settings screen
// would receive. Booleans, null, arrays and objects are not values.
func valueText(raw json.RawMessage) (string, bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return "", false
	}
	switch raw[0] {
	case '"':
		var s string
		if json.Unmarshal(raw, &s) != nil {
			return "", false
		}
		return s, true
	case '-', '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
		var f float64
		if json.Unmarshal(raw, &f) != nil {
			return "", false
		}
		return strconv.FormatFloat(f, 'f', -1, 64), true
	}
	return "", false
}

// writeFailureOf maps a Writer error to the tool's spec error; anything else
// is left to classify (busy, timeout, unavailable, internal).
func writeFailureOf(err error) error {
	if reason, ok := mcpset.IsInvalidValue(err); ok {
		return failDetail(CodeInvalidValue, reason)
	}
	switch {
	case errors.Is(err, mcpset.ErrWriteDisabled):
		return fail(CodeWriteDisabled)
	case errors.Is(err, mcpset.ErrKeyNotAllowed):
		return fail(CodeKeyNotAllowed)
	}
	return err
}
