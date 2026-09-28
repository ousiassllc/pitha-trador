# API 仕様

HALTアーキテクチャの3パターン（ページルート/アクションルート/APIルート）に従う。詳細な設計思想は `components/overview.md` を参照。

## 1. 認証・アクセス制御

- Wailsアプリ内蔵HTTPサーバーは `127.0.0.1` にのみバインドし、外部ネットワークからは到達不能（`requirements/non-functional.md` §4）
- 単一ユーザー・単一デスクトップアプリのため、外部IdP連携やユーザーログイン画面は持たない
- 起動時にWailsプロセスがランダムなローカルセッショントークンを生成し、Cookie（`HttpOnly`, `SameSite=Strict`）としてWebViewに設定する。全ての状態変更リクエスト（アクションルート・Huma APIのPOST/PUT/PATCH/DELETE）はこのセッションCookie必須とする
- HTMXフォームにはCSRFトークンをmetaタグ経由で付与し、`X-CSRF-Token`ヘッダで送信する（`components/overview.md` セキュリティ節）
- 実売買（Phase 7）移行時は、Kill Switch解除・発注確定操作にOS認証の追加確認を導入する（`requirements/non-functional.md` §4）
- **Setup Guard**: 必須認証情報（`JEV_API_KEY`/`JEV_BASE_URL`/`KABU_API_PASSWORD`）のいずれかが未設定の場合、Middlewareが`/setup`・`POST/DELETE /settings/:key`・静的アセット配信以外への全リクエストを`/setup`へ302リダイレクトする（`requirements/functional.md` FR-SETUP-1、`architecture/overview.md` §10.5）

## 2. ルーティング概要

| パターン | 例 | HX-Request分岐 | 返却 | 登録先 |
|---------|-----|----------------|------|--------|
| ページルート | `/scanner`, `/symbols/:symbol` | する | フルページ or フラグメント | Gin |
| アクションルート | `/system/pause` 等 | しない | フラグメントのみ | Gin |
| APIルート | `/api/v1/...` | しない | JSON | Huma |
| WebSocket | `/ws/scanner` 等 | 該当なし | JSONメッセージ | Gin (`github.com/coder/websocket`) |

## 3. ページルート

| メソッド | パス | 説明 |
|---------|------|------|
| GET | `/` | `/scanner` へリダイレクト |
| GET | `/scanner` | Scanner Dashboard。HX-Requestありなら候補テーブルフラグメントのみ返却 |
| GET | `/symbols/:symbol` | Symbol Detail。`<pitha-price-chart>` 等のLitアイランドを埋め込んだフルページ |
| GET | `/performance` | Performance画面。クエリ `from`/`to`（YYYY-MM-DD、JST、`to`含む）・`training_days`/`validation_days`/`forward_days`（既定5/2/1）指定時は記録済みデータでWalk Forwardバックテスト（FR-BT-1〜3）を実行し結果を表示する。不正入力は400 |
| GET | `/calibration` | Calibration画面 |
| GET | `/setup` | 初回セットアップ画面。必須認証情報未設定時は他の全ページからここへリダイレクトされる |

## 4. アクションルート

| メソッド | パス | 説明 | 返却 |
|---------|------|------|------|
| POST | `/system/pause` | 新規エントリー一時停止（Kill Switchとは別。手動での一時停止） | システム状態バッジ（OOB） |
| POST | `/system/resume` | 一時停止解除 | システム状態バッジ（OOB） |
| POST | `/system/kill` | Kill Switch手動発動（確認モーダル経由） | システム状態バッジ＋トースト（OOB） |
| GET | `/system/status` | システム状態バッジのフラグメント再取得（Lit→HTMX間接連携: `systemStateChanged`イベント受信時にHeaderが呼び出す） | システム状態バッジ |
| POST | `/positions/:id/close` | 手動決済（成行Paper Exit） | ポジション行フラグメント |

### システム状態遷移（アクションルート）

```mermaid
stateDiagram-v2
    [*] --> Running
    Running --> Paused: POST /system/pause
    Paused --> Running: POST /system/resume
    Running --> Killed: POST /system/kill\nまたはRisk Engine自動発動
    Paused --> Killed: POST /system/kill\nまたはRisk Engine自動発動
    Killed --> Running: 手動解除（要確認operation, Phase 7以降追加認証）
```

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

