# API仕様: API ルート（§5 続き: 実績・Calibration・自己改善・アクティビティ）

`docs/api/endpoints/huma-api.md`（§5）から分割した節（300行/ファイル制限のため）。節番号・内容は変更なし。Humaの共通方針（OpenAPIスペック・エラー形式・ページング等）、スキャナ・銘柄・シグナル・ポジション・注文・システム状態・エラーログの各ルートとエンドポイント一覧表は `docs/api/endpoints/huma-api.md` を参照。

### GET /api/v1/performance

全クローズ済みポジション（`positions.closed_at`あり）の実績集計。`total_pnl`/`daily_pnl`は`realized_pnl`の合計（`daily_pnl`はJST当日0時以降にクローズしたもの）。`win_rate`/`expectancy`/`max_drawdown_pct`はバックテスト（`internal/service/backtest.Aggregate`）と同じ定義で、各ポジションのエントリー約定額に対する損益率（%）から算出する。`profit_factor`は総利益÷総損失（損失なしは`null`）、`sharpe_ref`/`sortino_ref`はトレードごとリターンの平均÷標準偏差／下方偏差（年率換算なし、算出不能時は`null`）、`signal_count`はLONG/SHORTの`trade_signals`件数。

```json
// Output（抜粋）
{
  "total_pnl": 128340,
  "daily_pnl": 15200,
  "win_rate": 0.57,
  "profit_factor": 1.82,
  "expectancy": 0.34,
  "max_drawdown_pct": 4.1,
  "average_hold_time_minutes": 14.2,
  "sharpe_ref": 1.1,
  "sortino_ref": 1.6,
  "trade_count": 12,
  "signal_count": 342
}
```

### GET /api/v1/calibration

| クエリ | 型 | 説明 |
|-------|-----|------|
| `horizon` | string | 集計する判定水平線（分）: `5` / `10` / `15` / `all`（既定`all`）。`all`は現行水平線`5`/`10`/`15`のラベルをまとめて集計する。範囲外（`20`・数値以外・`0`等）は422 |

```json
// Output（抜粋）
{
  "horizon": "all",
  "buckets": [
    { "range": "0.50-0.60", "avg_confidence": 0.55, "direction_accuracy": 0.51, "avg_future_return_pct": -0.05,
      "sample_count": 80, "trade_count": 6, "total_pnl": -1800, "avg_pnl_pct": -0.21, "trade_win_rate": 0.5 },
    { "range": "0.60-0.70", "avg_confidence": 0.65, "direction_accuracy": 0.55, "avg_future_return_pct": 0.02,
      "sample_count": 64, "trade_count": 9, "total_pnl": 400, "avg_pnl_pct": 0.03, "trade_win_rate": 0.5 },
    { "range": "0.70-0.80", "avg_confidence": 0.75, "direction_accuracy": 0.63, "avg_future_return_pct": 0.11,
      "sample_count": 41, "trade_count": 12, "total_pnl": 5200, "avg_pnl_pct": 0.18, "trade_win_rate": 0.5 },
    { "range": "0.80-0.90", "avg_confidence": 0.85, "direction_accuracy": 0.71, "avg_future_return_pct": 0.24,
      "sample_count": 22, "trade_count": 7, "total_pnl": 6100, "avg_pnl_pct": 0.31, "trade_win_rate": 0.5 },
    { "range": "0.90-1.00", "avg_confidence": 0.94, "direction_accuracy": 0.78, "avg_future_return_pct": 0.39,
      "sample_count": 9, "trade_count": 3, "total_pnl": 3300, "avg_pnl_pct": 0.42, "trade_win_rate": 0.5 }
  ],
  "by_direction": [
    { "direction": "LONG", "sample_count": 120, "direction_accuracy": 0.62, "avg_future_return_pct": 0.14 },
    { "direction": "SHORT", "sample_count": 96, "direction_accuracy": 0.58, "avg_future_return_pct": 0.09 }
  ],
  "brier_score": 0.19,
  "log_loss": 0.52,
  "expected_calibration_error": 0.06,
  "trade_count": 20,
  "pnl_brier_score": 0.27,
  "pnl_log_loss": 0.74
}
```

