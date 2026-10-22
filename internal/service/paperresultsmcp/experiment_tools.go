package paperresultsmcp

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/experiment"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/mcpset"
)

// The tuning-experiment tools (issue #871; docs/api/endpoints/
// paper-results-mcp-experiments.md). experiment_create and experiment_close
// write one tuning_experiments row each, through mcpset.Writer and behind the
// write switch; experiments_list and experiment_compare only read. None of them
// changes a setting, the model or the prompt: an experiment records a change
// that was made some other way (settings_set, a model switch, a prompt version
// bump) and compares what happened afterwards.

func init() {
	tools = append(tools, experimentTools...)
}

// Listing limits of experiments_list: every row costs two aggregate scans.
const (
	defaultExperimentList = 20
	maxExperimentList     = 50
)

// changeArg is one entry of experiment_create's changes. before and after take
// a string or a number, like settings_set's value.
type changeArg struct {
	Kind   string          `json:"kind"`
	Name   string          `json:"name"`
	Before json.RawMessage `json:"before"`
	After  json.RawMessage `json:"after"`
}

var experimentTools = []tool{
	{
		name: "experiment_create",
		description: "調整の実験を 1 件記録する（仮説・変えた変数 1 つ・前後の値・開始時刻。変更そのものはしない。設定は先に settings_set で変え、モデルやプロンプトの版の切り替えは本体側で行ってから記録する）。" +
			"変えてよい変数は 1 つだけで、changes に 2 つ以上を入れると multiple_variables で拒否する。" +
			"kind は setting（設定キー。settings_get に出る MCP から変更できるキーだけ）/ model（Jev のモデル）/ prompt（Jev のプロンプトの版）。" +
			"setting の audit_id には、その変更の settings_set の応答の audit_id を渡せる（任意。実験期間の交絡の検出で、その変更自体を除くために使う）。" +
			"開始時刻 started_at は RFC3339（省略は今。未来は不可）。書き込み許可がオフのときは write_disabled",
		schema: objectSchema(map[string]any{
			"hypothesis": map[string]any{"type": "string", "maxLength": experiment.MaxHypothesisRunes, "description": "仮説（何をどう変えると何がどうなると考えるか）"},
			"changes": map[string]any{
				"type": "array", "minItems": 1,
				"items": objectSchema(map[string]any{
					"kind":   map[string]any{"type": "string", "enum": []string{experiment.KindSetting, experiment.KindModel, experiment.KindPrompt}},
					"name":   map[string]any{"type": "string", "maxLength": experiment.MaxNameRunes, "description": "設定キー / モデルの呼び名 / プロンプトの版の呼び名"},
					"before": map[string]any{"type": []string{"string", "number"}, "description": "変更前の値"},
					"after":  map[string]any{"type": []string{"string", "number"}, "description": "変更後の値（before と違う値）"},
				}, "kind", "name", "before", "after"),
				"description": "変えた変数。ちょうど 1 つ",
			},
			"started_at": map[string]any{"type": "string", "description": "実験の開始時刻（RFC3339）。変更が効き始めた時刻。省略は今"},
			"audit_id":   map[string]any{"type": "integer", "minimum": 1, "description": "kind が setting のとき、その変更の監査 ID（settings_set の応答の audit_id）"},
		}, "hypothesis", "changes"),
		writes:   true,
		noPeriod: true,
		decode: func(raw json.RawMessage) (periodArgs, runFunc, *toolError) {
			var a struct {
				Hypothesis string      `json:"hypothesis"`
				Changes    []changeArg `json:"changes"`
				StartedAt  string      `json:"started_at"`
				AuditID    *int64      `json:"audit_id"`
			}
			if err := strictDecode(raw, &a); err != nil {
				return periodArgs{}, nil, err
			}
			args := experiment.Args{Hypothesis: a.Hypothesis, StartedAt: a.StartedAt, AuditID: a.AuditID}
			for _, c := range a.Changes {
				before, okBefore := valueText(c.Before)
				after, okAfter := valueText(c.After)
				if !okBefore || !okAfter {
					return periodArgs{}, nil, fail(CodeInvalidArgument)
				}
				args.Changes = append(args.Changes, experiment.Change{Kind: c.Kind, Name: c.Name, Before: before, After: after})
			}
			return periodArgs{}, func(ctx context.Context, e toolEnv) (any, int, error) {
				req, err := args.Validate(e.now)
				if err != nil {
					return nil, 0, experimentFailureOf(err)
				}
				w, err := e.writer()
				if err != nil {
					return nil, 0, err
				}
				exp, err := w.CreateExperiment(ctx, e.client, req)
				if err != nil {
					return nil, 0, experimentFailureOf(err)
				}
				return exp, 1, nil
			}, nil
		},
	},
	{
		name: "experiment_close",
		description: "実行中の実験を終える（終了時刻を入れる。終了時刻は開始時刻以後・現在以前。省略は今）。" +
			"status は closed（終了。判定の対象）か aborted（中止。判定を出さない。既定は closed）。終わった実験は再び開けず not_running。書き込み許可がオフのときは write_disabled",
		schema: objectSchema(map[string]any{
			"experiment_id": map[string]any{"type": "integer", "minimum": 1},
			"status":        map[string]any{"type": "string", "enum": []string{experiment.StatusClosed, experiment.StatusAborted}, "default": experiment.StatusClosed},
			"ended_at":      map[string]any{"type": "string", "description": "終了時刻（RFC3339）。省略は今"},
		}, "experiment_id"),
		writes:   true,
		noPeriod: true,
		decode: func(raw json.RawMessage) (periodArgs, runFunc, *toolError) {
			var a struct {
				ExperimentID *int64  `json:"experiment_id"`
				Status       string  `json:"status"`
				EndedAt      *string `json:"ended_at"`
			}
			if err := strictDecode(raw, &a); err != nil {
				return periodArgs{}, nil, err
			}
			if a.ExperimentID == nil || *a.ExperimentID < 1 {
				return periodArgs{}, nil, fail(CodeInvalidArgument)
			}
			status := a.Status
			if status == "" {
				status = experiment.StatusClosed
			}
			if status != experiment.StatusClosed && status != experiment.StatusAborted {
				return periodArgs{}, nil, fail(CodeInvalidArgument)
			}
			var endedAt *time.Time
			if a.EndedAt != nil {
				t, err := time.Parse(time.RFC3339, *a.EndedAt)
				if err != nil {
					return periodArgs{}, nil, failDetail(CodeInvalidArgument, "ended_at は RFC3339 の時刻です")
				}
				endedAt = &t
			}
			id := *a.ExperimentID
			return periodArgs{}, func(ctx context.Context, e toolEnv) (any, int, error) {
				w, err := e.writer()
				if err != nil {
					return nil, 0, err
				}
				exp, err := w.CloseExperiment(ctx, id, status, endedAt)
				if err != nil {
					return nil, 0, experimentFailureOf(err)
				}
				return exp, 1, nil
			}, nil
		},
	},
	{
		name: "experiments_list",
		description: "調整の実験の一覧（新しい順）。状態・期間・変えた変数・前後の取引件数・判定の可否（verdict / judged）つき。" +
			"判定は 1 取引あたりの純損益（コスト控除後）の平均を前後で Welch の t 検定で比べ、差（後−前）の 95% 信頼区間と最大ドローダウンで決める（勝ち＝improved は、区間の下限が 0 より上かつドローダウンが前以下のときだけ）。" +
			"前後どちらかの取引件数が min_trades に届かないと insufficient_trades、実験中に対象以外が変わると confounded（どちらも判定なし）。" +
			"min_trades は下限 64（暫定の案でまだ承認されていない。応答の min_trades_source）。上げるのは可、下げるのは不可（下限未満は invalid_value で拒否）。読み取り専用",
		schema: objectSchema(map[string]any{
			"status":     map[string]any{"type": "string", "enum": []string{experiment.StatusRunning, experiment.StatusClosed, experiment.StatusAborted}, "description": "状態で絞り込む"},
			"limit":      map[string]any{"type": "integer", "minimum": 1, "maximum": maxExperimentList, "default": defaultExperimentList},
			"min_trades": minTradesProp(),
		}),
		noPeriod: true,
		decode: func(raw json.RawMessage) (periodArgs, runFunc, *toolError) {
			var a struct {
				Status    string `json:"status"`
				Limit     *int   `json:"limit"`
				MinTrades *int   `json:"min_trades"`
			}
			if err := strictDecode(raw, &a); err != nil {
				return periodArgs{}, nil, err
			}
			limit := defaultExperimentList
			if a.Limit != nil {
				switch {
				case *a.Limit < 1:
					return periodArgs{}, nil, fail(CodeInvalidArgument)
				case *a.Limit > maxExperimentList:
					return periodArgs{}, nil, fail(CodeLimitExceeded)
				}
				limit = *a.Limit
			}
			switch a.Status {
			case "", experiment.StatusRunning, experiment.StatusClosed, experiment.StatusAborted:
			default:
				return periodArgs{}, nil, fail(CodeInvalidArgument)
			}
			minTrades, err := resolveMinTrades(a.MinTrades)
			if err != nil {
				return periodArgs{}, nil, err
			}
			return periodArgs{}, func(ctx context.Context, e toolEnv) (any, int, error) {
				capital, err := e.paperCapital(ctx)
				if err != nil {
					return nil, 0, err
				}
				list, err := experiment.SummarizeAll(ctx, e.q, a.Status, limit, e.now, minTrades, capital)
				return map[string]any{"experiments": list}, len(list), err
			}, nil
		},
	},
	{
		name: "experiment_compare",
		description: "実験 1 つの前後比較。後 = 実験の開始から終了まで（実行中は今まで）、前 = 開始の直前の同じ長さ。" +
			"前後それぞれについて、paper_performance_summary と同じ指標（取引件数・純損益（コスト控除後）・勝率・平均の勝ち（avg_win_jpy）・平均の負け（avg_loss_jpy）・最大ドローダウン）と、" +
			"risk_rejections と同じ拒否理由の内訳を返す（クローズ時刻で数える。期間が JST の日の境界なら同じ期間の両ツールの応答と一致する）。" +
			"per_trade_pnl は 1 取引あたりの純損益（paper_performance_summary と同じ取引集合）の前後の n・平均・標準偏差と、平均の差 diff（後−前）・その 95% 信頼区間（ci_low / ci_high / ci_level）・Welch の t 値と自由度（t / df）。計算できない値は null。" +
			"verdict は aborted（中止）/ confounded（実験中に対象以外のホワイトリスト設定・Jev のモデル・プロンプトの版が変わった。confounders に列挙）/ " +
			"insufficient_trades（前後どちらかが min_trades 未満。数値は返すが判定しない）/ not_computable（件数は足りるが全取引が同一損益で区間が出ない。判定しない）/ " +
			"improved（区間の下限が 0 より上、かつ最大ドローダウンが前以下。唯一の勝ち）/ mixed（区間の下限は 0 より上だがドローダウンが悪化。勝ちとは言わない）/ " +
			"worsened（区間の上限が 0 より下）/ inconclusive（区間が 0 をまたぐ。差があるとも無いとも言えない）。judged は最後の 4 つのとき true。" +
			"min_trades は下限 64 で上げるのみ可。取引は同日・同相場に偏りうるので区間は楽観的になりうる。前後は別期間で、因果の証明にはならない。読み取り専用",
		schema: objectSchema(map[string]any{
			"experiment_id": map[string]any{"type": "integer", "minimum": 1},
			"min_trades":    minTradesProp(),
		}, "experiment_id"),
		noPeriod: true,
		decode: func(raw json.RawMessage) (periodArgs, runFunc, *toolError) {
			var a struct {
				ExperimentID *int64 `json:"experiment_id"`
				MinTrades    *int   `json:"min_trades"`
			}
			if err := strictDecode(raw, &a); err != nil {
				return periodArgs{}, nil, err
			}
			if a.ExperimentID == nil || *a.ExperimentID < 1 {
				return periodArgs{}, nil, fail(CodeInvalidArgument)
			}
			minTrades, err := resolveMinTrades(a.MinTrades)
			if err != nil {
				return periodArgs{}, nil, err
			}
			id := *a.ExperimentID
			return periodArgs{}, func(ctx context.Context, e toolEnv) (any, int, error) {
				exp, err := experiment.Get(ctx, e.q, id)
				if err != nil {
					return nil, 0, experimentFailureOf(err)
				}
				capital, err := e.paperCapital(ctx)
				if err != nil {
					return nil, 0, err
				}
				c, err := experiment.Compare(ctx, e.q, exp, e.now, minTrades, capital, true)
				return c, 2, err
			}, nil
		},
	},
}

