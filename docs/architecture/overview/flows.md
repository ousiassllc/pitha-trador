# アーキテクチャ設計: 通信フロー・障害対応（§10〜§11）

`docs/architecture/overview.md` から分割した章。§10 通信フロー（§10.1〜§10.5）/ §11 障害対応方針。節番号は分割前と同一で、コードコメント等の `overview.md §<番号>` は本ファイルの同番号の節を指す。

## 10. 通信フロー

### 10.1 起動時フロー

```mermaid
sequenceDiagram
    participant App as Wailsアプリ起動
    participant KABU as kabuステーションAPI
    participant DB as SQLite
    participant SCHED as Scheduler（自前Worker）

    App->>DB: マイグレーション適用確認（golang-migrate）・接続初期化（PRAGMA foreign_keys=ON, WAL）
    App->>KABU: /kabusapi/token でトークン発行
    KABU-->>App: token
    App->>KABU: 対象ユニバース銘柄登録・PUSH購読開始
    App->>SCHED: 周期ジョブ登録（60s/15-30s/5-15s）。前回クラッシュ時の`running`状態ジョブを`pending`へ復帰
    App->>App: WebView起動・Scanner Dashboard表示
```

### 10.2 スキャン〜発注フロー

`requirements/functional.md` §2 主要処理フロー（シーケンス図）を参照。アーキテクチャ上の要点は以下。

- Scheduler（自前Worker、`jobs`テーブル）が `market-data` → `feature-calc` → `jev-scout` → `jev-trader` の順にジョブをenqueueし、各Serviceがdomainモデルを介して疎結合に連携する
- `jev-scout`/`jev-trader`の直前にRAG Context Builder（§7）が類似局面を検索し文脈を付与する
- Risk判定・Paper発注は独立したキュー（ジョブ）を持たず、`jev-trader`ジョブ内でPolicy Engine → Risk Engine → Execution（Paper）を同期実行する。Risk Engineは必ずPolicy Engineの直後に評価され、Risk Engineの承認なしにExecutionへは到達しない
- `outcome-labeling` / `analytics` は約定・Exit後に非同期実行し、UIの応答性に影響を与えない

### 10.3 Kill Switchフロー（発動〜再開）

```mermaid
sequenceDiagram
    participant RE as Risk Engine
    participant EX as Execution
    participant DB as SQLite
    participant TRAY as Wails通知/トレイ
    participant SLACK as Slack Webhook

    RE->>RE: 日次損失上限/連敗上限/異常検知/ハートビート途絶を検出
    RE->>DB: kill_switch_events登録（reason, detail_json）
    RE->>EX: 新規エントリー停止指示
    RE->>TRAY: ネイティブ通知発火
    RE->>SLACK: Webhook通知送信（reason・自動/手動再開区分を含む）
    opt reasonが daily_loss_limit / unexpected_position / fill_discrepancy / consecutive_losses / db_write_failure / broker_api_error / operator_manual
        RE->>EX: 保有ポジション強制クローズ指示（必要な場合。未解除かつ建玉が残る間は1分周期で再実行する）
    end
    alt 自動再開対象（market_data_down / jev_api_down / operator_heartbeat_timeout）
        RE->>RE: 発動条件の解消を定期監視
        RE->>DB: kill_switch_resolutions に resolved_by=auto の解除行を追記
        RE->>EX: 新規エントリー再開
        RE->>SLACK: 自動再開を通知
    else 手動再開対象（daily_loss_limit / unexpected_position / fill_discrepancy / consecutive_losses / db_write_failure / broker_api_error / operator_manual）
        Note over RE: オペレーターがUI（pitha-kill-switch-panel）で明示的にresumeするまで停止を維持。consecutive_losses / daily_loss_limit のResumeは再開ベースラインを記録し、以後の連敗数・日次実現損失はその時刻以降のクローズ分のみで判定する（再発動ループの防止。`requirements/functional/components-pipeline.md` FR-RISK-7）
    end
```

検知（市場データ停止・Jev API異常・Broker API異常・想定外ポジション・約定差異・DB書き込み失敗・日次損失上限・連敗上限）と自動再開の解消監視は、SchedulerのCronトリガー（各1分周期、`WithRiskMonitor`/`WithAutoResumer`）が`risk.Engine.RunPeriodicChecks`/`AutoResume`を呼ぶことで実行する。日次損失上限（`daily_loss_limit`、`CheckDailyLossLimit`）・連敗上限（`consecutive_losses`、`CheckConsecutiveLosses`）はシグナル到来を待たずこの周期処理でも評価され、到達時はKill Switch発動＋強制決済を伴う（Policy Engine候補ごとの判定と同一の上限）。「日次損失接近」（`CheckDailyLossWarning`）は警告のみでKill Switchは発動しない。同じ周期処理で、Kill Switch未解除かつ建玉が残る間の強制決済再試行（`RetryForceClose`）も実行する。判定基準の詳細は`requirements/functional.md` FR-RISK-2/FR-RISK-7を参照。

### 10.4 操作者ハートビート監視（dead-man's switch、Live専用）

