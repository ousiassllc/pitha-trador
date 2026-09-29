# アーキテクチャ設計

## 1. 設計方針

- **単一プロセス・単一バイナリ**: Wails によりネイティブデスクトップアプリとして単一の Go プロセスに全機能（HTTP/HTMXサーバー、Scheduler/Worker、kabuステーションAPI連携、Jevアダプタ）を同居させる。単一Windowsホスト構成（`requirements/non-functional.md` §1）に合わせ、ネットワーク越しの分散構成は取らない
- **バックエンドファースト（HALT思想）**: フロントエンドはAPIサーバー（Gin）に内蔵し、SPAを作らない。判断に迷ったらサーバー側に寄せる。詳細は `components/overview.md`
- **計算とJev判断の分離**: 価格・リターン・VWAP・ATR・板インバランス等の計算可能な値はGoコードで計算する。Jevは解釈（regime/direction/toxic flow等）のみを担当する（`docs/overview.md`「含まないもの」の「LLMへの価格計算・ポジションサイズ計算の委任」を継続遵守）
- **SQLite中心のインフラ最小化**: Postgres/Redis/BullMQ/Celeryのような別プロセスのミドルウェアを一切持ち込まず、DB・Job Queue・ベクトル検索インデックスをすべて**単一のSQLiteファイル**（アプリ内蔵）で完結させる。Wailsの単一実行ファイル配布と最も相性がよく、Windowsホストへの事前インストール作業をゼロにする
- **AI自己改善ループの境界**: Sol/Opus（§8）は`runtime_settings`の`policy.*`キー（Policy Engineしきい値）のみ変更可能。`risk.*`キー（Risk Engineのリミット値）およびJevの`prompt_version`/質問セット自体は自己改善ループの対象外とし、人手のみが変更できる。これはアプリケーション層のアクセス制御（governorサービスが`risk.*`への書き込みAPIを持たない）で技術的に強制する（`docs/overview.md`「含まないもの」の「AIによるRisk Engineのリミット値そのものの変更」を継続遵守）
- **差し替え可能性**: 各レイヤーはインターフェースを介して疎結合にする。`internal/domain`・`internal/repository`・`internal/service` はWailsプロセスの存在を前提にしない。将来スキャン対象拡大や複数戦略運用でプロセス分離が必要になった場合に備える

## 2. 技術スタック