func minTradesProp() map[string]any {
	return map[string]any{
		"type": "integer",
		"description": "判定に要る前後それぞれの取引件数。下限は " + strconv.Itoa(experiment.MinTradesFloor) + "（暫定・承認前）で、上げるのは可、下げるのは不可。" +
			"下限未満や " + strconv.Itoa(experiment.MaxMinTrades) + " 超は invalid_value。省略は下限",
	}
}

// resolveMinTrades validates min_trades; nil is "not given" (0). One below the
// floor is refused with invalid_value and the reason, never raised to the
// floor behind the caller's back.
func resolveMinTrades(requested *int) (int, *toolError) {
	if requested == nil {
		return 0, nil
	}
	if err := experiment.CheckMinTrades(*requested); err != nil {
		return 0, failDetail(CodeInvalidValue, err.Error())
	}
	return *requested, nil
}

// experimentFailureOf maps an experiment error to the tool's spec error;
// anything else is left to classify (busy, timeout, unavailable, internal).
func experimentFailureOf(err error) error {
	if reason, ok := experiment.IsInvalid(err); ok {
		return failDetail(CodeInvalidArgument, reason)
	}
	switch {
	case errors.Is(err, experiment.ErrMultipleVariables):
		return fail(CodeMultipleVariables)
	case errors.Is(err, experiment.ErrKeyNotAllowed):
		return fail(CodeKeyNotAllowed)
	case errors.Is(err, experiment.ErrNotFound):
		return fail(CodeExperimentNotFound)
	case errors.Is(err, experiment.ErrNotRunning):
		return fail(CodeExperimentNotRunning)
	case errors.Is(err, mcpset.ErrWriteDisabled):
		return fail(CodeWriteDisabled)
	}
	return err
}
