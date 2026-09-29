# API仕様: API ルート（§5）

`docs/api/endpoints.md` から分割した章（300行/ファイル制限のため）。節番号・内容は変更なし。認証・ミドルウェア（§1）・ルーティング概要（§2）・WebSocket（§6）・エラーレスポンス（§7）は `docs/api/endpoints.md` を参照。

## 5. API ルート（Huma, `/api/v1`）

Huma が OpenAPI 3.1 スペックを `/api/v1/openapi.json` に自動生成する。以下は主要エンドポイント。

### GET /api/v1/scanner

Fast Screener通過〜Jev Trader評価済みの候補銘柄一覧を返す。Scanner Dashboardの初期ロード・`pitha-scanner-table`のフォールバック取得に使用（ライブ更新は`/ws/scanner`）。

```json
// Output（抜粋）
{
  "items": [
    {
      "symbol": "7203",
      "price": 2831.5,
      "return_1m": 0.12,
      "return_5m": 0.42,
      "volume_ratio_5m": 3.4,
      "price_vs_vwap_bps": 38,
      "spread_bps": 7,
      "jev_direction": "LONG",
      "jev_confidence": 0.74,
      "entry_quality": "strong",
      "current_position": null
    }
  ],
  "as_of": "2026-09-26T10:15:00+09:00"
}
```

### GET /api/v1/symbols/{symbol}

Symbol Detail向け統合情報（価格・Jev判定・Riskパラメータ）。

```json
// Output（抜粋）
{
  "symbol": "7203",
  "price": 2831.5,
  "vwap": 2823.0,
  "jev": {
    "direction": "LONG",
    "confidence": 0.74,
    "regime": "BREAKOUT",
    "entry_quality": "strong",
    "toxic_flow": 0.18,
    "liquidity_stressed": 0.09
  },
  "risk": {
    "allowed_position_pct": 1.4,
    "stop_loss_pct": 0.6,
    "take_profit_pct": 1.2
  },
  "current_position": null
}
```

`risk`は固定値ではなく稼働中エンジンの実設定から取得する。`allowed_position_pct`は`config/risk.yaml`の`max_position_per_symbol_pct`（Risk Engineが使用中の区分）、`stop_loss_pct`/`take_profit_pct`は`execution.Config`（Exit条件）の値。

### GET /api/v1/symbols/{symbol}/candles

`pitha-price-chart`（lightweight-charts）用ローソク足＋VWAP＋出来高系列。

| クエリ | 型 | 説明 |
|-------|-----|------|
| `from` | string(RFC3339) | 取得開始時刻（省略時は`to`の6時間前） |
| `to` | string(RFC3339) | 取得終了時刻（省略時は現在） |
| `interval` | string | `1m` 固定（MVP。`1m`以外は422） |

パスの`{symbol}`は英数字1〜16文字（`^[0-9A-Za-z]+$`、`/symbols/{symbol}`系ルート共通）。`from`/`to`がRFC3339でない場合、`symbol`/`interval`が範囲外の場合はいずれも422。

### GET /api/v1/symbols/{symbol}/decisions

Decision history（`jev_decisions`をJev Scout/Trader別に時系列で返す）。新しい順。未登録銘柄は404。

| クエリ | 型 | 説明 |
|-------|-----|------|
| `limit` | integer | 件数上限（既定100、1〜500。範囲外は422） |

出力は`{"symbol": "7203", "items": [...]}`。各itemは`id`/`symbol`/`timestamp`（RFC3339）/`decision_type`（`scout`/`trader`）/`direction`/`confidence`/`regime`/`entry_quality`/`toxic_flow`/`liquidity_stressed`/`continuation_probability`/`question_version`/`model_id`/`latency_ms`。

`direction`〜`continuation_probability`は`decision_type`が`trader`の行のみ値を持ち、`scout`行では`null`。

### GET /api/v1/signals / GET /api/v1/signals/{symbol}