| レイヤー | 技術 | 役割 |
|---------|------|------|
| 言語 | Go 1.25+ | バックエンド・Scheduler・アダプタ全般を単一言語で実装 |
| デスクトップシェル | Wails v2 | ネイティブウィンドウ（WebView2）・システムトレイ・ネイティブ通知・単一実行ファイル配布 |
| サーバーフレームワーク | Gin | ルーティング＋SSR。WailsのAssetServer.Handlerとして注入 |
| APIフレームワーク | Huma | OpenAPI 3.1自動生成、入出力バリデーション（`/api/v1/...`） |
| テンプレートエンジン | Templ | 型安全なGo HTMLテンプレート |
| インタラクション | HTMX | サーバー駆動のDOM更新 |
| リッチUI | Lit (Web Components) | チャート・ライブテーブル等（`components/overview.md`） |
| チャート描画 | lightweight-charts | ローソク足・VWAP・出来高チャート |
| スタイリング | Tailwind CSS | ユーティリティファーストCSS |
| ビルド | esbuild | Lit/TypeScriptバンドル |
| DB | **SQLite**（`modernc.org/sqlite`、アプリ内蔵） | 全永続データ（`architecture/er.md`参照）。exeに同梱、外部サービスのインストール不要 |
| DBアクセス | database/sql + sqlc（sqlite3方言） | 型安全なSQLクエリ生成。ORMは使わずSQLを直接管理 |
| マイグレーション | golang-migrate（sqlite3ドライバ） | `db/migrations` のSQLマイグレーション管理 |
| ベクトル検索 | `modernc.org/sqlite/vec`（sqlite-vecのpure Go移植、`vec0`仮想テーブル） | RAG類似検索（§7）。pgvector相当の機能をSQLite上で実現。CGO不要でクロスコンパイル可能（`environment/setup.md` §CI/CD参照） |
| Job Queue / Scheduler | 自前Workerプール（`jobs`テーブル + goroutine） | market-data, feature-calc, jev-scout, jev-trader, risk-check, paper-execution, outcome-labeling, analytics の8キュー。単一プロセス前提のためRedis/River等の外部キューは不要。`architecture/er.md` の`jobs`テーブルで永続化・再起動時リカバリ |
| 周期実行 | robfig/cron | 60秒/15-30秒/5-15秒サイクルのトリガー |
| リアルタイムPush | `github.com/coder/websocket` | Scanner Dashboard/Symbol DetailへのUI即時反映（`nhooyr.io/websocket`はメンテナがcoder/websocketへ移管し非推奨化されたため、フォーク後継のcoder/websocketを採用） |
| 市場データ・発注 | kabuステーションAPI（三菱UFJ eスマート証券、旧auカブコム証券） | 1分足・板・発注（REST + PUSH WebSocket） |
| Jevアダプタ | 独自HTTPクライアント | Jev API（外部LLM判断レイヤー）呼び出し |
| Luna/Sol/Opusアダプタ | 独自HTTPクライアント | ニュース分類（Luna）・振り返り分析（Sol）・改善提案レビュー（Opus）呼び出し（§8） |
| ロギング | slog（構造化JSON） | `requirements/non-functional.md` §5 準拠 |
| アラート | Slack Incoming Webhook | 即時通知（`requirements/non-functional.md` §5.2） |

## 3. ディレクトリ構成・レイヤー構造