`by_direction`は予測方向（`LONG`/`SHORT`、常に両方を返す）別の方向別平均リターン（`avg_future_return_pct`は方向調整済み＝SHORTは下落が正）と的中率（FR-CAL-2）。バケットの`trade_count`/`total_pnl`/`avg_pnl_pct`はconfidence bucket別PnL（FR-CAL-2）で、`positions.entry_order_id` → `paper_orders.trade_signal_id` → `trade_signals.jev_decision_id`で辿れるTrader判断由来のクローズ済みポジションの件数・実現損益合計（JPY）・エントリー金額に対する平均リターン（%）。手動エントリーは含まない。指標は`jev_decisions.question_version`で分離せず、全版のTrader判断（`trader-v2`/`trader-v3`等）を混在して集計する（版別・現行版のみの絞り込みやクエリは無い）。

実現トレードPnLをground truthとする評価（FR-CAL-5）: バケットの`trade_win_rate`は`trade_count`のうち`realized_pnl > 0`の割合（`trade_count`が0なら0）、トップレベルの`trade_count`は`pnl_brier_score`/`pnl_log_loss`の対象となったクローズ済みポジション数（confidenceがどのバケットにも入らないものを除く。0件なら両スコアも0）。両スコアはconfidenceを予測確率、`realized_pnl > 0`を結果としたBrier Score/Log Loss。`realized_pnl`は両約定の手数料控除後・約定モデル（FR-ENTRY-8）の呼値/スプレッド/滑りを織り込んだ値。判定水平線の既定は5/10/15分。

`horizon`（issue #719）はビン・方向別・Brier/Log Loss/ECEの全指標を、指定した水平線の`calibration_outcomes`行だけで再計算する（`ListLabeledSamples`が`horizon_minutes`で絞る）。応答の`horizon`は集計した水平線をそのまま返す（画面の表示用）。`all`は`calibration.DefaultHorizonsMinutes`（5/10/15）のみを合算し、#711以前の旧20分ラベルは混ざらない（旧ラベルはAPIからも選べない）。バケットの`trade_count`/`total_pnl`/`avg_pnl_pct`/`trade_win_rate`と`pnl_brier_score`/`pnl_log_loss`は実現トレードPnLに基づくためホライズンに依存せず、どの`horizon`でも同じ値を返す。

### GET /api/v1/policy-proposals

Sol/Opus自己改善ループ（`architecture/overview.md` §8）の監査用読み取り専用API。`policy_proposals`の提案・レビュー・適用・ロールバック履歴を返す。UIページは持たず、外部監視・手動確認用に提供する（実際の外部AI API呼び出しの結果を追跡できるようにするため、`requirements/functional.md` FR-SELFIMPROVE-7〜9）。

| クエリ | 型 | 説明 |
|-------|-----|------|
| `status` | string | `pending`/`approved`/`rejected`/`applied`/`rolled_back`でフィルタ（省略時は全件） |
| `limit` | integer | 件数上限（既定50、最大200） |

```json
// Output（抜粋）
{
  "items": [
    {
      "id": 42,
      "proposed_at": "2026-09-28T15:00:00Z",
      "proposed_by": "sol",
      "status": "applied",
      "proposed_changes": { "policy.long.min_probability": 0.68 },
      "backtest_result": { "baseline_expectancy": 0.31, "candidate_expectancy": 0.3165, "baseline_max_drawdown_pct": 5.0, "candidate_max_drawdown_pct": 4.83, "expectancy_delta_pct": 2.1, "max_drawdown_delta_pct": -3.4 },
      "reviewed_by": "opus",
      "review": { "verdict": "approve", "reason": "..." },
      "applied_policy_version": "sol-42",
      "applied_at": "2026-09-28T16:00:00Z",
      "rolled_back_at": null,
      "rolled_back_reason": null
    }
  ]
}
```

