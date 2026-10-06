# アーキテクチャ設計: 通信フロー・障害対応（§10〜§11）

`docs/architecture/overview.md` から分割した章。§10 通信フロー（§10.1〜§10.6）/ §11 障害対応方針。節番号は分割前と同一で、コードコメント等の `overview.md §<番号>` は本ファイルの同番号の節を指す。

## 10. 通信フロー

### 10.1 起動時フロー

```mermaid
sequenceDiagram
    participant App as Wailsアプリ起動
    participant KABU as kabuステーションAPI
    participant DB as SQLite
    participant SCHED as Scheduler（自前Worker）

    App->>App: `startup.RunMain`でプロセスを開始（`cmd/desktop`・`cmd/server`共通。日次JSONログを`PITHA_LOG_DIR`（未設定ならDBと同じ親ディレクトリの絶対パス`logs/`。作業ディレクトリに依存しない）へ設定し、以降の`run`が返した致命的エラーはERRORで記録して終了コード1にする。ログ用ディレクトリ・ファイルを作れない場合は標準エラー出力へフォールバックして起動を継続。#547）
    App->>App: 多重起動ロック`app.lock`を取得（`bootstrap.AcquireInstanceLock`。DBを開く前。取得失敗時は`bootstrap.Run`・`Recover`に到達せず終了。desktopは終了コード0、serverは非0）
    App->>DB: マイグレーション適用確認（golang-migrate）・接続初期化（PRAGMA foreign_keys=ON, WAL）
    App->>SCHED: 前回クラッシュ時の`running`状態ジョブを`pending`へ復帰（`Scheduler.Recover`）
    App->>App: 銘柄マスタCSVから`instruments`をupsert（`syncUniverse`。失敗・CSV不在はログのみで継続。kabuステーション不達の影響を受けない）
    App->>KABU: /kabusapi/token でトークン発行
    KABU-->>App: token（失敗しても起動を継続し、バックグラウンドで再試行）
    App->>SCHED: robfig/cronへ周期ジョブ登録（分単位以上の保守ジョブ。60秒フルスキャンは`scan.full_scan_enabled: true`のときだけ登録し、既定ではオフ＝登録しない。FR-SCHED-7）
    App->>App: 候補更新ループ（15-30秒、`candidates.Run`）と保有ポジション再評価ループ（5-15秒、`heldposition.Monitor.Run`）を別goroutineで起動（cronのジョブ登録ではない）。Luna（既定Jev。`LUNA_BASE_URL`で差し替え可）が使え、かつニュースフィードが`NEWS_FEED_ENABLED=off`でない場合のみ（既定ではJevキーだけで有効）、News Ingestのティッカー（`newsIngestTicker`。起動直後に1回、以降1分周期。§13）も別goroutineで起動する
    App->>App: ランキング監視ループ（`rankingwatch.Watcher.Run`。既定＝`scan.full_scan_enabled`が`true`でないとき。毎分kabu `GET /ranking`で監視銘柄（PUSH登録最大45件）を決め、`market-data`投入・Fast Screener対象に使う。FR-SCHED-9）と、`scan.ranking_measure.enabled`のときだけ`/ranking`計測ループ（`rankingmeasure`。FR-SCHED-8）を別goroutineで起動
    App->>KABU: 対象ユニバース銘柄登録・PUSH購読開始（既定のランキング監視では全銘柄ではなく、登録リストを空にしたうえで直近の監視リストだけを登録し、監視リストが決まるたびに`PUT /register`で更新する）
    App->>App: WebView起動・Scanner Dashboard表示
```

#### 停止フロー

`bootstrap.Services.Start`が起動したgoroutineは`Start`へ渡したcontextに紐づくため、停止はcontextの取り消し→`Services.Stop()`（Schedulerのcronとワーカーを止め、候補更新・保有監視・ランキング監視/計測・PushFeed・News Ingestのgoroutineと実行中ジョブの終了を待つ）の順で行い、DBを閉じるのはその後（`defer`）に限る。

