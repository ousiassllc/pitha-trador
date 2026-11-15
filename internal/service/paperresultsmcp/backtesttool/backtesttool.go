// Package backtesttool is the research-backtest tools of the Paper results MCP
// (issue #1086), declared with tooldef.
package backtesttool

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/backtestrun"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
	"github.com/ousiassllc/pitha-trador/internal/service/backtest"
	"github.com/ousiassllc/pitha-trador/internal/service/backtest/variant"
	"github.com/ousiassllc/pitha-trador/internal/service/backtestlab"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/backtestjob"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/backtestview"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/mcpset"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/mcpset/researchwrite"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/tooldef"
)

// The research-backtest tools (issue #1086; docs/api/endpoints/paper-results-mcp-backtest.md):
// backtest_start records one run in backtest_runs (mcpset.Writer, behind the
// write switch) and replays it in the background; backtest_get / backtest_list
// only read. A run reads stored market data and decisions, changes no setting
// and its result is an input of nothing.

// Options configures backtest_start (issue #1086).
type Options struct {
	// Source builds the replay input. Nil: backtest_start answers
	// backtest_unavailable. cmd/paper-results-mcp supplies one that opens its own
	// read-only database handle on first use.
	Source backtestjob.SourceFactory
	// RunTimeout overrides the 10 minute cap of one backtest run.
	RunTimeout time.Duration
	// Owner overrides the random token that tells this process's backtest rows
	// from an earlier process's.
	Owner string
	// OnFinish is called after a backtest run's final status was written: a test
	// seam, so a test waits for the end without sleeping.
	OnFinish func(runID int64)
}

// NewManager is the job manager that runs backtest_start's backtests in the
// background and records them through the MCP's write connection.
func NewManager(o Options, log *slog.Logger, writer func() (*mcpset.Writer, error)) *backtestjob.Manager {
	return backtestjob.New(backtestjob.Config{
		Source: o.Source, Owner: o.Owner, RunTimeout: o.RunTimeout, Logger: log, OnFinish: o.OnFinish,
		Store: func() (backtestjob.Store, error) {
			w, err := writer()
			if err != nil {
				return nil, err
			}
			return researchwrite.Backtests{W: w}, nil
		},
	})
}

// Listing limits of backtest_list.
const (
	defaultBacktestList = 20
	maxBacktestList     = 50
)

// The fixed texts of the refusals (error.data.detail).
const (
	detailBacktestBusy     = "別のバックテストが実行中です（同時に 1 件まで）。backtest_get で終わるのを確認してから出し直してください。何も書きません"
	detailBacktestPeriod   = "バックテストの期間は最大 31 日です"
	detailBacktestVariants = "variants は 12 組までです"
	detailBacktestExtra    = "extra_cost_bps は 6 個までです"
)