```mermaid
sequenceDiagram
    participant MW as 認証済みリクエストMiddleware
    participant RE as Risk Engine
    participant DB as SQLite

    MW->>DB: 認証済みUIリクエストのたびに last_ui_heartbeat_at を更新
    loop 立会時間中、周期チェック（自前Worker）
        RE->>DB: last_ui_heartbeat_at を参照
        alt now - last_ui_heartbeat_at > heartbeat_timeout_minutes（Live初期値120分）
            RE->>RE: reason=operator_heartbeat_timeout でKill Switch発動（§10.3へ）
        else 正常
            RE->>RE: 何もしない
        end
    end
```

- 自動再開（解消）は発動時刻より後に記録された本物のハートビートが`heartbeat_timeout_minutes`以内にあることを条件とする。発動判定の寄り付きクランプ（最後のハートビートを当日の寄り付きに切り上げる）は解消判定には使わない（寄り付き前は経過時間が負になり、操作者不在でも毎朝解消されてしまうため）
- ハートビートは有効なセッションCookieを持つ認証済みリクエスト（ページ/アクション/API呼び出し）を更新対象とする（`internal/router.WithHeartbeatRecorder` でSessionミドルウェアの直後に登録）。Cookieを持たないリクエストは対象外とし、外部からの無認証GETでdead-man's switchを延命できないようにする
- 操作者の操作ではないリクエストは更新対象外とする: `/static/...`、WebSocketのUpgrade（画面が自動で再接続する）、画面が自動ポーリングするルート（現状 `GET /system/update-status`、`hx-trigger="every 60s"`）
- 自動発火の再同期リクエストも更新対象外とする（FR-RISK-6）。`pitha-kill-switch-panel` の再同期（初回・`kill_switch` push受信時・WebSocket再接続後の `GET /api/v1/system/status`）と、`systemStateChanged` を契機とするHeaderの `#header-status`（`GET /system/status`）は専用ヘッダ `X-Pitha-Background: 1`（`middleware.BackgroundHeader`、Lit側は `lib/api.ts` の `get(path, { background: true })`、htmx側は `hx-headers`）を付け、Heartbeatミドルウェアが除外する。これらは操作者不在でも発火するため、`operator_heartbeat_timeout` のKill Switch発動後のpushが自らハートビートを更新し、`AutoResume` が無人のまま解除してしまうことを防ぐ
- 書き込みはスロットリングする: ミドルウェアが最終記録時刻をメモリ保持し、10秒以内の更新対象リクエストではSQLiteへ書き込まない（タイムアウトは分単位のため精度に影響しない。書き込み失敗時は次のリクエストで即再試行する）
- Paper Trading運用中は実資金リスクがないためハートビート監視を適用しない（`requirements/functional.md` §4.7 表の heartbeat_timeout_minutes は Live のみ設定）

### 10.5 初回セットアップ誘導

`requirements/functional.md` §4.18（FR-SETUP-1〜5）の実装詳細。

```mermaid
sequenceDiagram
    participant UI as WebView
    participant SG as Setup Guard Middleware
    participant DB as secrets テーブル
    participant SET as /setup（Settings Handler）

    UI->>SG: 任意のリクエスト（例: GET /scanner）
    SG->>DB: JEV_API_KEY/JEV_BASE_URL/KABU_API_PASSWORDの有無を確認
    alt いずれか未設定（または読み出し失敗）
        SG-->>UI: 302 /setup（HTMXは204+HX-Redirect、/api/v1は503 JSON、WebSocketは403）
        UI->>SET: GET /setup
        UI->>SET: POST /settings/:key（`SecretFieldRow`の保存。`/setup`・`/static/...`と同じくガード対象外）
        SET->>DB: 暗号化保存
    else 3キーとも設定済み
        SG->>SG: 通常のルートへ委譲（リダイレクトなし）
    end
```

- ガードは全ルート（ページ・アクション・`/api/v1`・WebSocket・404含む）の手前に置き、`/setup`・`POST`/`DELETE /settings/:key`・`/static/...`のみ通す。判定はリクエストごとにDBを参照し、状態を保持しないため、3キーが揃った次のリクエストから自動で解除される（アプリ再起動は不要）
- `/setup`はSettings画面と同じ`SecretFieldRow`・同じ`POST`/`DELETE /settings/:key`を使い、専用の保存実装を持たない。完了後も直接アクセスして再設定できる
- `/setup`は`Header`（ガード対象の`hx-get`フラグメントを持つ）を含まない専用レイアウト（`SetupShell`）で描画する
- 各種サービスは従来通り起動時の値を読むため、保存した認証情報の反映にはアプリ再起動が必要（§5）

## 11. 障害対応方針

| 障害 | 対応 |
|------|------|
| Jev API失敗 | 1回目リトライ→2回目以降exponential backoff→継続失敗でnew entry停止。既存ポジションはコードベースExit Ruleで継続管理 |
| Market Data欠損 | stale data判定→該当銘柄の新規取引禁止 |
| kabuステーションAPI異常 | Kill Switch発動条件に該当。新規取引停止、必要に応じ強制決済 |
| DB書き込み失敗継続 | Kill Switch発動条件に該当 |
| Wailsプロセスクラッシュ | プロセス監視による自動再起動。再起動中は新規エントリー停止（既存ポジションはkabuステーション側の待機注文/手動介入を前提）。再起動後、`jobs`テーブルの中断ジョブを`pending`へ復帰させ処理を再開する |