```mermaid
sequenceDiagram
    participant OS as OS/Wails
    participant App as アプリ（cmd/server／cmd/desktop）
    participant HTTP as HTTPサーバー（serverのみ）
    participant SVC as bootstrap.Services
    participant DB as SQLite

    alt cmd/server
        OS->>App: SIGINT/SIGTERM（`signal.NotifyContext`のcontext取り消し。`Services.Start`のgoroutineにも伝わる）
        App->>HTTP: `httpServer.shutdown(10s)`: 新規接続の受付停止と処理中リクエストの待機（最大10秒）
        App->>HTTP: サーバーcontextを取り消してWebSocketハンドラを終了させ、返るまで待機（最大10秒。`http.Server.Shutdown`は乗っ取り済み接続を待たないため`handlerTracker`で待つ）。失敗はERRORログのみで続行
        App->>SVC: `Services.Stop()`（ワーカー・バックグラウンドgoroutineの終了待ち）
        App->>DB: deferで`State.Close`（DBクローズ）、`app.lock`解放。`run`が返り`RunMain`が終了コード0で終了
    else cmd/desktop
        OS->>App: Wailsの`OnShutdown`（`App.shutdown`）
        App->>SVC: `OnStartup`で作ったcontextの`cancel`
        App->>SVC: `Services.Stop()`（ワーカー・バックグラウンドgoroutineの終了待ち）
        App->>App: ネイティブ通知の後始末（`runtime.CleanupNotifications`）
        opt 自動アップデートの`QuitForUpdate`でインストーラーが記録済み
            App->>App: 検証済みインストーラーを`/S`（サイレント）で切り離して起動（失敗はERRORログのみ。`updater.BuildSilentInstallCommand`）
        end
        App->>DB: `wails.Run`が返った後、deferで`State.Close`、`app.lock`解放
    end
```

- serverでListenAndServeが自発的に失敗した場合も、contextを取り消して`Services.Stop()`を呼んでから`server error`として`run`が返り、`RunMain`がERRORログ＋終了コード1にする
- desktopの`OnStartup`で`Services.Start`が失敗した場合は、ログに記録してネイティブのエラーダイアログを表示し、`runtime.Quit`で上記の`OnShutdown`に進む

### 10.2 スキャン〜発注フロー

`requirements/functional.md` §2 主要処理フロー（シーケンス図）を参照。アーキテクチャ上の要点は以下。

- `market-data` ジョブは、既定ではランキング監視（FR-SCHED-9。`internal/bootstrap/rankingwatch`が毎分kabu `GET /ranking`で決めた監視銘柄）が`Scheduler.EnqueueMarketData`で銘柄ごとにenqueueする。`scan.full_scan_enabled: true`のときだけ、Scheduler（自前Worker、`jobs`テーブル）の60秒フルスキャンが全銘柄（`market_index`/`sector_index`を含む）にenqueueする（FR-SCHED-2/7。既定のオフ中は指数行が更新されず、市場コンテキスト特徴量は欠損になりうる）。特徴量の算出・永続化は `market-data` ジョブ内で同期実行される。`feature-calc` キューはフルスキャンが投入せず、以前のバージョンが残した未処理ジョブを消化するだけの互換用の空ハンドラとして残っている（`market-data` → `feature-calc` の連鎖ではない。FR-SCHED-1）。`jev-scout` は候補更新サイクル（`bootstrap/candidates`）と、`market-data` ジョブ内のイベント再評価（FR-SCAN-1）からenqueueされ、`jev-trader` は `jev-scout` ジョブがenqueueする。各Serviceはdomainモデルを介して疎結合に連携する
- `jev-scout`/`jev-trader`の直前にRAG Context Builder（§7）が類似局面を検索し文脈を付与する
- Risk判定・Paper発注は独立したキュー（ジョブ）を持たず、`jev-trader`ジョブ内でPolicy Engine → Risk Engine → Execution（Paper）を同期実行する。Risk Engineは必ずPolicy Engineの直後に評価され、Risk Engineの承認なしにExecutionへは到達しない
- `outcome-labeling` は毎分のcron（`@every 1m`）が、判定水平線（5/10/20分）を経過したJev判断を拾ってenqueueする（約定・Exitを契機にはしない。判断から24時間以内のものに限り（`PendingLabels`へ下限`now-24h`を渡し`jev_decisions`の`(decision_type, timestamp)`索引で範囲走査する）、ラベル済み・恒久的にラベル不能と確定済み（`calibration_label_skips`）・pending/running中のペアは除く）。`analytics` は平日15:40 JSTのSol/Opus自己改善バッチ（`integrations.md` §8）専用のキューである。いずれも売買パスとは独立に非同期実行し、UIの応答性に影響を与えない