`backtest_result`はシャドウバックテスト（FR-SELFIMPROVE-4）の比較結果で、実行前は`null`。基準値・候補値の`baseline_expectancy`/`candidate_expectancy`/`baseline_max_drawdown_pct`/`candidate_max_drawdown_pct`と、相対変化率`expectancy_delta_pct`（`(candidate - baseline) / |baseline| * 100`。基準Expectancyが負でも改善なら正になる）・`max_drawdown_delta_pct`（`(candidate - baseline) / baseline * 100`）の計6フィールドを常に出力する。基準値（`baseline_expectancy`/`baseline_max_drawdown_pct`）が0のときは変化率を定義できないため、該当するデルタのみ`null`になる（`internal/web/handler/proposals`の`relativeDeltaPct`）。

`applied_at`（適用日時、RFC 3339）・`rolled_back_at`（FR-SELFIMPROVE-6の自動ロールバック日時、RFC 3339）・`rolled_back_reason`（ロールバック理由。劣化前後の実現Expectancy値を含む文字列）は`policy_proposals`の同名カラムの保存値で、未発生（未適用／ロールバックされていない）の場合は`null`（キーは常に出力する）。`status=rolled_back`の行で「いつ・なぜ」ロールバックされたかを本APIで確認できる（FR-SELFIMPROVE-7）。

`policy_proposals`の保存値のうちJSONとして解釈できないものがあっても、その1行のせいで一覧全体を失敗させない（古い／壊れた行が監査履歴を隠さないため）。該当行は一覧から除外せず返し、解釈できなかったフィールドだけを空にする（`proposed_changes`は`{}`、`backtest_result`/`review`は`null`。`proposed_changes`は`new_value`が1つでもJSONでなければ`{}`）。各失敗は`proposal_id`付きでERRORログに記録する。`500`になるのは`policy_proposals`の読み込み自体が失敗した場合（ストア障害）のみ。

### GET /api/v1/activity

System Activity Log向けの直近アクティビティ・キュー状況スナップショット（`requirements/functional.md` §4.15/§5.5）。`jobs`/`jev_decisions`/`kill_switch_events`を集約する読み取り専用API。新規永続テーブルは持たない。

| クエリ | 型 | 説明 |
|-------|-----|------|
| `limit` | integer | フィード件数（既定200、1〜500。範囲外は422） |
| `queue` | string | `jobs.queue`でフィルタ（省略時は全キュー）。許容値は`market-data`/`feature-calc`/`jev-scout`/`jev-trader`/`outcome-labeling`/`analytics`の6種で、範囲外は422。`job`イベントのみが対象で、指定時は`jev_scout`/`jev_trader`/`kill_switch`イベントは含まれない |
| `type` | string | イベント種別でフィルタ: `job` / `jev_scout` / `jev_trader` / `kill_switch` / `news_feed`（ニュース取得・Luna分類の失敗。インメモリ直近50件）/ `broker_notice`（ブローカーアダプタの運用者向け通知。立花の再認証遅延・取り合い・書面未読・API版数/書面更新予告。インメモリ直近50件。省略時は全種別。範囲外は422） |

```json
// Output（抜粋）
{
  "queues": [
    { "queue": "jev-scout", "pending": 3, "running": 1, "failed_recent": 0 }
  ],
  "events": [
    { "type": "job", "timestamp": "2026-09-29T01:16:00Z", "queue": "jev-scout", "detail": "queue=jev-scout status=succeeded attempts=1", "latency_ms": 1500 },
    { "type": "jev_trader", "timestamp": "2026-09-29T01:15:00Z", "symbol": "7203", "detail": "direction=LONG confidence=0.74", "latency_ms": 820 },
    { "type": "kill_switch", "timestamp": "2026-09-29T01:10:00Z", "detail": "reason=daily_loss_limit" }
  ],
  "as_of": "2026-09-29T01:15:03Z"
}
```

`events[].queue`は`type=job`のイベントにのみ付く（`jobs.queue`の値。それ以外のイベントではキーごと省略）。`/ws/activity`の`activity_event.event`も同じ構造。

`queues[].failed_recent`は`finished_at`が`as_of`から過去1時間（固定。設定では変更できない）以内の`failed`ジョブの件数で、`/ws/activity`の`job_update.failed_recent`も同じ窓で集計する。1時間より前に失敗したジョブは含まれない（`requirements/functional/components-platform.md` FR-ACT-1）。