// Defs are backtest_start, backtest_get and backtest_list.
func Defs() []tooldef.Def {
	return []tooldef.Def{
		{
			Name: "backtest_start",
			Description: "保存済みの market_snapshots と Jev の判断を再生して（読むだけ）、売買の条件（variants）ごとのバックテストをバックグラウンドで 1 回起動する。" +
				"すぐに run_id と status（queued）を返す。終わったかどうかと結果は backtest_get で読む（status が succeeded になるまで）。" +
				"variants は 1〜12 組（省略は基準 1 組＝いまの設定のまま）。各組は {label（40 文字まで・省略可）, params}。params に書いた項目だけを基準から変える: " +
				"entry_rule（meta / legacy_confidence）・min_ev_bps・min_entry_quality_long/short・min_probability_long/short・min_continuation_probability_long/short・" +
				"stop_loss_pct・take_profit_pct・trailing_stop_pct・max_holding_minutes・slippage_bps・fee_bps・max_trade_loss_pct・max_position_per_symbol_pct。" +
				"組は同じ期間・同じデータで 1 組ずつ順に再生する。範囲外の値は invalid_value（detail に項目名と許容範囲）、13 組以上・extra_cost_bps が 7 個以上・期間が 31 日を超えるときは limit_exceeded。" +
				"extra_cost_bps は追加コストの感度表の行（往復の bps。省略は 0・2・5・10。6 個まで）。" +
				"from / to は JST の日付（to を含まない。省略は直近 30 日。最大 31 日）。replay_mode は min_entry_quality の基準で paper（既定）か live。" +
				"同時に走れるのは 1 件で、実行中にもう一度呼ぶと concurrency_limited（何も書かない）。1 回の実行は 10 分まで（超えると failed・error_code timed_out）。" +
				"書き込み許可がオフのときは write_disabled。研究用の記録で、設定・Policy・Risk・発注・資金配分には使われない（runtime_settings は変えない）",
			Schema: tooldef.PeriodSchema(map[string]any{
				"replay_mode": map[string]any{"type": "string", "enum": []string{"paper", "live"}, "default": "paper", "description": "min_entry_quality の基準（paper: policy.paper.*、live: policy.*）"},
				"variants": map[string]any{
					"type": "array", "minItems": 1, "maxItems": backtestlab.MaxVariants,
					"description": "試す条件の組。省略は基準（いまの設定）1 組",
					"items": tooldef.ObjectSchema(map[string]any{
						"label":  map[string]any{"type": "string", "maxLength": backtestlab.MaxLabelRunes},
						"params": variantParamsSchema(),
					}),
				},
				"extra_cost_bps": map[string]any{
					"type": "array", "maxItems": backtestlab.MaxExtraCostPoints, "items": map[string]any{"type": "number", "minimum": 0, "maximum": backtestlab.MaxExtraCostBps},
					"description": "追加の往復コスト（bps）の行。省略は [0, 2, 5, 10]",
				},
			}),
			Writes: true,
			Decode: decodeBacktestStart,
		},
		{
			Name: "backtest_get",
			Description: "backtest_start で起動したバックテスト 1 件の状態と結果（読み取り専用）。status は queued / running / succeeded / failed。" +
				"期間（依頼した from〜to と、実際にデータのあった最初と最後のスナップショットの時刻）・再生モード・コードの版（code_version）・試した組の数（explored_count）・追加コスト（extra_cost_bps）・依頼した組（requested_variants）を常に返す。" +
				"succeeded のとき variants に組ごとの結果: trade_count・win_rate・expectancy_pct（コスト込みの 1 取引あたりの平均純リターン）・max_drawdown_pct・gross/slippage/fee/net_pnl_pct・" +
				"days（JST の決済日ごとの件数と純損益）・expectancy_ci（決済日でまとめた 95% 区間。2 日未満は has_ci が false）・sensitivity（追加コストごとの期待値と勝ち残る割合）。" +
				"is_reference_best は期待値が最大の 1 組だけに付く参考印で、複数の組から選んだ最良値は偶然で良く見える（多重比較）ので、explored_count と caution と一緒に読む。" +
				"failed のとき error_code は timed_out / interrupted / unavailable / internal のいずれか（エラー文は返さない）。MCP の再起動で途中のまま残った実行は interrupted として返る",
			Schema: tooldef.ObjectSchema(map[string]any{
				"run_id": map[string]any{"type": "integer", "minimum": 1},
			}, "run_id"),
			NoPeriod: true,
			Decode:   decodeBacktestGet,
		},
		{
			Name: "backtest_list",
			Description: "起動したバックテストの一覧（新しい順。状態で絞り込める。読み取り専用）。各行は run_id・status・時刻・期間・再生モード・code_version・explored_count・error_code。" +
				"組ごとの結果は含まない（backtest_get）。古い実行は自動では消えず、一覧は limit 件ずつ（既定 20・最大 50）返す",
			Schema: tooldef.ObjectSchema(map[string]any{
				"status": map[string]any{"type": "string", "enum": backtestrun.Statuses(), "description": "状態で絞り込む"},
				"limit":  map[string]any{"type": "integer", "minimum": 1, "maximum": maxBacktestList, "default": defaultBacktestList},
				"cursor": map[string]any{"type": "string", "description": "前回応答の next_cursor"},
			}),
			NoPeriod: true,
			Decode:   decodeBacktestList,
		},
	}
}