```text
pitha-trador/
├── cmd/
│   ├── desktop/                  # Wailsエントリーポイント（main.go, app.go, notify.go, wails.json）
│   └── server/                   # ヘッドレス起動（Wails非依存のnet/httpサーバー。main.go, addr.go。CI・WebView2が動かない環境向け）
├── internal/
│   ├── bootstrap/                # 両エントリーポイント共通の起動処理（DB open+マイグレーション、config/*.yamlの4段階解決、各サービスの組み立て・ジョブ登録）
│   │   ├── heldposition/         # FR-SCHED-4 保有ポジション監視・Exit評価ループ（5〜15秒周期、最新板で再評価）
│   │   ├── paperexec/            # Policy Engineのシグナル実行フック→Execution（Paper）のアダプタ
│   │   └── alerts/               # 非機能§5.2のアラート宛先（構造化ログ・Slack）とサービス別Notifierの組み立て
│   ├── config/                   # config/*.yamlの型付きローダー、AES-256-GCM秘密情報ヘルパー（他の内部パッケージに依存しない）
│   ├── logging/                  # slog JSON出力の日次ローテーション（rotate.go）・30日超のgzipアーカイブ（archive.go。`requirements/non-functional.md` §5）
│   ├── version/                  # ビルド時に埋め込むバージョン文字列（`ldflags -X`。自動アップデート判定で使用）
│   ├── domain/                   # ドメインモデル（他レイヤーに非依存）
│   │   ├── instrument.go
│   │   ├── snapshot.go           # market_snapshots相当
│   │   ├── feature.go
│   │   ├── candidate.go          # Fast Screener候補
│   │   ├── jevdecision.go
│   │   ├── newscontext.go        # Luna分類結果（news_context）
│   │   ├── signal.go             # trade_signals相当
│   │   ├── order.go              # paper_orders相当
│   │   ├── position.go
│   │   ├── killswitch.go         # SystemState・KillSwitchEvent
│   │   ├── failurestreak.go
│   │   ├── activity.go           # System Activity Feedのイベント型
│   │   ├── calibration.go
│   │   └── policyproposal.go     # policy_proposals相当
│   ├── repository/               # domainのみに依存（例外: `internal/config`のAES-256-GCMヘルパー）。SQLite接続・マイグレーション（db.go）を含む
│   │   ├── instrument_repo.go
│   │   ├── snapshot_repo.go
│   │   ├── decision_repo.go
│   │   ├── signal_repo.go
│   │   ├── order_repo.go
│   │   ├── position_repo.go
│   │   ├── decisiontrade/        # クローズ済みポジションと開始時のJev判断の結合読み取り（FR-CAL-2の帯別PnL用）
│   │   ├── snapshotcols/         # market_snapshotsのFeature列とdomain.Featureの対応表（INSERT/SELECT用）
│   │   ├── calibration_repo.go
│   │   ├── job_repo.go           # jobsテーブル（自前Worker用）
│   │   ├── proposal_repo.go      # policy_proposals
│   │   ├── killswitch_repo.go    # kill_switch_events / kill_switch_resolutions
│   │   ├── runtime_settings_repo.go # runtime_settings（policy.*/system.*）
│   │   ├── secrets_repo.go       # secrets（AES-256-GCM暗号化）
│   │   ├── dbmw.go               # DB書き込み失敗の検知フック
│   │   └── timeconv.go           # 時刻のSQLite表現との相互変換
│   ├── service/                  # domain, repositoryに依存
│   │   ├── marketdata/           # kabuステーションAPIクライアント（REST+PUSH WS）
│   │   ├── marketcalendar/       # 東証の立会時間・祝日判定（Scheduler SessionGate・Risk・Execution・heldpositionが依存。ネットワーク/tzdata非依存の純粋ルール）
│   │   ├── featureengine/        # 特徴量算出
│   │   │   └── eventtrigger/     # FR-SCAN-1/2 イベントトリガ判定（Detect）
│   │   ├── pushfeed/             # 起動時の銘柄登録・PUSH購読とPUSH板キャッシュ（REST GetBoardへのフォールバック付き）
│   │   ├── screener/             # Fast Screener・screen_score算出
│   │   ├── jev/                  # Jevアダプタ（client.go, scout.go, trader.go, schemas.go, prompt_version.go）
│   │   ├── rag/                  # 埋め込み生成・sqlite-vec類似検索（§7）
│   │   ├── policy/                # Policy Engine
│   │   ├── risk/                  # Risk Engine（Kill Switch含む）
│   │   │   ├── sizing/            # FR-ENTRY-3 ポジションサイズ算出（リスク上限からの純関数）
│   │   │   ├── repoportfolio/     # risk.PortfolioProviderの本番実装（positionsから建玉・日次損失・連敗を導出）
│   │   │   └── multinotify/       # risk.Notifierを複数チャネルへ扇状に配信
│   │   ├── execution/             # Paper/kabu発注実行
│   │   │   └── enrich/            # jev_decisionsのresponse_json内のJev Trader応答項目（regime等）をJevDecisionへ復元（Exit条件・Symbol Detail共用）
│   │   ├── calibration/           # Outcome labeling・Brier/Log Loss算出
│   │   ├── backtest/              # Backtest Engine（Walk Forward評価・Governor用シャドーバックテスト）
│   │   ├── assist/                # Luna/Sol/Opusアダプタ
│   │   │   ├── luna.go
│   │   │   ├── sol.go
│   │   │   └── opus.go
│   │   ├── newsfeed/              # News Ingest: 外部ニュースフィード定期取得→Luna呼び出し（§13）
│   │   ├── selfimprove/           # Sol提案生成〜Opusレビュー〜適用/ロールバック（§8）
│   │   ├── notify/                # Slack Incoming Webhookによる即時アラート送信
│   │   ├── updater/               # GitHub Releases自動アップデート（検知・安全ゲート・検証、desktopのみ配線、§9）
│   │   ├── activityfeed/          # jobs/jev_decisions/kill_switch_events集約の読み取り専用フィード（System Activity Log向け、§12）
│   │   ├── insight/               # 判断履歴・シグナル・実績サマリーの読み取り専用クエリ（`api/endpoints.md` §5）
│   │   ├── backup/                # 日次SQLiteバックアップ（daily 90日 + weekly gzip、`requirements/non-functional.md` §3）
│   │   ├── retention/             # jobs / market_snapshotsの期限切れ行パージ（`requirements/non-functional.md` §3）
│   │   └── scheduler/             # 自前Workerプール定義・周期ジョブ登録
│   │       └── maintenance/       # 日次ハウスキーピング（バックアップ・データ保持パージ・ログアーカイブ）のcatch-up実行。最終成功日をruntime_settingsへ保持し、起動時と10分ごとに未実行分を実行
│   ├── router/                    # SSR + API ルーティング定義（Huma登録含む）
│   │   ├── options.go             # Option群（依存注入）
│   │   ├── router.go              # New（Gin Engine組み立て）
│   │   ├── router_middleware.go   # middlewareの適用順
│   │   ├── router_routes.go       # ルート登録
│   │   └── static.go              # 静的アセット配信（go:embed、`PITHA_STATIC_DIR`によるディスク上書き）
│   └── web/
│       ├── handler/               # scanner.go, symbol*.go, performance.go, calibration.go, system.go, settings.go, activity.go, update.go, policy_proposals.go ほか（*_ws.goはWebSocket）
│       ├── insightapi/            # decisions/signals/performance の読み取り専用JSON API（Huma登録、`service/insight`を使用）
│       ├── middleware/            # HostGuard（Host/Origin検証）, Session（Cookie+CSRF）, RequestLog, Recovery, 操作者ハートビート記録（§10.4）, Setup Guard（§10.5）, SystemState
│       ├── atoms/
│       ├── molecules/
│       ├── organisms/
│       ├── pages/
│       └── layout/
├── static/
│   └── src/
│       ├── components/            # Lit Web Components（pitha-* 、詳細は components/overview.md）
│       │   └── lib/               # api.ts, ws.ts, ws-status.ts, logger.ts, styles.ts
│       ├── css/
│       └── dist/                  # ビルド成果物
├── db/
│   └── migrations/                # golang-migrate SQLマイグレーション（SQLite方言、vec0仮想テーブル作成含む）
├── config/
│   ├── strategy.yaml               # スキャン頻度・Fast Screenerしきい値・Policy Engineしきい値
│   ├── risk.yaml                   # Risk Engine制限値（`requirements/functional.md` §4.7参照）
│   └── embed.go                    # 上記YAMLのgo:embed（配布exe用の既定値、§9）
└── tests/
```