`trade_signals`の一覧・銘柄別履歴（`risk_passed`, `reject_reason`含む）。新しい順。`/signals/{symbol}`の未登録銘柄は404。クエリ `limit`（既定100、1〜500。範囲外は422）。パスの`{symbol}`は英数字1〜16文字（`^[0-9A-Za-z]+$`、違反は422）。

```json
// Output（抜粋）
{
  "items": [
    {
      "id": 3, "symbol": "7203", "timestamp": "2026-09-27T09:31:00Z",
      "direction": "LONG", "score": 0.74, "entry_price_reference": 2831.5,
      "policy_version": "v1", "risk_passed": false, "reject_reason": "spread_too_wide",
      "jev_decision_id": 2
    }
  ]
}
```

### GET /api/v1/positions

現在保有中および直近クローズ済みポジション一覧。

| クエリ | 型 | 説明 |
|-------|-----|------|
| `limit` | integer | 件数上限（既定100、1〜500。範囲外は422） |

### GET /api/v1/orders

`paper_orders`一覧（ステータスフィルタ `?status=` 対応）。

| クエリ | 型 | 説明 |
|-------|-----|------|
| `status` | string | `PENDING`/`FILLED`/`CANCELLED`/`REJECTED`でフィルタ（省略時は全件。それ以外は422） |
| `limit` | integer | 件数上限（既定100、1〜500。範囲外は422） |

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
      "proposed_changes": { "policy.long.min_confidence": 0.68 },
      "backtest_result": { "expectancy_delta_pct": 2.1, "max_drawdown_delta_pct": -3.4 },
      "reviewed_by": "opus",
      "review": { "verdict": "approve", "reason": "..." },
      "applied_policy_version": "v12"
    }
  ]
}
```

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

### GET /api/v1/system/status / POST /api/v1/system/pause / resume / kill

Kill Switchの状態取得（読み取り専用の`GET`）と操作。`pitha-kill-switch-panel`が再接続・自動発動通知後の再同期に`GET`を、`window.confirm`確認後の操作に`POST`を呼ぶ（外部スクリプトからも利用可）。HTMX用の同名アクションルートは持たない。どれも`{"state":"running","can_pause":true,"can_resume":false,"can_kill":true}`の形式で（`POST`は更新後の）状態を返す。`state`は`running`/`paused`/`killed`、`can_*`は現在の`state`から各`POST`が有効な遷移か。

### エンドポイント一覧表

| メソッド | パス | 概要 |
|---------|------|------|
| GET | `/api/v1/scanner` | 候補銘柄一覧 |
| GET | `/api/v1/symbols/{symbol}` | 銘柄詳細 |
| GET | `/api/v1/symbols/{symbol}/candles` | チャート用系列データ |
| GET | `/api/v1/symbols/{symbol}/decisions` | Jev判断履歴 |
| GET | `/api/v1/signals` | トレードシグナル一覧 |
| GET | `/api/v1/signals/{symbol}` | 銘柄別シグナル履歴 |
| GET | `/api/v1/positions` | ポジション一覧 |
| GET | `/api/v1/orders` | 注文一覧 |
| GET | `/api/v1/performance` | 実績集計 |
| GET | `/api/v1/calibration` | Calibrationバケット集計 |
| GET | `/api/v1/policy-proposals` | Sol/Opus自己改善ループの提案・レビュー履歴（監査用） |
| GET | `/api/v1/system/status` | システム状態と許可される操作の取得（読み取り専用） |
| POST | `/api/v1/system/pause` | 一時停止 |
| POST | `/api/v1/system/resume` | 再開 |
| POST | `/api/v1/system/kill` | Kill Switch発動 |
| GET | `/api/v1/openapi.json` | OpenAPI 3.1スペック（Huma自動生成） |
| GET | `/api/v1/activity` | System Activity Log向けキュー状況・直近アクティビティ |