// variantParamsSchema is the schema of variant.Params: every member optional.
func variantParamsSchema() map[string]any {
	num := func(desc string) map[string]any { return map[string]any{"type": "number", "description": desc} }
	quality := map[string]any{"type": "string", "enum": []string{"poor", "fair", "good", "strong", "exceptional"}}
	return tooldef.ObjectSchema(map[string]any{
		"entry_rule":                         map[string]any{"type": "string", "enum": []string{variant.EntryRuleMeta, variant.EntryRuleLegacyConfidence}, "description": "meta（ライブの規則）/ legacy_confidence（Jev の確信度を min_probability と比べる比較用）"},
		"min_ev_bps":                         num("meta 規則が見る期待値（bps）の下限。0〜1000"),
		"min_entry_quality_long":             quality,
		"min_entry_quality_short":            quality,
		"min_probability_long":               num("0 より大きく 1 以下（legacy_confidence だけが読む）"),
		"min_probability_short":              num("0 より大きく 1 以下（legacy_confidence だけが読む）"),
		"min_continuation_probability_long":  num("0〜1"),
		"min_continuation_probability_short": num("0〜1"),
		"stop_loss_pct":                      num("0〜100（0 は使わない）"),
		"take_profit_pct":                    num("0〜100（0 は使わない）"),
		"trailing_stop_pct":                  num("0〜100（0 は使わない）"),
		"max_holding_minutes":                map[string]any{"type": "integer", "minimum": 0, "maximum": variant.MaxHoldingMinutes, "description": "0〜1440（0 は使わない）"},
		"slippage_bps":                       num("0〜200"),
		"fee_bps":                            num("0〜200"),
		"max_trade_loss_pct":                 num("1 取引の最大損失（資金に対する %）。0.1〜3"),
		"max_position_per_symbol_pct":        num("1 銘柄の最大ポジション（%）。0 より大きく 100 以下"),
	})
}

func decodeBacktestStart(raw json.RawMessage) (tooldef.PeriodArgs, tooldef.Run, *tooldef.Failure) {
	var a struct {
		tooldef.PeriodArgs
		ReplayMode   string                `json:"replay_mode"`
		Variants     []backtestlab.Variant `json:"variants"`
		ExtraCostBps []float64             `json:"extra_cost_bps"`
	}
	if err := tooldef.StrictDecode(raw, &a); err != nil {
		return a.PeriodArgs, nil, err
	}
	mode := a.ReplayMode
	switch mode {
	case "":
		mode = "paper"
	case "paper", "live":
	default:
		return a.PeriodArgs, nil, tooldef.Fail(tooldef.CodeInvalidArgument)
	}
	req, err := backtestlab.Normalize(a.Variants, a.ExtraCostBps)
	var invalid *backtestlab.InvalidError
	switch {
	case errors.Is(err, backtestlab.ErrTooManyVariants):
		return a.PeriodArgs, nil, tooldef.FailDetail(tooldef.CodeLimitExceeded, detailBacktestVariants)
	case errors.Is(err, backtestlab.ErrTooManyExtraCosts):
		return a.PeriodArgs, nil, tooldef.FailDetail(tooldef.CodeLimitExceeded, detailBacktestExtra)
	case errors.As(err, &invalid):
		return a.PeriodArgs, nil, tooldef.FailDetail(tooldef.CodeInvalidValue, invalid.Field+": "+invalid.Reason)
	case err != nil:
		return a.PeriodArgs, nil, &tooldef.Failure{Code: tooldef.CodeInvalidArgument, Cause: err}
	}
	return a.PeriodArgs, func(ctx context.Context, e tooldef.Env) (any, int, error) {
		if e.Range.To.After(e.Range.From.AddDate(0, 0, backtestlab.MaxPeriodDays)) {
			return nil, 0, tooldef.FailDetail(tooldef.CodeLimitExceeded, detailBacktestPeriod)
		}
		run, err := e.Backtests.Start(ctx, backtestjob.Job{
			Client:   e.Client,
			FromDate: e.Range.From.In(query.JST).Format("2006-01-02"), ToDate: e.Range.To.In(query.JST).Format("2006-01-02"),
			Period:     backtest.Period{Start: e.Range.From.UTC(), End: e.Range.To.UTC()},
			ReplayMode: mode, Request: req,
		})
		switch {
		case errors.Is(err, backtestjob.ErrBusy):
			return nil, 0, tooldef.FailDetail(tooldef.CodeConcurrencyLimited, detailBacktestBusy)
		case errors.Is(err, backtestjob.ErrUnavailable):
			return nil, 0, tooldef.Fail(tooldef.CodeBacktestUnavailable)
		case errors.Is(err, mcpset.ErrWriteDisabled):
			return nil, 0, tooldef.Fail(tooldef.CodeWriteDisabled)
		case err != nil:
			return nil, 0, err
		}
		return backtestview.NewRun(run, e.Backtests.Owner()), 1, nil
	}, nil
}

