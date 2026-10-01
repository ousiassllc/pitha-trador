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
| デスクトップシェル | Wails v2 | ネイティブウィンドウ（WebView2）・ネイティブ通知（OSシステムトレイは未対応）・単一実行ファイル配布 |
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
| Job Queue / Scheduler | 自前Workerプール（`jobs`テーブル + goroutine） | market-data, feature-calc, jev-scout, jev-trader, outcome-labeling, analytics の6キュー（Risk判定・Paper発注は`jev-trader`内で同期実行しキューを持たない）。単一プロセス前提のためRedis/River等の外部キューは不要。`architecture/er.md` の`jobs`テーブルで永続化・再起動時リカバリ |
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
│   ├── desktop/                  # Wailsエントリーポイント（main.go, app.go, notify.go, instance_lock.go, wails.json）。`--supervise`起動（main.goの`superviseSelf`）と`build/windows/installer/project.nsi`のStartupショートカット（自動起動）を含む
│   └── server/                   # ヘッドレス起動（Wails非依存のnet/httpサーバー。main.go, addr.go。CI・WebView2が動かない環境向け）
├── internal/
│   ├── bootstrap/                # 両エントリーポイント共通の起動処理の組み立て役（composition root）。直下は DB open+マイグレーション・config/*.yamlの4段階解決（bootstrap.go）、`Services`組み立て・起動停止（services.go, lifecycle.go）、定数（constants.go）、Riskエンジン配線・取引時間判定・自己改善ジョブ（risk.go, session.go, selfimprove_job.go）のみ
│   │   ├── candidates/           # 候補銘柄の定期更新（Fast Screener実行・jev-scoutのenqueue・更新間隔ティッカー。#246）
│   │   ├── marketdatajob/        # market-data / feature-calc ジョブハンドラ（板→Reading変換・イベント再評価enqueue。#246）
│   │   ├── backtestsource/       # Backtest Engine向けのDB読み出しソース（`backtestsource.Source`。#246）
│   │   ├── heldposition/         # FR-SCHED-4 保有ポジション監視・Exit評価ループ（5〜15秒周期、最新板で再評価）
│   │   ├── paperexec/            # Policy Engineのシグナル実行フック→Execution（Paper）のアダプタ
│   │   └── alerts/               # 非機能§5.2のアラート宛先（構造化ログ・Slack）とサービス別Notifierの組み立て
│   ├── config/                   # config/*.yamlの型付きローダー、AES-256-GCM秘密情報ヘルパー（他の内部パッケージに依存しない）
│   ├── safego/                   # FR-SCHED-6 常駐goroutineのpanic回復（`Recover`/`Run`/`Try`/`Loop`。panicをスタック付きでslogに記録し、ループは次サイクルへ継続。他の内部パッケージに依存しない）
│   ├── logging/                  # slog JSON出力の日次ローテーション（rotate.go）・30日超のgzipアーカイブ（archive.go）・エラーログの抽出とマスク（export.go、読み取り専用。`requirements/non-functional.md` §5・§5.3）
│   ├── supervisor/               # --supervise起動時の子プロセス監視・指数バックオフ再起動（cmd/desktopのみが利用。非機能§3）
│   ├── singleinstance/           # ファイルロックによる多重起動ガード（cmd/desktopのみが利用。OSがプロセス終了時にロックを解放）
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
│   ├── repository/               # domainのみに依存（例外: `system`の`SecretsRepository`のみ`internal/config`のAES-256-GCMヘルパー）。直下にはファイルを置かず、リソース群ごとのサブパッケージ（#244）
│   │   ├── sqlutil/              # 共有ヘルパー: 時刻のSQLite表現変換（`FormatTime`/`ParseTime`）・`Nullable*`/`Null*`スキャナ・`RowScanner`/`Execer`/`Executor`インターフェース。標準ライブラリのみに依存するリーフ
│   │   ├── sqlitedb/             # SQLite接続（`Open`）・golang-migrateマイグレーション・`BackupTo`・sqlmw計装ドライバ・DB書き込み失敗検知フック（`DBWriteFailures`）。`domain`とマイグレーションSQLの`go:embed`元`db`のみに依存するリーフ
│   │   ├── market/               # instruments / market_snapshots（`InstrumentRepository`, `SnapshotRepository`）。`snapshotcols`を本番コードで使う
│   │   ├── jobqueue/             # jobsテーブル・キュー名/状態定数（`Job`, `JobRepository`, `ErrJobNotFound`）
│   │   ├── judgement/            # jev_decisions / calibration_outcomes / policy_proposals（`DecisionRepository`, `CalibrationRepository`, `ProposalRepository`）
│   │   ├── trading/              # trade_signals / paper_orders / positions（`SignalRepository`, `OrderRepository`, `PositionRepository`）
│   │   ├── system/               # kill_switch_events / runtime_settings / secrets（`KillSwitchRepository`, `RuntimeSettingsRepository`, `SecretsRepository`）
│   │   ├── decisiontrade/        # クローズ済みポジションと開始時のJev判断の結合読み取り（FR-CAL-2の帯別PnL用。複数リソース群を跨ぐ読み取りの置き場。本番コードは`domain`のみに依存し、他テーブルはSQLで直接結合する）
│   │   └── snapshotcols/         # market_snapshotsのFeature列とdomain.Featureの対応表（INSERT/SELECT用。`market`が本番コードで使う`domain`のみに依存するリーフ）
│   ├── service/                  # domain, repositoryに依存
│   │   ├── marketdata/           # kabuステーションAPIクライアント（REST+PUSH WS）
│   │   ├── marketcalendar/       # 東証の立会時間・祝日判定（Scheduler SessionGate・Risk・Execution・heldpositionが依存。ネットワーク/tzdata非依存の純粋ルール）
│   │   ├── featureengine/        # 特徴量算出
│   │   │   ├── eventtrigger/     # FR-SCAN-1/2 イベントトリガ判定（Detect）
│   │   │   └── marketcontextflow/ # テスト専用: `MarketContextLoader`の回帰テスト（行数上限のためfeatureengineから分離、#248）
│   │   ├── pushfeed/             # 起動時の銘柄登録・PUSH購読とPUSH板キャッシュ（REST GetBoardへのフォールバック付き）
│   │   ├── screener/             # Fast Screener・screen_score算出
│   │   ├── jev/                  # Jevアダプタ（client.go, evaluate.go, scout.go, trader.go, schemas.go, questions*.go, prompt_version.go, systemone/=ワイヤ層, jevtest/=テスト用フェイク, clientflow/=Clientテスト）
│   │   ├── rag/                  # 埋め込み生成・sqlite-vec類似検索（§7）
│   │   ├── policy/                # Policy Engine
│   │   ├── risk/                  # Risk Engine（Kill Switch含む）。`Engine`のメソッド群（Check・状態遷移・監視・警告）は結合が強いため直下の1パッケージに保つ（#247）
│   │   │   ├── sizing/            # FR-ENTRY-3 ポジションサイズ算出（リスク上限からの純関数）
│   │   │   ├── repoportfolio/     # risk.PortfolioProviderの本番実装（positionsから建玉・日次損失・連敗を導出）
│   │   │   ├── multinotify/       # risk.Notifierを複数チャネルへ扇状に配信
│   │   │   ├── killswitchflow/    # テスト専用: Kill Switch発動〜再開フローの回帰テスト（行数上限のためriskから分離）
│   │   │   ├── checkflow/         # テスト専用: `Engine.Check`（FR-RISK-1）の回帰テスト（#247）
│   │   │   └── monitorflow/       # テスト専用: 定期監視・日次損失警告・Notifierの回帰テスト（#247）
│   │   ├── execution/             # Paper/kabu発注実行
│   │   │   ├── enrich/            # jev_decisionsのresponse_json内のJev Trader応答項目（regime等）をJevDecisionへ復元（Exit条件・Symbol Detail共用）
│   │   │   ├── vwapcross/         # FR-EXIT-1 VWAP逆クロスのクロス判定・前回観測トラッカー（execution.Engineが依存する本番コード）
│   │   │   ├── closerace/         # テスト専用: 決済競合（手動決済/CloseAll/Exitモニタ）の回帰テスト（行数上限のためexecutionから分離）
│   │   │   └── closeflow/         # テスト専用: `Engine.Close`/`CloseAll`の回帰テスト（行数上限のためexecutionから分離、#248）
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
│   │       ├── maintenance/       # 日次ハウスキーピング（バックアップ・データ保持パージ・ログアーカイブ）のcatch-up実行。最終成功日をruntime_settingsへ保持し、起動時と10分ごとに未実行分を実行
│   │       └── maintenanceflow/   # テスト専用: バックアップ・データ保持パージ・ログローテーションの各ジョブ呼び出しの回帰テスト（行数上限のためschedulerから分離、#248）
│   ├── router/                    # SSR + API ルーティング定義（Huma登録含む）。`web/handler/**`・`web/apierror`・`web/insightapi`・`web/middleware`・`config`・`static/src`（go:embed）を参照する
│   │   ├── options.go             # Option群（依存注入）
│   │   ├── router.go              # New（Gin Engine組み立て）
│   │   ├── router_middleware.go   # middlewareの適用順
│   │   ├── router_routes.go       # ルート登録
│   │   ├── static.go              # 静的アセット配信（go:embed、`PITHA_STATIC_DIR`によるディスク上書き）
│   │   └── analysisflow/          # テスト専用: 分析系ルート（ポリシー提案）の回帰テスト（行数上限のためrouterから分離、#248）
│   └── web/
│       ├── apierror/              # /api/v1 の huma.NewError 上書き（5xx は固定メッセージのみ返し原因を slog へ。issue #215）
│       ├── handler/               # Ginハンドラ。直下は scanner.go, performance.go, calibration.go, policy_proposals.go, swagger.go（一覧・分析系）。それ以外は責務別サブパッケージ（#245）
│       │   ├── shared/            # 共有ヘルパー（`action_error.go`のアクションエラー整形（`RespondActionError`/`RespondPageError`）・`RenderErrorPage`（routerのmiddlewareも使用）・`ws_poll.go`のWebSocketポーリング（`PollWebSocket`）とJSONフレーム送信（`WriteJSON`））。Templ（`web/atoms`・`web/pages`）・標準/外部ライブラリのみに依存するリーフで、他のhandlerサブパッケージに依存しない
│       │   ├── symbol/            # Symbol List/Detail/Page/Close と `/ws/symbols/:symbol`（symbol*.go）
│       │   ├── system/            # System状態・Kill Switch操作・`/ws/system`・自動アップデートUI（system.go, system_ws.go, update.go）
│       │   ├── settings/          # `/settings`・認証情報の保存/削除・初回セットアップ画面`/setup`（settings.go）
│       │   └── activity/          # System Activity Log（`GET /api/v1/activity`・`/ws/activity`）
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
│       ├── img/                   # logo.svg（Header表示用、go:embed対象）
│       └── dist/                  # ビルド成果物
├── db/
│   └── migrations/                # golang-migrate SQLマイグレーション（SQLite方言、vec0仮想テーブル作成含む）
├── config/
│   ├── strategy.yaml               # スキャン頻度・Fast Screenerしきい値・Policy Engineしきい値
│   ├── risk.yaml                   # Risk Engine制限値（`requirements/functional.md` §4.7参照）
│   └── embed.go                    # 上記YAMLのgo:embed（配布exe用の既定値、§9）
└── tests/
```

### サブパッケージ単位の責務規約

レイヤー（import方向の境界）は最上位ディレクトリ（`domain`/`repository`/`service`/`web`/`router`/`bootstrap`）で決まり、**1パッケージ（ディレクトリ）は1つの責務**を持つ。旧規約の「レイヤー内の全ファイルを1ディレクトリへ平坦に置く」は廃止し、ディレクトリ行数上限（linterly: 300行/ファイル・2000行/ディレクトリ。除外で回避しない）を超える見込みのレイヤーは責務別サブパッケージへ分割する。ツリーの`service/`配下と同様、サブパッケージはディレクトリ単位（責務）で記載し、新規サブパッケージはファイル名を列挙せずディレクトリ行のみ追加する（ファイル構成はパッケージコメントを一次情報とする）。`*_test.go`のみのディレクトリ（`execution/closerace`・`execution/closeflow`・`risk/killswitchflow`・`risk/checkflow`・`risk/monitorflow`・`featureengine/marketcontextflow`・`scheduler/maintenanceflow`・`router/analysisflow`）は行数上限を満たすためにテストを分離したもので、本番コードではない。

`repository`（#244）・`web/handler`（#245）・`bootstrap`（#246）・`service/risk`（#247）はいずれも分割済みで、上のツリーは実装と一致している（各Issueは完了時に本ツリーが実装と一致することを受け入れ条件とする）。**サブパッケージ共通の規約**:

- 兄弟サブパッケージ同士は本番コードでimportしない。共有コードは`sqlutil`/`sqlitedb`/`handler/shared`のようなリーフ・ヘルパー用サブパッケージへ切り出す。リソース群を跨ぐ読み取りは`decisiontrade`のように専用サブパッケージ（本番コードは`domain`のみに依存し、他テーブルはSQLで直接結合する）へ置く。**本番コードの例外は`market` → `snapshotcols`のみ**（`market_snapshots`のFeature列対応表を共有する`domain`のみに依存するリーフ。逆向きは禁止）
- **テスト専用のクロスリソース読み取り例外**: `decisiontrade`・`snapshotcols`の外部テスト（`_test.go`）は、データ準備と列対応の整合検証のため兄弟群（`judgement`/`market`/`trading`。`snapshotcols`のテストは`market`）の公開APIと`sqlitedb.Open`をimportしてよい。この例外は`_test.go`に閉じ、本番コードへ広げない。それ以外の群のテストは他リソース群のデータをSQLで直接用意する
- サブパッケージは親パッケージをimportしない（循環回避）。親（`bootstrap`）は組み立て役として子を参照してよく、子は依存を引数（構造体・小さなインターフェース）で受け取る
- 分割後の呼び出し元はサブパッケージ名で修飾する（例: `jobqueue.Job`、`sqlitedb.Open`）。センチネルエラーは返すパッケージが定義する（他レイヤーが分類する必要がある場合は従来どおり`domain/`に置く）
- テストは対象コードと同じサブパッケージへ移設する。`service/risk`のようにパッケージ内結合が強くコードを分割できない場合、または本番コードは上限内でも外部テスト（`package X_test`）を足すとディレクトリ2000行を超える場合のみ、外部テストをテスト専用サブパッケージ（`*flow`等）へ分離する。各テスト専用サブパッケージは自前のヘルパーを持ち、兄弟テストパッケージ同士はimportしない
- `.linterlyignore`に手書きソースの除外を置かない。許容するのは自動生成物`*_templ.go`と実行時ログ`**/logs/**`のみ（詳細は`environment/setup.md`）

#### 分割後の構成（後続Issueの設計判断）

| パッケージ | 分割方針 | 依存方向 |
|-----------|---------|---------|
| `repository`（#244） | テーブルの結合度でリソース群に分ける（`market`/`jobqueue`/`judgement`/`trading`/`system`）。各リポジトリ型はそのテーブルを所有する群に置き、群内のリポジトリ実装のファイル名は`*_repo.go`を維持する（`jobqueue`の`job_queries.go`等の補助クエリと、各群の`doc.go`（パッケージコメント）は例外）。`formatTime`/`nullable*`/`rowScanner`/`execer`/`sqlExecutor`は公開名（`FormatTime`/`Nullable*`/`RowScanner`/`Execer`/`Executor`）にして`sqlutil`へ集約し、`db.go`・`dbmw.go`は`sqlitedb`へ。既存の`decisiontrade`/`snapshotcols`は現位置を維持 | 群 → `sqlutil`・`domain`（`market`のみ`snapshotcols`も可）。`sqlitedb` → `domain`・マイグレーションSQLの`go:embed`元`db`。群同士・本番コードでの群→`sqlitedb`は禁止（テストのDB準備のみ`_test.go`から`sqlitedb.Open`可。`decisiontrade`・`snapshotcols`のテストは前節の例外）。`system`のみ`internal/config`も可 |
| `web/handler`（#245） | 画面/APIの責務別に`symbol`/`system`/`settings`/`activity`へ分け、小さい一覧・分析系ハンドラは直下に残す。`settings/`のテストは表示・保存・削除・セットアップの観点でファイルを分割している（共通のフェイクは`testutil_test.go`）。`action_error.go`・`ws_poll.go`（と各WebSocketハンドラが共用していた`writeJSON`）は`shared`へ移し公開名にした（直下・全サブパッケージ・`router`が共用。`RespondActionError`/`RespondPageError`/`PollWebSocket`/`WriteJSON`/`RenderErrorPage`）。クライアント切断で即終了する回帰テスト（#127）は各WebSocketハンドラを所有するパッケージ（直下`scanner_test.go`・`symbol`・`system`）が個別に持ち、テストが兄弟を跨がない | 直下・サブパッケージ → `service`・`domain`・`shared`・Templ（`web/atoms`・`web/pages`等）。基盤パッケージ`internal/config`（`symbol`・`settings`）・`internal/version`（`system`）も参照する。`shared` → Templ のみ（handler・`service`に依存しない）。`repository/**`は不可。サブパッケージ同士・直下への逆import禁止。`router` → 直下・`shared`・全サブパッケージ |
| `bootstrap`（#246） | 直下は組み立て役（`Run`/`State`/`Services`/`BuildServices`/`Start`/`Stop`）のみ。ジョブ/ループ単位の責務を`candidates`/`marketdatajob`/`backtestsource`へ切り出す（既存の`heldposition`/`paperexec`/`alerts`と同格）。切り出し先は`Services`ではなく必要な依存だけをフィールドに持つ構造体（`candidates.Refresher`・`marketdatajob.Handler`・`backtestsource.Source`）に対するメソッドとして実装し、直下の`BuildServices`が引数で組み立てる（`marketdatajob.BoardSource`のような小さなインターフェース経由で依存を受ける）。テストは各サブパッケージ内で最小のフェイク/実DBを組み立て、`Services`全体には依存しない。直下のテストは`BuildServices`の配線検証のみ | 直下 → 全サブパッケージ・`service`・`repository`・`sqlitedb`。サブパッケージ → `service`・`domain`・`repository`群と、基盤パッケージ`internal/config`（`candidates`・`marketdatajob`・`alerts`）・`internal/safego`（`candidates`・`heldposition`）のみ。親・兄弟への依存禁止 |
| `service/risk`（#247） | `Engine`のメソッド群（`check.go`のCheck・`state.go`の状態遷移・`losslimit.go`の損失上限・`monitor.go`の定期監視・`warning.go`の日次損失警告・`autoresume.go`・`baseline.go`・`session.go`・`settings.go`・`sizing.go`）は`Engine`のunexported状態を共有するため**分割せず**`risk`直下に保つ（本番コードのみで約1.6k行＝ディレクトリ上限内。直下に`_test.go`は置かない）。行数上限は、`package risk_test`の外部テスト（旧940行）を`killswitchflow`に倣ってテスト専用サブパッケージへ移して満たした（`checkflow`=`Engine.Check`・`monitorflow`=定期監視・日次損失警告・Notifier）。各テスト専用サブパッケージは`helpers_test.go`に自前のフェイク（`fakePortfolio`/`fakeCloser`/`fakeNotifier`等）とテストDB準備（`sqlitedb.Open`）を持ち、兄弟テストパッケージをimportしない | テスト専用サブパッケージ → `risk`（公開API）・`domain`・`config`・`repository/**`（`Engine`へ渡す依存の組み立てとテストDB準備のみ）。本番の依存方向（`risk` → `domain`・`repository/**`・`config`）は変更しない |

### レイヤー依存ルール（HALT準拠）

依存は上から下への一方向のみ。逆方向のimportは禁止。**ルールはレイヤーのサブツリー全体（サブパッケージを含む）に適用する。**

```text
handler → service → repository → domain
   ↓
 Templ テンプレート（atoms/molecules/organisms/pages）
```

- `domain/`: 他レイヤーに依存しない。純粋なビジネスロジック（例: Risk Engineのしきい値判定ロジック自体はdomainに置き、DB/HTTPアクセスはrepository/serviceに分離）
- `repository/**`: `domain/` のみに依存（`sqlutil`は標準ライブラリのみ、`sqlitedb`は標準ライブラリ・外部ライブラリ・`domain`・マイグレーションSQLを`go:embed`する最上位`db`パッケージのみ）。例外として`SecretsRepository`（issue #57、`repository/system`）のみ`internal/config`のAES-256-GCMヘルパー（依存を持たない、`domain`と同格の基盤パッケージ）にも依存する。この例外は`system`パッケージ内に閉じ、`sqlutil`等の共有サブパッケージや他のサブパッケージへ広げない。サブパッケージ間の依存方向は前節の規約に従う
- `internal/safego`: 標準ライブラリのみに依存し、`internal/config`・`domain`と同格の基盤パッケージ。`service/`・`bootstrap`・`cmd/server`のいずれからも参照できる（常駐goroutineのpanic回復、FR-SCHED-6）
- `service/**`: `domain/`, `repository/**` に依存。`marketdata`/`jev`/`assist`など外部I/OはこのレイヤーでHTTPクライアントとして実装する
- `web/**`（`web/handler/**`を含む）: `service/`, `domain/` に依存（基盤パッケージ`internal/config`・`internal/version`も、`web/handler/{symbol,settings,system}`と`organisms`が参照する）。`repository/**` を直接使わない（`.golangci.yml` の depguard `web-no-repository` が `internal/web/**` から `internal/repository` **および全サブパッケージ**（`pkg`はプレフィックス一致）への import を lint で拒否する。サブパッケージ追加でルールの書き換えは不要）。**depguardが強制するのはこの`web` → `repository/**`の1方向のみ**で、`repository`・`web/handler`・`bootstrap`のサブパッケージ間（兄弟同士）のimport禁止、`repository`の依存先の制限、`router`・`bootstrap`の参照範囲などは規約でありlintでは強制されない（レビューで担保する）。repositoryが返すセンチネルエラーのうちhandlerが分類する必要があるもの（例: `domain.ErrPositionNotFound`）は`domain/`に定義し、repositoryはそれを返す
- `bootstrap/**`・`cmd/*`: 組み立て役として全レイヤー（`repository/**`含む）を参照してよい。`bootstrap`の子パッケージは親を参照しない（`internal/config`・`internal/safego`等の基盤パッケージは参照してよい）
- `router/`: `web/handler/**` を参照してルートを定義する。あわせて`web/apierror`・`web/insightapi`・`web/middleware`・`internal/config`・`static/src`（go:embedされた静的アセット）も参照する。`router`のテストは`repository/sqlitedb`・`repository/system`を実DB準備のためimportしてよい（depguard対象外）

## 4. コンポーネント責務

| コンポーネント | 責務 | 実装場所 |
|---------------|------|---------|
| Market Data Client | kabuステーションAPIからの1分足・板・約定データ取得（REST）、リアルタイム価格のPUSH WebSocket受信、トークン管理 | `internal/service/marketdata` |
| Feature Engine | 価格・VWAP・出来高・ボラティリティ・板/約定・市場コンテキスト特徴量の算出（`requirements/functional.md` §4.1）。`marketcontextflow`はテスト専用 | `internal/service/featureengine`（`eventtrigger`, `marketcontextflow`） |
| Fast Screener | 数値フィルター・screen_score算出・上位N銘柄選定（§4.2） | `internal/service/screener` |
| Jev Adapter (Scout/Trader) | 構造化状態と型付き質問（`noul`/`choice`）をTypeSafe AI公式API（`POST /v1/systemone`）へ送信し、回答をScoutResponse/TraderResponseへ変換する（§4.4, §4.5, §6） | `internal/service/jev` |
| RAG Context Builder | 現在の状態ベクトルからsqlite-vecで類似過去局面を検索し、Jevへのfew-shot文脈を構築する（§7、FR-RAG-1〜5） | `internal/service/rag` |
| Policy Engine | Jev出力をトレードシグナルへ変換（§4.6） | `internal/service/policy` |
| Market Calendar | 東証の立会時間（前場/後場）・祝日判定。立会時間外の市場データ取得・Jev呼び出し・新規発注停止（Scheduler SessionGate）、FR-RISK-6のハートビート判定、FR-EXIT-1の引け前強制決済が参照する（`requirements/non-functional.md` §3） | `internal/service/marketcalendar` |
| Risk Engine | ポジションサイズ・損失上限・Kill Switch（§4.7）。全レイヤーの中で最終拒否権を持つ。サイズ算出（FR-ENTRY-3）・ポートフォリオ状態の導出・複数チャネル通知はサブパッケージ。`killswitchflow`・`checkflow`・`monitorflow`はテスト専用 | `internal/service/risk`（`sizing`, `repoportfolio`, `multinotify`, `killswitchflow`, `checkflow`, `monitorflow`） |
| Execution | Paper Entry/Exit・kabuステーションAPI発注（実売買移行時）。Jev Trader応答項目の復元は`enrich`、FR-EXIT-1 VWAP逆クロス判定は`vwapcross`。`closerace`・`closeflow`はテスト専用 | `internal/service/execution`（`enrich`, `vwapcross`, `closerace`, `closeflow`） |
| Calibration | Outcome Labeling、Brier Score/Log Loss/ECE算出（§4.12） | `internal/service/calibration` |
| Self-Improvement Governor | Sol提案の受理、Opusレビュー依頼、シャドーバックテスト実行、`runtime_settings`への適用・ロールバック（§8、FR-SELFIMPROVE-1〜7） | `internal/service/selfimprove` |
| Luna/Sol/Opus Adapter | ニュース分類（Luna）・振り返り分析（Sol）・提案レビュー（Opus）のAPI呼び出し | `internal/service/assist` |
| Scheduler/Worker | `jobs`テーブルを介した自前Workerプールによるキュー処理・周期実行トリガー（§4.10）。`maintenanceflow`はテスト専用 | `internal/service/scheduler`（`maintenance`, `maintenanceflow`） |
| Activity Feed | `jobs`/`jev_decisions`/`kill_switch_events`を集約し、System Activity Log向けのキュー状況・直近アクティビティを提供（新規永続テーブルなし、§12）。HTTP/WebSocket公開は`web/handler/activity` | `internal/service/activityfeed`・`internal/web/handler/activity` |
| Backtest Engine | Walk Forward評価とGovernor用シャドーバックテスト（未来情報混入の検査・損益指標算出。§8） | `internal/service/backtest` |
| Notifier | Slack Incoming Webhookによる即時アラート送信（Kill Switch発動・障害等。§10.3） | `internal/service/notify` |
| Updater | GitHub Releasesの新版検知・安全ゲート（建玉なし・Kill Switch非発動・直近発注なし）・インストーラ検証。desktopビルドのみ配線（§9） | `internal/service/updater` |
| Insight | 判断履歴・シグナル・実績サマリーの読み取り専用クエリ（`api/endpoints.md` §5）。HTTP公開は`internal/web/insightapi` | `internal/service/insight` |
| Backup | 日次SQLiteバックアップ（daily 90日保持 + ISO週ごとのweekly gzip、`requirements/non-functional.md` §3） | `internal/service/backup` |
| Retention | `jobs`（成功7日・失敗30日）・`market_snapshots`（90日）の期限切れ行のパージ。監査系テーブルは対象外 | `internal/service/retention` |
| Background Task Guard | 常駐goroutine（候補更新・保有監視・PushFeed・News Ingest・トークン再発行）のpanic回復（FR-SCHED-6）。`Recover`（defer用）・`Run`（panic有無を返す）・`Try`（panicをerrorに変換）・`Loop`（待機→1サイクルを`Try`で保護し、panicもエラーもログに残して継続）を提供し、panicは`slog`にスタックトレース付きで記録する。`cmd/server`・`bootstrap`・`bootstrap/candidates`・`bootstrap/heldposition`・`service/marketdata`・`service/pushfeed`・`service/scheduler`（`updatecheck`含む）から使う | `internal/safego` |
| Bootstrap | desktop/server共通の起動処理（DB open+マイグレーション、`config/*.yaml`解決、サービス組み立て・ジョブ登録。§10.1）。サブパッケージ: `candidates`（候補銘柄の定期更新）、`marketdatajob`（market-data/feature-calcジョブ）、`backtestsource`（Backtest用DB読み出し）、`heldposition`（FR-SCHED-4 保有ポジション5〜15秒Exit監視）、`paperexec`（Policy→Execution Paperアダプタ）、`alerts`（アラート宛先）。分割方針は§3 | `internal/bootstrap`（`candidates`, `marketdatajob`, `backtestsource`, `heldposition`, `paperexec`, `alerts`） |
| Logging | slog JSON出力・日次ローテーション・30日超のgzipアーカイブ・エラーログのエクスポート（`requirements/non-functional.md` §5・§5.3、フローは`overview/flows.md` §10.6） | `internal/logging` |
| Supervisor | 子プロセスの異常終了時の指数バックオフ再起動（1秒〜5分、1分安定でリセット）。終了コード0で監視終了。`cmd/desktop`の`--supervise`起動でのみ使う（`requirements/non-functional.md` §3） | `internal/supervisor` |
| Single Instance Guard | DBと同じディレクトリのロックファイル（`app.lock`／`supervisor.lock`）による多重起動防止。2つ目の起動は`bootstrap.Run`に到達する前に終了コード0で終了し、Scheduler・Kill Switch・発注の二重稼働を防ぐ | `internal/singleinstance` |
| Headless Server | Wailsに依存しない`net/http`エントリーポイント（Updater非配線。§9） | `cmd/server` |
| Setup Guard Middleware | 必須認証情報（JEV_API_KEY/JEV_BASE_URL/KABU_API_PASSWORD）が未設定の間、`/setup`・`POST`/`DELETE /settings/:key`・`/static/...`以外の全リクエストを`/setup`へ誘導する（ページ遷移は302、HTMXは`HX-Redirect`、`/api/v1`は503 JSON、WebSocketは403。§10.5、FR-SETUP-1） | `internal/web/middleware` |
| API Error Formatter | `/api/v1` のエラーボディ整形。`registerAPI`から`apierror.Install()`で`huma.NewError`を上書きし、4xxはバリデーション詳細を`errors[]`に残し、5xxは固定メッセージのみ返して原因を`slog`へ記録する（`api/endpoints.md` §7、issue #215） | `internal/web/apierror` |
| Repository | SQLiteの永続化（テーブルを所有するリソース群`market`/`jobqueue`/`judgement`/`trading`/`system`、共有ヘルパー`sqlutil`・接続`sqlitedb`、複数リソースの読み取り`decisiontrade`・列対応表`snapshotcols`。§3） | `internal/repository`（サブパッケージ群） |
| Web (HTMX/Templ/Lit) | UI提供（`components/overview.md`）。画面/APIハンドラは責務別サブパッケージ: `symbol`（Symbol List/Detail/Close・`/ws/symbols/:symbol`）、`system`（System状態・Kill Switch・`/ws/system`・自動アップデートUI・エラーログのダウンロード`/api/v1/logs/errors`）、`settings`（`/settings`・`/setup`画面。Setup Guard Middlewareの誘導先）、`activity`（Activity Feed画面/API）、`shared`（共通ヘルパー） | `internal/web`（`handler/{shared,symbol,system,settings,activity}`） |

## 5〜13. 分割章

以降の章は `.linterly.yml` の300行/ファイル制限のため別ファイルに分割している（節番号・内容は分割前と同一）。

| 節 | ファイル |
|----|----------|
| §5 kabuステーションAPI連携 / §6 Jev API連携 / §7 RAG連携 / §8 自己改善ループ / §9 Wails統合 / §12 System Activity Feed連携 / §13 Luna ニュース分類・News Ingest連携 | `docs/architecture/overview/integrations.md` |
| §10 通信フロー（§10.1〜§10.6）/ §11 障害対応方針 | `docs/architecture/overview/flows.md` |

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
| 1.20 | 2026-09-29 | §3ツリーに`execution/{vwapcross,closerace}`・`risk/killswitchflow`を追加（`closerace`・`killswitchflow`はテスト専用ディレクトリ）。§4のRisk/Execution行に`vwapcross`・`killswitchflow`・`closerace`を追記 | issue #199 |
| 1.21 | 2026-09-29 | §3ツリーに`internal/supervisor`・`internal/singleinstance`と`cmd/desktop`の`--supervise`起動・インストーラー自動起動を追記、§4にSupervisor・Single Instance Guardを追加 | issue #208/#210 |
| 1.22 | 2026-09-29 | §3の`internal/web/`ツリーに`apierror/`を追加、§4にAPI Error Formatter行を新設 | issue #215/#219 |
| 1.23 | 2026-09-29 | §3ツリーに`internal/safego`を追加、§4にBackground Task Guard行を新設、§3レイヤー依存ルールに基盤パッケージとして`safego`を追記 | issue #226/#229 |
| 1.24 | 2026-09-30 | §3の「ディレクトリ＝レイヤー」規約を「サブパッケージ単位の責務」規約へ改め、`repository`（`sqlutil`/`sqlitedb`/`market`/`jobqueue`/`judgement`/`trading`/`system`）・`web/handler`（`shared`/`symbol`/`system`/`settings`/`activity`）・`bootstrap`（`candidates`/`marketdatajob`/`backtestsource`）・`service/risk`（Engine集約＋テスト専用`checkflow`/`monitorflow`）の分割後構成とサブパッケージ間の依存方向を確定。レイヤー依存ルールをサブツリー全体（depguardのプレフィックス一致）へ適用する形に更新し、`.linterlyignore`の手書きソース除外を全廃する方針（許容は`*_templ.go`と`**/logs/**`のみ）を明記 | issue #243（#134の先行仕様更新） |
| 1.25 | 2026-09-30 | §9に自動アップデート周期確認の再試行（取得失敗・安全ゲート保留は指数バックオフで再試行）を追記 | issue #240 |
| 1.26 | 2026-09-30 | §3の`repository`を実装に合わせて分割済みと明記（`sqlutil`/`sqlitedb`/`market`/`jobqueue`/`judgement`/`trading`/`system`。直下のファイルは廃止）。テストDB準備のみ`_test.go`から`sqlitedb.Open`可・他リソース群のデータはSQLで直接用意する（兄弟import禁止の維持） | issue #244 |
| 1.27 | 2026-09-30 | §3の`web/handler`を実装に合わせて分割済みと明記（`shared`/`symbol`/`system`/`settings`/`activity`。`shared`の公開ヘルパー名、`settings`テストの分割、`router`テストの統合によるディレクトリ行数維持） | issue #245 |
| 1.28 | 2026-09-30 | §3の`bootstrap`を実装に合わせて分割済みと明記（`candidates`/`marketdatajob`/`backtestsource`を追加し、直下に`constants.go`を新設。`BacktestSource`は`backtestsource.Source`、`Services`メソッドは各サブパッケージの構造体メソッドへ移動）。テストは対象サブパッケージへ移設 | issue #246 |
| 1.29 | 2026-09-30 | §3の`service/risk`を実装に合わせて分割済みと明記（`Engine`本体は直下に維持し、`package risk_test`の外部テストをテスト専用`checkflow`/`monitorflow`へ移設。各々が自前のフェイク・テストDBヘルパーを持つ）。§4のBackground Task Guard行の利用元に`bootstrap/candidates`・`scheduler/updatecheck`を反映 | issue #247 |
| 1.30 | 2026-09-30 | §3のツリーに、行数上限（300行/ファイル・2000行/ディレクトリ）を満たすために外部テストを移したテスト専用サブパッケージ`featureengine/marketcontextflow`・`execution/closeflow`・`scheduler/maintenanceflow`・`router/analysisflow`を追記（各々が自前のヘルパーを持つ）。`.linterlyignore`が`*_templ.go`と`**/logs/**`のみであることを最終確認 | issue #248 |
| 1.31 | 2026-09-30 | 分割後レビュー指摘を反映: §3で本番コードの兄弟import例外（`market` → `snapshotcols`）とテスト専用のクロスリソース読み取り例外（`decisiontrade`・`snapshotcols`の`_test.go`）を明記、depguardの強制範囲（`web` → `repository/**`のみ）を明記、`sqlutil`/`sqlitedb`・`web`・`bootstrap`・`router`の実import先（`config`・`version`・`safego`・`db`・`web/apierror`等）と`settings/`の`/setup`担当・テスト専用ディレクトリ一覧・`job_queries.go`/`doc.go`・旧外部テスト行数（940行）を実装に合わせて訂正、§4に`Repository`行と`web/handler`サブパッケージの実装場所を追加。なお1.24の「分割後構成を確定」は当時の目標構成を指し、実装は1.26〜1.30で適用済み。`integrations.md` §12（`internal/web/handler/activity`）・`er/tables-system.md`（`secrets`の実装パス）は#249で本文を更新済みだが自身に改訂履歴を持たないため本表で記録する | 分割後レビュー（#249〜#256） |
| 1.32 | 2026-10-01 | §6（`integrations.md`）をTypeSafe AI公式API（`POST {BaseURL}/v1/systemone`、型付き`questions`→`answers`）に合わせて書き直し: リクエスト/応答形式、Scout/Traderの質問表、厳格な応答検証、`question_version`の`scout-v2`/`trader-v2`、429/529はbackoff付き再試行・401/422は即失敗、`request_cost`はNULL、`BaseURL`はホスト名のみ | issue #263（旧実装は存在しない`/v1/scout`・`/v1/trader`を想定していた） |
| 1.33 | 2026-10-01 | §3の`logging/`にエラーログExporter、§4のLogging/Web行にエラーログのエクスポートを追記し、`overview/flows.md`に§10.6を新設 | issue #267 |
