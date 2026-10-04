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

```json
// Output（抜粋）
{
  "buckets": [
    { "range": "0.50-0.60", "avg_confidence": 0.55, "direction_accuracy": 0.51, "avg_future_return_pct": -0.05,
      "sample_count": 80, "trade_count": 6, "total_pnl": -1800, "avg_pnl_pct": -0.21 },
    { "range": "0.60-0.70", "avg_confidence": 0.65, "direction_accuracy": 0.55, "avg_future_return_pct": 0.02,
      "sample_count": 64, "trade_count": 9, "total_pnl": 400, "avg_pnl_pct": 0.03 },
    { "range": "0.70-0.80", "avg_confidence": 0.75, "direction_accuracy": 0.63, "avg_future_return_pct": 0.11,
      "sample_count": 41, "trade_count": 12, "total_pnl": 5200, "avg_pnl_pct": 0.18 },
    { "range": "0.80-0.90", "avg_confidence": 0.85, "direction_accuracy": 0.71, "avg_future_return_pct": 0.24,
      "sample_count": 22, "trade_count": 7, "total_pnl": 6100, "avg_pnl_pct": 0.31 },
    { "range": "0.90-1.00", "avg_confidence": 0.94, "direction_accuracy": 0.78, "avg_future_return_pct": 0.39,
      "sample_count": 9, "trade_count": 3, "total_pnl": 3300, "avg_pnl_pct": 0.42 }
  ],
  "by_direction": [
    { "direction": "LONG", "sample_count": 120, "direction_accuracy": 0.62, "avg_future_return_pct": 0.14 },
    { "direction": "SHORT", "sample_count": 96, "direction_accuracy": 0.58, "avg_future_return_pct": 0.09 }
  ],
  "brier_score": 0.19,
  "log_loss": 0.52,
  "expected_calibration_error": 0.06
}
```

`by_direction`は予測方向（`LONG`/`SHORT`、常に両方を返す）別の方向別平均リターン（`avg_future_return_pct`は方向調整済み＝SHORTは下落が正）と的中率（FR-CAL-2）。バケットの`trade_count`/`total_pnl`/`avg_pnl_pct`はconfidence bucket別PnL（FR-CAL-2）で、`positions.entry_order_id` → `paper_orders.trade_signal_id` → `trade_signals.jev_decision_id`で辿れるTrader判断由来のクローズ済みポジションの件数・実現損益合計（JPY）・エントリー金額に対する平均リターン（%）。手動エントリーは含まない。

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
      "backtest_result": { "expectancy_delta_pct": 2.1, "max_drawdown_delta_pct": -3.4 },
      "reviewed_by": "opus",
      "review": { "verdict": "approve", "reason": "..." },
      "applied_policy_version": "v12"
    }
  ]
}
```

`policy_proposals`の保存値のうちJSONとして解釈できないものがあっても、その1行のせいで一覧全体を失敗させない（古い／壊れた行が監査履歴を隠さないため）。該当行は一覧から除外せず返し、解釈できなかったフィールドだけを空にする（`proposed_changes`は`{}`、`backtest_result`/`review`は`null`。`proposed_changes`は`new_value`が1つでもJSONでなければ`{}`）。各失敗は`proposal_id`付きでERRORログに記録する。`500`になるのは`policy_proposals`の読み込み自体が失敗した場合（ストア障害）のみ。

### GET /api/v1/activity

System Activity Log向けの直近アクティビティ・キュー状況スナップショット（`requirements/functional.md` §4.15/§5.5）。`jobs`/`jev_decisions`/`kill_switch_events`を集約する読み取り専用API。新規永続テーブルは持たない。

| クエリ | 型 | 説明 |
|-------|-----|------|
| `limit` | integer | フィード件数（既定200、1〜500。範囲外は422） |
| `queue` | string | `jobs.queue`でフィルタ（省略時は全キュー）。`job`イベントのみが対象で、指定時は`jev_scout`/`jev_trader`/`kill_switch`イベントは含まれない |
| `type` | string | イベント種別でフィルタ: `job` / `jev_scout` / `jev_trader` / `kill_switch`（省略時は全種別） |

```json
// Output（抜粋）
{
  "queues": [
    { "queue": "jev-scout", "pending": 3, "running": 1, "failed_recent": 0 }
  ],
  "events": [
    { "type": "jev_trader", "timestamp": "2026-09-29T01:15:00Z", "symbol": "7203", "detail": "direction=LONG confidence=0.74", "latency_ms": 820 },
    { "type": "kill_switch", "timestamp": "2026-09-29T01:10:00Z", "detail": "reason=daily_loss_limit" }
  ],
  "as_of": "2026-09-29T01:15:03Z"
}
```

`queues[].failed_recent`は`finished_at`が`as_of`から過去1時間（固定。設定では変更できない）以内の`failed`ジョブの件数で、`/ws/activity`の`job_update.failed_recent`も同じ窓で集計する。1時間より前に失敗したジョブは含まれない（`requirements/functional/components-platform.md` FR-ACT-1）。