### レイヤー依存ルール（HALT準拠）

依存は上から下への一方向のみ。逆方向のimportは禁止。

```text
handler → service → repository → domain
   ↓
 Templ テンプレート（atoms/molecules/organisms/pages）
```

- `domain/`: 他レイヤーに依存しない。純粋なビジネスロジック（例: Risk Engineのしきい値判定ロジック自体はdomainに置き、DB/HTTPアクセスはrepository/serviceに分離）
- `repository/`: `domain/` のみに依存。例外として`SecretsRepository`（issue #57）のみ`internal/config`のAES-256-GCMヘルパー（依存を持たない、`domain`と同格の基盤パッケージ）にも依存する
- `service/`: `domain/`, `repository/` に依存。`marketdata`/`jev`/`assist`など外部I/OはこのレイヤーでHTTPクライアントとして実装する
- `web/handler/`: `service/`, `domain/` に依存。`repository/` を直接使わない（`.golangci.yml` の depguard が `internal/web/**` から `internal/repository` への import を lint で拒否する）。repositoryが返すセンチネルエラーのうちhandlerが分類する必要があるもの（例: `domain.ErrPositionNotFound`）は`domain/`に定義し、repositoryはそれを返す
- `router/`: `handler/` を参照してルートを定義

## 4. コンポーネント責務