### 10.3 Kill Switchフロー（発動〜再開）

```mermaid
sequenceDiagram
    participant RE as Risk Engine
    participant EX as Execution
    participant DB as SQLite
    participant TRAY as Wails通知
    participant SLACK as Slack Webhook

    RE->>RE: 日次損失上限/連敗上限/異常検知/ハートビート途絶を検出
    RE->>DB: kill_switch_events登録（reason, detail_json）
    RE->>EX: 新規エントリー停止指示
    opt reasonが daily_loss_limit / unexpected_position / fill_discrepancy / consecutive_losses / db_write_failure / broker_api_error / operator_manual
        RE->>EX: 保有ポジション強制クローズ指示（必要な場合。未解除かつ建玉が残る間は1分周期で再実行する）
    end
    RE->>TRAY: ネイティブ通知発火（強制決済の後）
    RE->>SLACK: Webhook通知送信（reason・自動/手動再開区分を含む。強制決済の後）
    alt 自動再開対象（market_data_down / jev_api_down / operator_heartbeat_timeout）
        RE->>RE: 発動条件の解消を定期監視
        RE->>DB: kill_switch_resolutions に resolved_by=auto の解除行を追記
        RE->>EX: 新規エントリー再開
        RE->>SLACK: 自動再開を通知
    else 手動再開対象（daily_loss_limit / unexpected_position / fill_discrepancy / consecutive_losses / db_write_failure / broker_api_error / operator_manual）
        Note over RE: オペレーターがUI（pitha-kill-switch-panel）で明示的にresumeするまで停止を維持。consecutive_losses / daily_loss_limit のResumeは再開ベースラインを記録し、以後の連敗数・日次実現損失はその時刻以降のクローズ分のみで判定する（再発動ループの防止。`requirements/functional/components-pipeline.md` FR-RISK-7）
    end
```

発動時の実行順序は「`kill_switch_events`登録 → 強制決済（該当reasonのみ）→ 通知」である（`risk.Engine.TriggerKillSwitch`→`enforceKillSwitch`、#628）。通知（ネイティブ・Slack）は最善努力のアラートであり、Slackの遅延・不達（チャネルごと10秒のHTTPタイムアウト）が損失拡大中の決済を遅らせないよう決済を先に行う。通知は`CloseAll`が失敗した場合も試行し、通知の失敗はログに残すだけで`TriggerKillSwitch`のエラーには含めない（返るのは決済の失敗のみ）。

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
- 操作者の操作ではないリクエストは更新対象外とする: `/static/...`、WebSocketのUpgrade（画面が自動で再接続する）、画面が自動ポーリングするリクエスト。自動ポーリング（`hx-trigger="... every Ns"`）は現状 `Header` の `#update-banner`（`GET /system/update-status`、`every 60s`）と `#marketdata-banner`（`GET /system/marketdata-status`、`every 30s`）で、いずれも下記の `X-Pitha-Background: 1` を `hx-headers` で付ける。サーバー側にURLパスの除外一覧は持たない（追加漏れが安全機構を無効化する構造を避けるため）。周期ポーリングを追加するときは必ず `hx-headers` に同ヘッダを付ける（付け忘れるとそのリクエストは操作者操作として数えられる）。`updateStatusChanged` を契機とする `#update-banner` の再取得も同じ要素の `hx-headers` で引き続き除外される
- 自動発火の再同期リクエストも更新対象外とする（FR-RISK-6）。`pitha-kill-switch-panel` の再同期（初回・`kill_switch` / `state_changed` push受信時・WebSocket再接続後の `GET /api/v1/system/status`）と、`systemStateChanged` を契機とするHeaderの `#header-status`（`GET /system/status`）、`pitha-activity-feed` のWebSocket再接続後のスナップショット再取得（`GET /api/v1/activity`、`?type=kill_switch&limit=10`）は専用ヘッダ `X-Pitha-Background: 1`（`middleware.BackgroundHeader`、Lit側は `lib/api.ts` の `get(path, { background: true })`、htmx側は `hx-headers`）を付け、Heartbeatミドルウェアが除外する。これらは操作者不在でも発火するため、`operator_heartbeat_timeout` のKill Switch発動後のpushが自らハートビートを更新し、`AutoResume` が無人のまま解除してしまうことを防ぐ
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
    SG->>DB: JEV_API_KEY/KABU_API_PASSWORDの有無を確認
    alt いずれか未設定（または読み出し失敗）
        SG-->>UI: 302 /setup（HTMXは204+HX-Redirect、/api/v1は503 JSON、WebSocketは403）
        UI->>SET: GET /setup
        UI->>SET: POST /settings/:key（`SecretFieldRow`の保存。`/setup`・`/static/...`と同じくガード対象外）
        SET->>DB: 暗号化保存
    else 2キーとも設定済み
        SG->>SG: 通常のルートへ委譲（リダイレクトなし）
    end