func decodeBacktestGet(raw json.RawMessage) (tooldef.PeriodArgs, tooldef.Run, *tooldef.Failure) {
	var a struct {
		RunID *int64 `json:"run_id"`
	}
	if err := tooldef.StrictDecode(raw, &a); err != nil {
		return tooldef.PeriodArgs{}, nil, err
	}
	if a.RunID == nil || *a.RunID < 1 {
		return tooldef.PeriodArgs{}, nil, tooldef.Fail(tooldef.CodeInvalidArgument)
	}
	id := *a.RunID
	return tooldef.PeriodArgs{}, func(ctx context.Context, e tooldef.Env) (any, int, error) {
		run, err := backtestrun.Get(ctx, e.Q, id)
		if errors.Is(err, backtestrun.ErrNotFound) {
			return nil, 0, tooldef.Fail(tooldef.CodeBacktestNotFound)
		}
		if err != nil {
			return nil, 0, err
		}
		detail, err := backtestview.NewDetail(run, e.Backtests.Owner())
		return detail, 1, err
	}, nil
}

func decodeBacktestList(raw json.RawMessage) (tooldef.PeriodArgs, tooldef.Run, *tooldef.Failure) {
	var a struct {
		Status string `json:"status"`
		Limit  *int   `json:"limit"`
		Cursor string `json:"cursor"`
	}
	if err := tooldef.StrictDecode(raw, &a); err != nil {
		return tooldef.PeriodArgs{}, nil, err
	}
	limit, err := tooldef.ResolveLimit(a.Limit, maxBacktestList)
	if a.Limit == nil {
		limit = defaultBacktestList
	}
	if err != nil {
		return tooldef.PeriodArgs{}, nil, err
	}
	if a.Status != "" && !backtestrun.ValidStatus(a.Status) {
		return tooldef.PeriodArgs{}, nil, tooldef.Fail(tooldef.CodeInvalidArgument)
	}
	return tooldef.PeriodArgs{}, func(ctx context.Context, e tooldef.Env) (any, int, error) {
		filter := backtestrun.ListFilter{Status: a.Status, Limit: limit}
		if a.Cursor != "" {
			_, id, err := query.DecodeCursor(a.Cursor)
			if err != nil {
				return nil, 0, err
			}
			filter.BeforeID = id
		}
		runs, more, err := backtestrun.List(ctx, e.Q, filter)
		if err != nil {
			return nil, 0, err
		}
		views := make([]backtestview.Run, len(runs))
		for i, r := range runs {
			views[i] = backtestview.NewRun(r, e.Backtests.Owner())
		}
		var next *string
		if more {
			last := runs[len(runs)-1]
			c := query.EncodeCursor(sqlutil.FormatTime(last.CreatedAt), last.ID)
			next = &c
		}
		return map[string]any{"runs": views, "next_cursor": next}, len(views), nil
	}, nil
}