| コンポーネント | 責務 | 実装場所 |
|---------------|------|---------|
| Market Data Client | kabuステーションAPIからの1分足・板・約定データ取得（REST）、リアルタイム価格のPUSH WebSocket受信、トークン管理 | `internal/service/marketdata` |
| Feature Engine | 価格・VWAP・出来高・ボラティリティ・板/約定・市場コンテキスト特徴量の算出（`requirements/functional.md` §4.1） | `internal/service/featureengine` |
| Fast Screener | 数値フィルター・screen_score算出・上位N銘柄選定（§4.2） | `internal/service/screener` |
| Jev Adapter (Scout/Trader) | 構造化状態をJev APIへ送信し、choice/score/yes-no型の判断を受け取る（§4.4, §4.5） | `internal/service/jev` |
| RAG Context Builder | 現在の状態ベクトルからsqlite-vecで類似過去局面を検索し、Jevへのfew-shot文脈を構築する（§7、FR-RAG-1〜5） | `internal/service/rag` |
| Policy Engine | Jev出力をトレードシグナルへ変換（§4.6） | `internal/service/policy` |
| Market Calendar | 東証の立会時間（前場/後場）・祝日判定。立会時間外の市場データ取得・Jev呼び出し・新規発注停止（Scheduler SessionGate）、FR-RISK-6のハートビート判定、FR-EXIT-1の引け前強制決済が参照する（`requirements/non-functional.md` §3） | `internal/service/marketcalendar` |
| Risk Engine | ポジションサイズ・損失上限・Kill Switch（§4.7）。全レイヤーの中で最終拒否権を持つ。サイズ算出（FR-ENTRY-3）・ポートフォリオ状態の導出・複数チャネル通知はサブパッケージ | `internal/service/risk`（`sizing`, `repoportfolio`, `multinotify`） |
| Execution | Paper Entry/Exit・kabuステーションAPI発注（実売買移行時）。Jev Trader応答項目の復元は`enrich` | `internal/service/execution`（`enrich`） |
| Calibration | Outcome Labeling、Brier Score/Log Loss/ECE算出（§4.12） | `internal/service/calibration` |
| Self-Improvement Governor | Sol提案の受理、Opusレビュー依頼、シャドーバックテスト実行、`runtime_settings`への適用・ロールバック（§8、FR-SELFIMPROVE-1〜7） | `internal/service/selfimprove` |
| Luna/Sol/Opus Adapter | ニュース分類（Luna）・振り返り分析（Sol）・提案レビュー（Opus）のAPI呼び出し | `internal/service/assist` |
| Scheduler/Worker | `jobs`テーブルを介した自前Workerプールによるキュー処理・周期実行トリガー（§4.10） | `internal/service/scheduler` |
| Activity Feed | `jobs`/`jev_decisions`/`kill_switch_events`を集約し、System Activity Log向けのキュー状況・直近アクティビティを提供（新規永続テーブルなし、§12） | `internal/service/activityfeed` |
| Backtest Engine | Walk Forward評価とGovernor用シャドーバックテスト（未来情報混入の検査・損益指標算出。§8） | `internal/service/backtest` |
| Notifier | Slack Incoming Webhookによる即時アラート送信（Kill Switch発動・障害等。§10.3） | `internal/service/notify` |
| Updater | GitHub Releasesの新版検知・安全ゲート（建玉なし・Kill Switch非発動・直近発注なし）・インストーラ検証。desktopビルドのみ配線（§9） | `internal/service/updater` |
| Insight | 判断履歴・シグナル・実績サマリーの読み取り専用クエリ（`api/endpoints.md` §5）。HTTP公開は`internal/web/insightapi` | `internal/service/insight` |
| Backup | 日次SQLiteバックアップ（daily 90日保持 + ISO週ごとのweekly gzip、`requirements/non-functional.md` §3） | `internal/service/backup` |
| Retention | `jobs`（成功7日・失敗30日）・`market_snapshots`（90日）の期限切れ行のパージ。監査系テーブルは対象外 | `internal/service/retention` |
| Bootstrap | desktop/server共通の起動処理（DB open+マイグレーション、`config/*.yaml`解決、サービス組み立て・ジョブ登録。§10.1）。サブパッケージ: `heldposition`（FR-SCHED-4 保有ポジション5〜15秒Exit監視）、`paperexec`（Policy→Execution Paperアダプタ）、`alerts`（アラート宛先） | `internal/bootstrap`（`heldposition`, `paperexec`, `alerts`） |
| Logging | slog JSON出力・日次ローテーション・30日超のgzipアーカイブ（`requirements/non-functional.md` §5） | `internal/logging` |
| Headless Server | Wailsに依存しない`net/http`エントリーポイント（Updater非配線。§9） | `cmd/server` |
| Setup Guard Middleware | 必須認証情報（JEV_API_KEY/JEV_BASE_URL/KABU_API_PASSWORD）が未設定の間、`/setup`・`POST`/`DELETE /settings/:key`・`/static/...`以外の全リクエストを`/setup`へ誘導する（ページ遷移は302、HTMXは`HX-Redirect`、`/api/v1`は503 JSON、WebSocketは403。§10.5、FR-SETUP-1） | `internal/web/middleware` |
| Web (HTMX/Templ/Lit) | UI提供（`components/overview.md`） | `internal/web` |