```

- ガードは全ルート（ページ・アクション・`/api/v1`・WebSocket・404含む）の手前に置き、`/setup`・`POST`/`DELETE /settings/:key`・`/static/...`のみ通す。判定はリクエストごとにDBを参照し、状態を保持しないため、2キーが揃った次のリクエストから自動で解除される（アプリ再起動は不要）
- `/setup`はSettings画面と同じ接続先一覧・モーダル（`ConnectionList`）と`SecretFieldRow`・同じ`POST`/`DELETE /settings/:key`を使い、専用の保存実装を持たない。完了後も直接アクセスして再設定できる
- `/setup`は`Header`（ガード対象の`hx-get`フラグメントを持つ）を含まない専用レイアウト（`SetupShell`）で描画する
- 各種サービスは従来通り起動時の値を読むため、保存した認証情報の反映にはアプリ再起動が必要（§5）

### 10.6 エラーログエクスポート

`requirements/functional/components-platform.md` §4.19（FR-ERRLOG-1〜7）の実装詳細。

```mermaid
sequenceDiagram
    participant UI as WebView（Settings #error-log-panel）
    participant API as GET /api/v1/logs/errors
    participant EXP as logging.Exporter
    participant FS as logs/（日次.log・.log.gz）

    UI->>API: フォーム送信（days, level）。セッションCookie必須
    API->>EXP: Export(days, level)
    EXP->>FS: 対象期間の<YYYY-MM-DD>.log / .log.gz を新しい日から読む
    EXP->>EXP: レベル抽出・秘密情報マスク・合計10MiBまで新しい記録を優先
    EXP-->>API: 記録（時刻順）・件数・切り捨て有無
    API-->>UI: 200 NDJSON（Content-Disposition: attachment）
```

- `internal/logging`に読み取り専用のExporterを置き、`web/handler/system`がインターフェース越しに呼ぶ（`bootstrap`が`LogDir`を渡して組み立て、`router`のOptionで注入する。`web` → `repository/**`の禁止は変わらない）。書き込み側の`RotatingWriter`・`Archiver`とは独立で、ログファイルを変更しない
- 当日の書き込み中ファイルは読み取り時点までを対象とし、末尾の不完全行は捨てる。`.log.gz`は透過的に展開する
- マスク規則と上限は FR-ERRLOG-3/4 に従う。ダウンロード操作は操作者の操作としてハートビート更新対象（§10.4）

## 11. 障害対応方針

| 障害 | 対応 |
|------|------|
| Jev API失敗 | 1回目リトライ→2回目以降exponential backoff→継続失敗でnew entry停止。既存ポジションはコードベースExit Ruleで継続管理 |
| Market Data欠損 | 現状: 銘柄単位のstale判定による新規取引禁止は未実装（`StatusTracker`は記録のみ）。全体の`market_data_down` Kill Switch（`GetBoard`5回連続失敗）で新規取引を停止する。銘柄単位の禁止はPhase 7移行前に実装する（`overview/integrations.md` §5） |
| kabuステーションAPI異常 | Kill Switch発動条件に該当。新規取引停止、必要に応じ強制決済 |
| DB書き込み失敗継続 | Kill Switch発動条件に該当 |
| Wailsプロセスクラッシュ | `--supervise`起動の監視プロセス（`internal/supervisor`）が自動再起動する（`architecture/overview/integrations.md` §9）。プロセス停止中は新規エントリーも行われない（既存ポジションはkabuステーション側の待機注文/手動介入を前提）。再起動後、`jobs`テーブルの中断ジョブを`pending`へ復帰させ処理を再開する |