### GET /api/v1/symbols/{symbol}/candles

`pitha-price-chart`（lightweight-charts）用ローソク足＋VWAP＋出来高系列。

| クエリ | 型 | 説明 |
|-------|-----|------|
| `from` | string(RFC3339) | 取得開始時刻 |
| `to` | string(RFC3339) | 取得終了時刻（省略時は現在） |
| `interval` | string | `1m` 固定（MVP） |

### GET /api/v1/symbols/{symbol}/decisions

Decision history（`jev_decisions`をJev Scout/Trader別に時系列で返す）。

### GET /api/v1/signals / GET /api/v1/signals/{symbol}

`trade_signals`の一覧・銘柄別履歴（`risk_passed`, `reject_reason`含む）。

### GET /api/v1/positions

現在保有中および直近クローズ済みポジション一覧。

### GET /api/v1/orders

`paper_orders`一覧（ステータスフィルタ `?status=` 対応）。

### GET /api/v1/performance

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
  "signal_count": 342
}
```

### GET /api/v1/calibration

```json
// Output（抜粋）
{
  "buckets": [
    { "range": "0.50-0.60", "direction_accuracy": 0.51, "avg_future_return_pct": -0.05 },
    { "range": "0.60-0.70", "direction_accuracy": 0.55, "avg_future_return_pct": 0.02 },
    { "range": "0.70-0.80", "direction_accuracy": 0.63, "avg_future_return_pct": 0.11 },
    { "range": "0.80-0.90", "direction_accuracy": 0.71, "avg_future_return_pct": 0.24 },
    { "range": "0.90-1.00", "direction_accuracy": 0.78, "avg_future_return_pct": 0.39 }
  ],
  "brier_score": 0.19,
  "log_loss": 0.52,
  "expected_calibration_error": 0.06
}
```

### POST /api/v1/system/pause / resume / kill

アクションルート（`/system/...`）のJSON版。外部監視ツール・スクリプトからの操作用に提供する（HTMX UIは同機能をアクションルート経由で呼ぶ）。

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
| POST | `/api/v1/system/pause` | 一時停止 |
| POST | `/api/v1/system/resume` | 再開 |
| POST | `/api/v1/system/kill` | Kill Switch発動 |
| GET | `/api/v1/openapi.json` | OpenAPI 3.1スペック（Huma自動生成） |

## 6. WebSocket

| パス | 用途 | 送信メッセージ例 |
|------|------|-----------------|
| `/ws/scanner` | Scanner Dashboardのライブ更新（`pitha-scanner-table`） | `{"type":"scanner_update","items":[...]}` |
| `/ws/symbols/{symbol}` | Symbol Detailのライブ更新（`pitha-price-chart`, Jev判定パネル） | `{"type":"tick","price":2831.5,...}` / `{"type":"jev_update","direction":"LONG",...}` |
| `/ws/system` | Kill Switch発動等のシステムイベント通知（ヘッダーバッジ用、OOBの代替としてLit非経由でも利用可） | `{"type":"kill_switch","reason":"daily_loss_limit"}` |

WebSocketクライアント実装は `components/overview.md` の `lib/ws.ts`（自動再接続、指数バックオフ）を必ず経由する。

## 7. エラーレスポンス

- Huma APIのバリデーションエラーはRFC 7807 Problem Details形式で自動生成される（`components/overview.md` Huma APIパターン参照）
- ビジネスエラー（例: Risk Engine拒否によりKill Switch解除不可）はカスタムエラーも同じProblem Details形式に統一する
- アクションルート（HTMX）のエラーはフォーム再レンダリング（422）またはOOBトースト（403/5xx）で返す（`components/overview.md` エラーハンドリング節）

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-09-26 | 新規作成 | 初版 |
| 1.1 | 2026-09-28 | §3 `/performance` にWalk Forwardバックテスト実行クエリを追記 | #53 バックテスト実行導線 |
| 1.2 | 2026-09-29 | §1にSetup Guardの説明を追記。§3に`GET /setup`を追加 | 環境設定項目未入力時のセットアップ画面誘導 |