## 5〜13. 分割章

以降の章は `.linterly.yml` の300行/ファイル制限のため別ファイルに分割している（節番号・内容は分割前と同一）。

| 節 | ファイル |
|----|----------|
| §5 kabuステーションAPI連携 / §6 Jev API連携 / §7 RAG連携 / §8 自己改善ループ / §9 Wails統合 / §12 System Activity Feed連携 / §13 Luna ニュース分類・News Ingest連携 | `docs/architecture/overview/integrations.md` |
| §10 通信フロー（§10.1〜§10.5）/ §11 障害対応方針 | `docs/architecture/overview/flows.md` |

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-09-26 | 新規作成 | 初版 |
| 1.1 | 2026-09-26 | §10.3を発動〜再開フローに拡張し、§10.4操作者ハートビート監視（dead-man's switch）を追加 | Phase 7も含めた完全自動運用への方針変更 |
| 1.2 | 2026-09-26 | §7 RAG連携、§8 自己改善ループ（Sol/Opus連携）を追加。DBをPostgreSQLからSQLiteへ全面移行（Job QueueはRiverから自前Workerへ、pgvectorはsqlite-vecへ） | 自己学習による継続的改善の組み込み、Wails単一exe配布との整合 |
| 1.3 | 2026-09-26 | §7のベクトル次元表記を16→14（`architecture/er.md`と整合）に修正 | レビュー指摘対応 |
| 1.4 | 2026-09-26 | ベクトル検索を`modernc.org/sqlite/vec`と明記し、CGO不要である旨を追記（CIでのWindowsクロスビルド可否の根拠） | レビュー指摘対応 |
| 1.5 | 2026-09-27 | リアルタイムPushライブラリを`nhooyr.io/websocket`から後継の`github.com/coder/websocket`へ変更（旧パッケージはメンテナ自身がdeprecated宣言、APIは互換） | golangci-lint（staticcheck SA1019）指摘対応 |
| 1.6 | 2026-09-28 | §3レイヤー依存ルールに`SecretsRepository`の`internal/config`依存という例外を明記。§5/§6にJEV_API_KEY/JEV_BASE_URL/KABU_API_PASSWORDの入力経路をSettings画面（`/settings`）・DB保存（`secrets`テーブル、AES-256-GCM暗号化）へ変更した旨を追記（issue #57、`.env`/環境変数からの入力を廃止） | issue #57実装 |
| 1.7 | 2026-09-28 | §9に config/strategy.yaml・config/risk.yaml・静的アセットの`go:embed`埋め込みと4段階の解決優先順位（明示パス→環境変数→実行ファイル隣接→埋め込み既定値）を追記。`runtime.Caller(0)`ベースの`repoRoot`/`staticDir`（ビルドマシンの絶対パス依存で配布先では動作しなかった）を廃止 | issue #59実装（配布可能な.exeへの対応） |
| 1.8 | 2026-09-29 | §9に自動アップデートの検知状態（`Checker.Status()`）とUI通知・手動確認導線を追記 | issue #76実装 |
| 1.9 | 2026-09-29 | §4に`Activity Feed`コンポーネント（`internal/service/activityfeed`）を追加、§12 System Activity Feed連携を新設。既存jobs/jev_decisions/kill_switch_eventsの読み取り専用集約で新規テーブルなし | 実行中処理を可視化するログ画面の追加要望 |
| 1.10 | 2026-09-29 | §8のシーケンス図にFR-SELFIMPROVE-8（LLM出力の機械的検証）・FR-SELFIMPROVE-9（決定的しきい値とOpus APIレビューの併用）を反映。§13 Luna ニュース分類・News Ingest連携を新設、`internal/service/newsfeed`を追加 | 現状Jevのみが実AI呼び出しであった状態の是正（AI機能実装フェーズ） |
| 1.11 | 2026-09-29 | §8・§13に外部AI API/ニュースフィードの契約（エンドポイント・リクエスト/レスポンス・キャッシュ/ポーリング仕様・未設定時の挙動）を追記 | #81/#82実装で確定した外部API契約の仕様書反映 |
| 1.12 | 2026-09-29 | §5の認証情報入力経路をキー単位の保存・削除（`POST`/`DELETE /settings/:key`、allow-list外は400）と明記 | issue #79実装 |
| 1.13 | 2026-09-29 | §3 middleware/にSetup Guardを追記、§4にSetup Guard Middleware行、§5の未設定時挙動をSetup Guardへの誘導へ変更、§10.5初回セットアップ誘導を追加 | issue #80実装 |
| 1.14 | 2026-09-29 | §5の発注方針にBroker認証情報のProduction/Paper分離をPhase 7で実施する旨を追記 | issue #103対応 |
| 1.15 | 2026-09-29 | §5〜§13を`docs/architecture/overview/`配下の章別ファイル（integrations/flows）へ分割。節番号・内容は変更なし | issue #119（300行/ファイル制限の形骸化解消） |
| 1.16 | 2026-09-29 | §4 Setup Guardの応答をリクエスト種別別に変更 | issue #140実装 |
| 1.17 | 2026-09-29 | §3 middleware/にHostGuard・Session・RequestLog・Recovery・SystemStateの名称を反映（適用順は`api/endpoints.md` §1） | issue #136/#149 |
| 1.18 | 2026-09-29 | §3ディレクトリ構成に`cmd/server`・`internal/bootstrap`・`config`・`logging`・`version`・`backtest`・`notify`・`updater`・`insight`・`backup`・`retention`・`insightapi`等の実在パッケージを反映、§4にBacktest/Notifier/Updater/Insight/Backup/Retention/Bootstrap/Logging/Headless Serverを追加。実在しない節番号参照（`overview.md` §2.2・非目標・§4 ER・§5.2）を実際の参照先へ修正 | issue #153/#155 |
| 1.19 | 2026-09-29 | §3ツリーに`marketcalendar`・`bootstrap/{heldposition,paperexec,alerts}`・`risk/{sizing,repoportfolio,multinotify}`・`execution/enrich`・`repository/{decisiontrade,snapshotcols}`を追加。§4にMarket Calendar行を新設し、Risk/Execution/Bootstrapの実装場所にサブパッケージを追記 | issue #179 |
