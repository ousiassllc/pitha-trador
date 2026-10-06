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
| DBアクセス | database/sql（手書きSQL、`internal/repository/**`） | ORM・SQL生成ツール（sqlc等）は使わず、SQLを直接管理 |
| マイグレーション | golang-migrate（`database/sqlite`ドライバ、`modernc.org/sqlite`上） | `db/migrations` のSQLマイグレーション管理 |
| ベクトル検索 | `modernc.org/sqlite/vec`（sqlite-vecのpure Go移植、`vec0`仮想テーブル） | RAG類似検索（§7）。pgvector相当の機能をSQLite上で実現。CGO不要でクロスコンパイル可能（`environment/setup.md` §CI/CD参照） |
| Job Queue / Scheduler | 自前Workerプール（`jobs`テーブル + goroutine） | market-data, feature-calc, jev-scout, jev-trader, outcome-labeling, analytics の6キュー（`feature-calc`は互換用の空ジョブで、特徴量算出は`market-data`ジョブ内で完結する。Risk判定・Paper発注は`jev-trader`内で同期実行しキューを持たない）。単一プロセス前提のためRedis/River等の外部キューは不要。`architecture/er.md` の`jobs`テーブルで永続化・再起動時リカバリ |
| 周期実行 | robfig/cron ＋自前ループ | robfig/cron（`Scheduler`）は60秒フルスキャンと分単位以上の保守ジョブ（孤児回復・Outcome Labeling・ハートビート/リスク監視・自動再開・アップデート確認・日次の自己改善/バックアップ等）のトリガー。15-30秒の候補更新は`internal/bootstrap/candidates`の自前ループ（`min`+ジッター）、5-15秒の保有ポジション再評価は`internal/bootstrap/lifecycle.go`が起動する別goroutine（`heldposition.Monitor.Run`）で、いずれもcronのジョブ登録ではない |
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
│   ├── desktop/                  # Wailsエントリーポイント（main.go, app.go, notify.go, ws_listener.go, wails.json）。`main`は`cmd/server`と同じく`startup.RunMain`で始まる。`ws_listener.go`はWindowsのみの`/ws/...`専用ループバックリスナー。`--supervise`起動（main.goの`superviseSelf`）と`build/windows/installer/project.nsi`のStartupショートカット（自動起動）を含む
│   └── server/                   # ヘッドレス起動（Wails非依存のnet/httpサーバー。main.go, addr.go, shutdown.go。CI・WebView2が動かない環境向け）。`main`は`startup.RunMain`（ログ設定と致命的エラーのERROR記録・終了コード。#547）で始まり、`bootstrap.AcquireInstanceLock(bootstrap.AppLockName)`で`app.lock`を取得してから`bootstrap.Run`へ進む（取得失敗時は非0終了。#468）。`shutdown.go`はSIGINT/SIGTERM時のHTTP停止とWebSocketハンドラの終了待ち（`httpServer.shutdown`。§10.1の停止フロー）
├── internal/
│   ├── bootstrap/                # 両エントリーポイント共通の起動処理の組み立て役（composition root）。直下は DB open+マイグレーション・config/*.yamlの4段階解決（bootstrap.go）、`Services`組み立て・起動停止（services.go, services_build.go, lifecycle.go）、定数（constants.go）、Riskエンジン配線・取引時間判定・自己改善ジョブ（risk.go, session.go, selfimprove_job.go）、secrets読込とrouterオプション列の共通化（router_options.go: `LoadSecrets`/`RouterOptions`。#372）、起動時の銘柄マスタCSV同期（universe.go: `syncUniverse`・`PITHA_UNIVERSE_PATH`。#389）、多重起動ロックの取得（instance_lock.go: `AcquireInstanceLock`・`AppLockName`/`SupervisorLockName`。desktop/server共通の`app.lock`・`supervisor.lock`。#468）のみ
│   │   ├── candidates/           # 候補銘柄の定期更新（Fast Screener実行・最新Jev Trader判断と保有ポジションの候補への付与（#492）・jev-scoutのenqueue・更新間隔ティッカー。#246）
│   │   ├── marketdatajob/        # market-data / feature-calc（空ジョブ）ジョブハンドラ（板→Reading変換・特徴量算出・イベント再評価enqueue。#246）
│   │   ├── backtestsource/       # Backtest Engine向けのDB読み出しソース（`backtestsource.Source`。#246）
│   │   ├── heldposition/         # FR-SCHED-4 保有ポジション監視・Exit評価ループ（5〜15秒周期、最新板で再評価）
│   │   ├── rankingmeasure/       # FR-SCHED-8 kabu `/ranking`計測ループ（`scan.ranking_measure`でオプトイン。件数・`duration_ms`・`CurrentPriceTime`・HTTP/kabuコードだけをログに出し、価格は保存・出力しない。#652）
│   │   ├── newstargets/          # News Ingestの対象銘柄（Fast Screener候補＋保有ポジション銘柄。全銘柄は取得しない。#531）
│   │   ├── paperexec/            # Policy Engineのシグナル実行フック→Execution（Paper）のアダプタ
│   │   ├── alerts/               # 非機能§5.2のアラート宛先（構造化ログ・Slack）とサービス別Notifierの組み立て
│   │   ├── startup/              # desktop/server共通のプロセス起動処理（`RunMain`: ログ設定→`run`実行→致命的エラーのERROR記録と終了コード決定、`LogDir`/`EnvLogDir`=`PITHA_LOG_DIR`: ログディレクトリ解決。`internal/logging`のみに依存。#547・#551・#552）
│   │   └── universe/             # 銘柄マスタCSVのパース・検証と`instruments`へのupsert（`Parse`/`SyncFile`。`domain`のみに依存。#389）
│   ├── config/                   # config/*.yamlの型付きローダー、AES-256-GCM秘密情報ヘルパー（他の内部パッケージに依存しない）
│   │   └── strategyflow/         # テスト専用: strategy.yamlの起動時検証（`Validate`）・`scan.*`/`event_trigger`の既定値補完の回帰テスト。linterlyの2000行/ディレクトリ制限のため`config`直下から分離（#459）
│   ├── safego/                   # FR-SCHED-6 常駐goroutineのpanic回復（`Recover`/`Run`/`Try`/`Loop`。panicをスタック付きでslogに記録し、ループは次サイクルへ継続。他の内部パッケージに依存しない）
│   ├── httpbody/                 # 外部API応答ボディの上限付き読み取り（`ReadAll`・`DefaultMaxBytes`=4 MiB・`ErrTooLarge`。標準ライブラリのみに依存。`service/marketdata`・`service/jev`が使う）
│   ├── textutil/                 # 外部応答ボディ・エラー文字列のバイト数切り詰め（`Truncate`/`Excerpt`・`ErrorBodyExcerptBytes`=256。UTF-8のルーン境界で切り、`…`を付す。標準ライブラリのみに依存するリーフ。`service/jev`・`service/policy`・`service/assist`・`service/scheduler`が使う。#530）
│   ├── logging/                  # slog JSON出力の日次ローテーション（rotate.go）・30日超のgzipアーカイブ（archive.go）・エラーログの抽出とマスク（export.go・export_mask.go、読み取り専用。`requirements/non-functional.md` §5・§5.3）
│   ├── supervisor/               # --supervise起動時の子プロセス監視・指数バックオフ再起動（cmd/desktopのみが利用。非機能§3）
│   ├── singleinstance/           # ファイルロックによる多重起動ガード（cmd/desktopとcmd/serverが利用。OSがプロセス終了時にロックを解放）
│   ├── version/                  # ビルド時に埋め込むバージョン文字列（`ldflags -X`。自動アップデート判定で使用）
│   ├── domain/                   # ドメインモデル（標準ライブラリと基盤パッケージ`internal/config`のみに依存。`config`への依存は`policyproposal.go`のしきい値検証`config.IsProbabilityThreshold`に限る。#527）
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
│   │   ├── policyproposal.go     # policy_proposals相当
│   │   ├── scan.go               # スキャンサイクルの銘柄別判定・除外/欠損理由（`ScanCycle`/`ScanSymbol`/`ScreenReason`）
│   │   └── scan_query.go         # スキャン一覧の絞り込み・ページング・集計（`ScanQuery`/`ScanPage`/`ScanSummary`）
│   ├── repository/               # domainのみに依存（例外: `system`の`SecretsRepository`のみ`internal/config`のAES-256-GCMヘルパー）。直下にはファイルを置かず、リソース群ごとのサブパッケージ（#244）
│   │   ├── sqlutil/              # 共有ヘルパー: 時刻のSQLite表現変換（`FormatTime`/`ParseTime`）・`Nullable*`/`Null*`スキャナ・`RowScanner`/`Execer`/`Executor`インターフェース。標準ライブラリのみに依存するリーフ
│   │   ├── sqlitedb/             # SQLite接続（`Open`）・golang-migrateマイグレーション・`BackupTo`・sqlmw計装ドライバ・DB書き込み失敗検知フック（`DBWriteFailures`）。`domain`とマイグレーションSQLの`go:embed`元`db`のみに依存するリーフ
│   │   ├── market/               # instruments / market_snapshots（`InstrumentRepository`, `SnapshotRepository`）。`snapshotcols`を本番コードで使う
│   │   ├── jobqueue/             # jobsテーブル・キュー名/状態定数（`Job`, `JobRepository`, `ErrJobNotFound`）
│   │   ├── judgement/            # jev_decisions / policy_proposals（`DecisionRepository`, `ProposalRepository`）
│   │   ├── calibration/          # calibration_outcomes / calibration_label_skips（`CalibrationRepository`。`judgement`から分離: linterlyのディレクトリ2000行制限、issue #601）
│   │   ├── trading/              # trade_signals / paper_orders / positions（`SignalRepository`, `OrderRepository`, `PositionRepository`）
│   │   ├── system/               # kill_switch_events / runtime_settings / secrets（`KillSwitchRepository`, `RuntimeSettingsRepository`, `SecretsRepository`）
│   │   ├── decisiontrade/        # クローズ済みポジションと開始時のJev判断の結合読み取り（FR-CAL-2の帯別PnL用。複数リソース群を跨ぐ読み取りの置き場。本番コードは`domain`のみに依存し、他テーブルはSQLで直接結合する）
│   │   └── snapshotcols/         # market_snapshotsのFeature列とdomain.Featureの対応表（INSERT/SELECT用。`market`が本番コードで使う`domain`のみに依存するリーフ）
│   ├── service/                  # domain, repositoryに依存
│   │   ├── marketdata/           # kabuステーションAPIクライアント（REST+PUSH WS）
│   │   │   ├── feedfail/         # テスト専用: GetBoard失敗のうちmarket_data_downの連続失敗に数えるもの（#532）
│   │   │   ├── logflow/          # テスト専用: GetBoardの構造化ログ（ディレクトリ行数上限対応で移動）
│   │   │   ├── infolimit/        # 情報API・銘柄登録のプロセス全体レート制限（公式10件/秒、既定8。issue #514）
│   │   │   ├── quote/            # kabuステーションAPIの板を一般的なbid/askへ変換（`Bid`/`Ask`/`SpreadBps`。売/買の入れ替えの単一定義。`bootstrap/marketdatajob`と`bootstrap/heldposition`が共用。`marketdata`・`featureengine`に依存する本番コード）
│   │   │   ├── rateflow/         # テスト専用: 情報APIレート上限と4001006の回帰テスト（#514）
│   │   │   ├── rankingflow/      # テスト専用: `MeasureRanking`（`/ranking`の件数・同順位・`CurrentPriceTime`への縮約）の回帰テスト（#652）
│   │   │   └── tokenflow/        # テスト専用: トークン発行・状態・失効時の再発行の回帰テスト（#621）
│   │   ├── marketcalendar/       # 東証の立会時間・祝日判定（Scheduler SessionGate・Risk・Execution・heldpositionが依存。ネットワーク/tzdata非依存の純粋ルール）
│   │   ├── featureengine/        # 特徴量算出
│   │   │   ├── eventtrigger/     # FR-SCAN-1/2 イベントトリガ判定（Detect）
│   │   │   └── marketcontext/    # 市場コンテキスト（指数リターン・市場ブレッドス）の`Loader`。全銘柄共通値を30秒キャッシュして共有（#622。旧`marketcontextflow`のテストも同居）
│   │   ├── pushfeed/             # 起動時の銘柄登録・PUSH購読とPUSH板キャッシュ（REST GetBoardへのフォールバック付き）
│   │   ├── symbolcache/          # kabuステーションAPI銘柄情報（貸借・値幅上下限）の1営業日キャッシュ（issue #511）
│   │   ├── screener/             # Fast Screener・screen_score算出
│   │   ├── jev/                  # Jevアダプタ（client.go, evaluate.go, scout.go, trader.go, schemas.go, questions*.go, prompt_version.go, systemone/=ワイヤ層, jevtest/=テスト用フェイク, clientflow/=Clientテスト）
│   │   ├── rag/                  # 埋め込み生成・sqlite-vec類似検索（§7）
│   │   ├── policy/                # Policy Engine
│   │   ├── risk/                  # Risk Engine（Kill Switch含む）。`Engine`のメソッド群（Check・状態遷移・監視・警告）は結合が強いため直下の1パッケージに保つ（#247）
│   │   │   ├── sizing/            # FR-ENTRY-3 ポジションサイズ算出（リスク上限からの純関数）
│   │   │   ├── repoportfolio/     # risk.PortfolioProviderの本番実装（positionsから建玉・日次損失・連敗を導出）
│   │   │   ├── correlation/       # FR-RISK-1「同じ方向に重ねない」ゲート（同方向ポジション上限`SameDirectionExceeded`・市場逆行判定`MarketAdverse`の純関数。`Engine.Check`が利用する本番コード）
│   │   │   ├── multinotify/       # risk.Notifierを複数チャネルへ扇状に配信
│   │   │   ├── killswitchflow/    # テスト専用: Kill Switch発動〜再開フローの回帰テスト（行数上限のためriskから分離）
│   │   │   ├── checkflow/         # テスト専用: `Engine.Check`（FR-RISK-1）の回帰テスト（#247）
│   │   │   └── monitorflow/       # テスト専用: 定期監視・日次損失警告・Notifierの回帰テスト（#247）
│   │   ├── execution/             # Paper/kabu発注実行
│   │   │   ├── enrich/            # jev_decisionsのresponse_json内のJev Trader応答項目（regime等）をJevDecisionへ復元（Exit条件・Symbol Detail・バックテスト入力（`bootstrap/backtestsource`）・RAGの類似判断（`rag`）が共用）
│   │   │   ├── fillflow/          # テスト専用: 約定モデル（FR-ENTRY-8）・立会時間の`execution.Engine`テスト（行数上限のためexecutionから分離、#248/#509）
│   │   │   ├── exitflow/          # テスト専用: FR-EXIT-1 Exit条件（`EvaluateExit`）の回帰テスト（行数上限のためexecutionから分離、#248/#509）
│   │   │   ├── vwapcross/         # FR-EXIT-1 VWAP逆クロスのクロス判定・前回観測トラッカー（execution.Engineが依存する本番コード）
│   │   │   ├── pendingfill/       # テスト専用: PENDING指値Entryの約定・重複PENDING・孤児PENDING成行の掃除・非正価格拒否の回帰テスト（行数上限のためexecutionから分離、#348）
│   │   │   ├── closerace/         # テスト専用: 決済競合（手動決済/CloseAll/Exitモニタ）の回帰テスト（行数上限のためexecutionから分離）
│   │   │   ├── closeflow/         # テスト専用: `Engine.Close`/`CloseAll`の回帰テスト（行数上限のためexecutionから分離、#248）
│   │   │   └── latestdecision/    # テスト専用: 最新Jev Trader判断の件数窓・経過時間上限なしの単一定義（`DecisionRepository.LatestTrader`/`LatestTraderByInstruments`/`LatestScout`と`execution.Engine.State`の`LatestTraderDecision`）の回帰テスト（行数上限のためexecution/judgementから分離、#496/#497/#499）
│   │   ├── fillmodel/             # 約定モデル（FR-ENTRY-8。呼値・スプレッド・滑り・手数料。Paper Trading(`execution`)とBacktest Engineが共用する本番コード、#509）
│   │   ├── calibration/           # Outcome labeling・Brier/Log Loss算出
│   │   ├── backtest/              # Backtest Engine（Walk Forward評価・Governor用シャドーバックテスト）
│   │   ├── assist/                # Luna/Sol/Opusアダプタ
│   │   │   ├── luna.go
│   │   │   ├── sol.go
│   │   │   └── opus.go
│   │   ├── newsfeed/              # News Ingest: 外部ニュースフィード定期取得→Luna呼び出し（§13）
│   │   ├── selfimprove/           # Sol提案生成〜Opusレビュー〜適用/ロールバック（§8）
│   │   │   └── governorflow/      # テスト専用: Governor（`RunDaily`/`EvaluateProposal`/`ApplyApproved`/`TrackAndRollback`）の回帰テスト。linterlyの2000行/ディレクトリ制限のため`selfimprove`直下から分離（#450〜#452, #455）
│   │   ├── notify/                # Slack Incoming Webhookによる即時アラート送信
│   │   ├── updater/               # GitHub Releases自動アップデート（検知・安全ゲート・検証、desktopのみ配線、§9）
│   │   │   ├── tempcleanup/       # 更新後に残るインストーラー一時ディレクトリの起動時掃除（`CleanupStale`、issue #590）。`updater`と`cmd/desktop`から使うリーフ
│   │   │   └── checkflow/         # テスト専用: ダウンロード堅牢化・Status分類のテスト。`updater`の行数上限のため分離（#398）
│   │   ├── activityfeed/          # jobs/jev_decisions/kill_switch_events集約の読み取り専用フィード（System Activity Log向け、§12）
│   │   ├── insight/               # 判断履歴・シグナル・実績サマリーの読み取り専用クエリ（`api/endpoints.md` §5）
│   │   ├── backup/                # 日次SQLiteバックアップ（daily 90日 + weekly gzip、`requirements/non-functional.md` §3）
│   │   ├── retention/             # jobs / market_snapshotsの期限切れ行パージ（`requirements/non-functional.md` §3）
│   │   └── scheduler/             # 自前Workerプール定義・周期ジョブ登録
│   │       ├── updatecheck/       # アップデート確認ジョブ（周期確認の取得失敗・安全ゲート保留を指数バックオフで再試行。`scheduler`の行数上限のため分離、§9。Scheduler の更新確認配線の回帰テストも同居、#400）
│   │       ├── orphans/           # 全キュー共通の孤児`running`ジョブの`failed`回復（`Fail`/`FailAll`。しきい値は固定10分、Schedulerが1分ごとに実行。`requirements/non-functional.md` §2.1、#424/#425）
│   │       ├── maintenance/       # 日次ハウスキーピング（バックアップ・データ保持パージ・ログアーカイブ）のcatch-up実行。最終成功日をruntime_settingsへ保持し、起動時と10分ごとに未実行分を実行
│   │       ├── maintenanceflow/   # テスト専用: バックアップ・データ保持パージ・ログローテーションの各ジョブ呼び出しの回帰テスト（行数上限のためschedulerから分離、#248）
│   │       └── outcomeflow/       # テスト専用: `EnqueueOutcomeLabeling`（ペア走査・pending/running重複排除・ラベル不能ペアの除外）の回帰テスト（行数上限のためschedulerから分離、#481）
│   ├── router/                    # SSR + API ルーティング定義（Huma登録含む）。`web/handler/**`・`web/apierror`・`web/insightapi`・`web/middleware`・`config`・`static/src`（go:embed）を参照する
│   │   ├── options.go             # Option群（依存注入）
│   │   ├── router.go              # New（Gin Engine組み立て）
│   │   ├── router_middleware.go   # middlewareの適用順
│   │   ├── router_routes.go       # ルート登録
│   │   ├── router_noroute.go      # 未定義パス・メソッドの404（`/api/v1`はproblem+json、ページはErrorPage、HTMXはトースト、issue #605）
│   │   ├── static.go              # 静的アセット配信（go:embed、`PITHA_STATIC_DIR`によるディスク上書き）
│   │   ├── ws_listener.go         # `WebSocketOnly`（`/ws/...`のUpgradeだけを通すラッパー。desktopがWails AssetServerと別のloopbackリスナーで使う）
│   │   ├── analysisflow/          # テスト専用: 分析系ルート（ポリシー提案）の回帰テスト（行数上限のためrouterから分離、#248）
│   │   ├── apiroutes/             # テスト専用: `/api/v1`のOpenAPI設定（servers・スキーマリンク）とInsight APIルート登録の回帰テスト（行数上限のためrouterから分離、#314）
│   │   ├── staticroute/           # テスト専用: `/static`配信（vendor・esbuild成果物・`PITHA_STATIC_DIR`上書き・Swagger有効化）の回帰テスト（行数上限のためrouterから分離、#314）
│   │   ├── systemheader/          # テスト専用: HeaderのKill Switchパネルと`/api/v1/system/status`の許可アクションの回帰テスト（行数上限のためrouterから分離、#314）
│   │   └── wslistener/            # テスト専用: `WebSocketOnly`の統合テスト（行数上限のためrouterから分離）
│   └── web/
│       ├── apierror/              # /api/v1 の huma.NewError 上書き（5xx は固定メッセージのみ返し原因を slog へ。issue #215）
│       ├── handler/               # Ginハンドラ。直下は`doc.go`のみで、すべて責務別サブパッケージ（#245・#370）: scanner/（scanner.go, scanner_scan.go: `GET /scanner/scan`・`GET /api/v1/scanner/scan`）, performance/（performance.go, performance_view.go）, calibration/（calibration.go）, proposals/（proposals.go）, swagger/（swagger.go）, symbol/, system/, settings/, activity/, shared/
│       │   ├── shared/            # 共有ヘルパー（`render.go`のTempl描画（`RenderHTML`：バッファに描画し、失敗時はログ＋500で部分的な200を返さない）・`action_error.go`のアクションエラー整形（`RespondActionError`/`RespondPageError`）・`RenderErrorPage`（routerのmiddlewareも使用）・`ws_poll.go`のWebSocketポーリング（`PollWebSocket`）とJSONフレーム送信（`WriteJSON`）・`ws_accept.go`のWebSocket Upgrade（`AcceptWebSocket`：`middleware.WebSocketBase`設定時は`wails.localhost`等のOriginホスト名も許可））。Templ（`web/atoms`・`web/pages`）・標準/外部ライブラリのみに依存するリーフで、他のhandlerサブパッケージに依存しない
│       │   ├── symbol/            # Symbol List/Detail/Page/Close と `/ws/symbols/:symbol`（symbol*.go）
│       │   ├── system/            # System状態・Kill Switch操作・`/ws/system`・自動アップデートUI（system.go, system_ws.go, update.go）・エラーログDL（`GET /api/v1/logs/errors`、error_log.go）・市況データ接続バナー（`GET /system/marketdata-status`、marketdata.go）
│       │   ├── settings/          # `/settings`・認証情報の保存/削除・初回セットアップ画面`/setup`（settings.go）。接続先別のフィールド定義は settings_fields.go
│       │   └── activity/          # System Activity Log（`GET /api/v1/activity`・`/ws/activity`）
│       ├── insightapi/            # decisions/signals/performance の読み取り専用JSON API（Huma登録、`service/insight`を使用）
│       ├── middleware/            # SecurityHeaders（`security_headers.go`: CSP/`X-Content-Type-Options`/`X-Frame-Options`/`Referrer-Policy`、`/swagger`用`SwaggerCSP`。最外周。issue #378）, HostGuard（Host/Origin検証）, Session（Cookie+CSRF）, RequestLog, Recovery, 操作者ハートビート記録（§10.4）, Setup Guard（§10.5）, SystemState, `ws_base.go`のWebSocketBase（`<meta name="ws-base">`用のコンテキスト値）, `error_page.go`のErrorPageRenderer（Recovery/Sessionが返すエラーページの描画注入）
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
│       ├── csp/                   # lightweight-charts-style-hash.test.ts（CSPのstyle hashとインストール版の一致検証）
│       ├── img/                   # logo.svg（Header表示用、go:embed対象）
│       ├── vendor/                # htmx.min.js（checked-in、go:embed対象）
│       ├── embed.go               # `//go:embed dist img vendor`（パッケージ`staticassets`）
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

レイヤー（import方向の境界）は最上位ディレクトリ（`domain`/`repository`/`service`/`web`/`router`/`bootstrap`）で決まり、**1パッケージ（ディレクトリ）は1つの責務**を持つ。旧規約の「レイヤー内の全ファイルを1ディレクトリへ平坦に置く」は廃止し、ディレクトリ行数上限（linterly: 300行/ファイル・2000行/ディレクトリ。除外で回避しない）を超える見込みのレイヤーは責務別サブパッケージへ分割する。ツリーの`service/`配下と同様、サブパッケージはディレクトリ単位（責務）で記載し、新規サブパッケージはファイル名を列挙せずディレクトリ行のみ追加する（ファイル構成はパッケージコメントを一次情報とする）。`*_test.go`のみのディレクトリ（`execution/closerace`・`execution/closeflow`・`execution/pendingfill`・`risk/killswitchflow`・`risk/checkflow`・`risk/monitorflow`・`featureengine/marketcontextflow`・`scheduler/maintenanceflow`・`selfimprove/governorflow`・`jev/clientflow`・`updater/checkflow`・`router/analysisflow`・`router/apiroutes`・`router/staticroute`・`router/systemheader`・`router/wslistener`・`config/strategyflow`・`marketdata/rateflow`）は行数上限を満たすためにテストを分離したもので、本番コードではない。

`repository`（#244）・`web/handler`（#245）・`bootstrap`（#246）・`service/risk`（#247）はいずれも分割済みで、上のツリーは実装と一致している（各Issueは完了時に本ツリーが実装と一致することを受け入れ条件とする）。**サブパッケージ共通の規約**:

- 兄弟サブパッケージ同士は本番コードでimportしない。共有コードは`sqlutil`/`sqlitedb`/`handler/shared`のようなリーフ・ヘルパー用サブパッケージへ切り出す。リソース群を跨ぐ読み取りは`decisiontrade`のように専用サブパッケージ（本番コードは`domain`のみに依存し、他テーブルはSQLで直接結合する）へ置く。**本番コードの例外は`market` → `snapshotcols`のみ**（`market_snapshots`のFeature列対応表を共有する`domain`のみに依存するリーフ。逆向きは禁止）
- **テスト専用のクロスリソース読み取り例外**: `decisiontrade`・`snapshotcols`の外部テスト（`_test.go`）は、データ準備と列対応の整合検証のため兄弟群（`judgement`/`market`/`trading`。`snapshotcols`のテストは`market`）の公開APIと`sqlitedb.Open`をimportしてよい。この例外は`_test.go`に閉じ、本番コードへ広げない。それ以外の群のテストは他リソース群のデータをSQLで直接用意する
- サブパッケージは、親パッケージがその子をimportする場合に親をimportしない（循環回避）。親（`bootstrap`）は組み立て役として子を参照してよく、子は依存を引数（構造体・小さなインターフェース）で受け取る。**例外**: 親が本番コードでimportしない補助サブパッケージは親の公開型を参照してよい。`service/marketdata/quote`（`marketdata.Board`）・`service/jev/jevtest`（`jev.Client`向けのhttptestハンドラ。`jev`の公開型を参照）・`service/risk/multinotify`（`risk.Notifier`）の3つで、いずれも親は子をimportせず（子をimportするのは`bootstrap`配下の組み立て役とテストのみ）、循環は起きない
- 分割後の呼び出し元はサブパッケージ名で修飾する（例: `jobqueue.Job`、`sqlitedb.Open`）。センチネルエラーは返すパッケージが定義する（他レイヤーが分類する必要がある場合は従来どおり`domain/`に置く）
- テストは対象コードと同じサブパッケージへ移設する。`service/risk`のようにパッケージ内結合が強くコードを分割できない場合、または本番コードは上限内でも外部テスト（`package X_test`）を足すとディレクトリ2000行を超える場合のみ、外部テストをテスト専用サブパッケージ（`*flow`等）へ分離する。各テスト専用サブパッケージは自前のヘルパーを持ち、兄弟テストパッケージ同士はimportしない
- `.linterlyignore`に手書きソースの除外を置かない。許容するのは自動生成物`*_templ.go`・実行時ログ`**/logs/**`・ライセンス全文`LICENSE`（手書きソースではない定型文）のみ（詳細は`environment/setup.md`）

#### 分割後の構成（後続Issueの設計判断）

| パッケージ | 分割方針 | 依存方向 |
|-----------|---------|---------|
| `repository`（#244） | テーブルの結合度でリソース群に分ける（`market`/`jobqueue`/`judgement`/`calibration`/`trading`/`system`）。各リポジトリ型はそのテーブルを所有する群に置き、群内のリポジトリ実装のファイル名は`*_repo.go`を維持する（`jobqueue`の`job_queries.go`等の補助クエリと、各群の`doc.go`（パッケージコメント）は例外）。`formatTime`/`nullable*`/`rowScanner`/`execer`/`sqlExecutor`は公開名（`FormatTime`/`Nullable*`/`RowScanner`/`Execer`/`Executor`）にして`sqlutil`へ集約し、`db.go`・`dbmw.go`は`sqlitedb`へ。既存の`decisiontrade`/`snapshotcols`は現位置を維持 | 群 → `sqlutil`・`domain`（`market`のみ`snapshotcols`も可）。`sqlitedb` → `domain`・マイグレーションSQLの`go:embed`元`db`。群同士・本番コードでの群→`sqlitedb`は禁止（テストのDB準備のみ`_test.go`から`sqlitedb.Open`可。`decisiontrade`・`snapshotcols`のテストは前節の例外）。`system`のみ`internal/config`も可 |
| `web/handler`（#245・#370） | 画面/APIの責務別に`symbol`/`system`/`settings`/`activity`/`scanner`/`performance`/`calibration`/`proposals`/`swagger`へ分け、直下は`doc.go`のみ（直下に残していた一覧・分析系ハンドラも#370で`web/handler`が2000行上限を超えたため分割）。`settings/`のテストは表示・保存・削除・セットアップの観点でファイルを分割している（共通のフェイクは`testutil_test.go`）。`action_error.go`・`ws_poll.go`（と各WebSocketハンドラが共用していた`writeJSON`）は`shared`へ移し公開名にした（全サブパッケージ・`router`が共用。`RespondActionError`/`RespondPageError`/`PollWebSocket`/`WriteJSON`/`RenderErrorPage`）。クライアント切断で即終了する回帰テスト（#127）は各WebSocketハンドラを所有するパッケージ（`scanner`・`symbol`・`system`）が個別に持ち、テストが兄弟を跨がない | 各サブパッケージ → `service`・`domain`・`shared`・Templ（`web/atoms`・`web/pages`等）。基盤パッケージ`internal/config`（`symbol`・`settings`）・`internal/version`（`system`）も参照する。`shared` → Templ のみ（handler・`service`に依存しない）。`repository/**`は不可。サブパッケージ同士・`handler`直下への逆import禁止。`router` → `shared`・全サブパッケージ |
| `bootstrap`（#246） | 直下は組み立て役（`Run`/`State`/`Services`/`BuildServices`/`Start`/`Stop`、`LoadSecrets`/`RouterOptions`（desktop/server共通のsecrets読込とrouterオプション列。#372）、起動時の銘柄マスタCSV同期`syncUniverse`（#389））のみ。ジョブ/ループ単位の責務を`candidates`/`marketdatajob`/`backtestsource`へ切り出す（既存の`heldposition`/`paperexec`/`alerts`/`universe`と同格）。切り出し先は`Services`ではなく必要な依存だけをフィールドに持つ構造体（`candidates.Refresher`・`marketdatajob.Handler`・`backtestsource.Source`）に対するメソッドとして実装し、直下の`BuildServices`が引数で組み立てる（`marketdatajob.BoardSource`のような小さなインターフェース経由で依存を受ける）。テストは各サブパッケージ内で最小のフェイク/実DBを組み立て、`Services`全体には依存しない。直下のテストは`BuildServices`の配線検証のみ | 直下 → 全サブパッケージ・`service`・`repository`・`sqlitedb`・`internal/router`・`internal/web/handler/{scanner,symbol}`（`router_options.go`のみ）。サブパッケージ → `service`・`domain`・`repository`群と、基盤パッケージ`internal/config`（`candidates`・`marketdatajob`・`alerts`）・`internal/safego`（`candidates`・`heldposition`）のみ（`universe`は`domain`のみ）。親・兄弟への依存禁止 |
| `service/risk`（#247） | `Engine`のメソッド群（`check.go`のCheck・`state.go`の状態遷移・`losslimit.go`の損失上限・`monitor.go`の定期監視・`warning.go`の日次損失警告・`autoresume.go`・`baseline.go`・`session.go`・`settings.go`・`sizing.go`）は`Engine`のunexported状態を共有するため**分割せず**`risk`直下に保つ（本番コードのみで約1.6k行＝ディレクトリ上限内。直下に`_test.go`は置かない）。行数上限は、`package risk_test`の外部テスト（旧940行）を`killswitchflow`に倣ってテスト専用サブパッケージへ移して満たした（`checkflow`=`Engine.Check`・`monitorflow`=定期監視・日次損失警告・Notifier）。各テスト専用サブパッケージは`helpers_test.go`に自前のフェイク（`fakePortfolio`/`fakeCloser`/`fakeNotifier`等）とテストDB準備（`sqlitedb.Open`）を持ち、兄弟テストパッケージをimportしない | テスト専用サブパッケージ → `risk`（公開API）・`domain`・`config`・`repository/**`（`Engine`へ渡す依存の組み立てとテストDB準備のみ）。本番の依存方向（`risk` → `domain`・`repository/**`・`config`）は変更しない |

### レイヤー依存ルール（HALT準拠）

依存は上から下への一方向のみ。逆方向のimportは禁止。**ルールはレイヤーのサブツリー全体（サブパッケージを含む）に適用する。**

```text
handler → service → repository → domain
   ↓
 Templ テンプレート（atoms/molecules/organisms/pages）
```

- `domain/`: 標準ライブラリと基盤パッケージ`internal/config`（`policyproposal.go`のしきい値検証`config.IsProbabilityThreshold`のみ。`config`は他の内部パッケージに依存しないため循環しない。issue #527）にのみ依存し、他のレイヤー（`repository`・`service`・`web`等）には依存しない。純粋なビジネスロジック（例: Risk Engineのしきい値判定ロジック自体はdomainに置き、DB/HTTPアクセスはrepository/serviceに分離）。この例外を他のdomainファイルへ広げない
- `repository/**`: `domain/` のみに依存（`sqlutil`は標準ライブラリのみ、`sqlitedb`は標準ライブラリ・外部ライブラリ・`domain`・マイグレーションSQLを`go:embed`する最上位`db`パッケージのみ）。例外として`SecretsRepository`（issue #57、`repository/system`）のみ`internal/config`のAES-256-GCMヘルパー（依存を持たない、`domain`と同格の基盤パッケージ）にも依存する。この例外は`system`パッケージ内に閉じ、`sqlutil`等の共有サブパッケージや他のサブパッケージへ広げない。サブパッケージ間の依存方向は前節の規約に従う
- `internal/safego`: 標準ライブラリのみに依存し、`internal/config`・`domain`と同格の基盤パッケージ。`service/`・`bootstrap`・`cmd/server`のいずれからも参照できる（常駐goroutineのpanic回復、FR-SCHED-6）
- `internal/httpbody`: 標準ライブラリのみに依存し、`internal/safego`と同格の基盤パッケージ。`service/`から参照できる（kabuステーション・Jevの応答ボディを4 MiBで打ち切る。超過は`ErrTooLarge`。連携側の扱いは`overview/integrations.md` §5・§6）。Slack Webhookのエラー応答は`httpbody`を使わず`service/notify`内で先頭4 KiBに切り詰めて（`...(truncated)`付き）エラー文に埋め込む
- `internal/textutil`: 標準ライブラリのみに依存し、`internal/safego`・`internal/httpbody`と同格の基盤パッケージ。`service/`から参照できる（外部APIの非200応答ボディやエラー文字列を、DB列・ログへ載せる前にバイト数で切り詰める`Truncate`/`Excerpt`。`ErrorBodyExcerptBytes`=256）
- `service/**`: `domain/`, `repository/**` に依存。`marketdata`/`jev`/`assist`など外部I/OはこのレイヤーでHTTPクライアントとして実装する
- `web/**`（`web/handler/**`を含む）: `service/`, `domain/` に依存（基盤パッケージ`internal/config`・`internal/version`も、`web/handler/{symbol,settings,system}`と`organisms`が参照する）。`repository/**` を直接使わない（`.golangci.yml` の depguard `web-no-repository` が `internal/web/**` から `internal/repository` **および全サブパッケージ**（`pkg`はプレフィックス一致）への import を lint で拒否する。サブパッケージ追加でルールの書き換えは不要）。**depguardが強制するのはこの`web` → `repository/**`と、次項のTempl層 → `service/**`の2方向のみ**で、`repository`・`web/handler`・`bootstrap`のサブパッケージ間（兄弟同士）のimport禁止、`repository`の依存先の制限、`router`・`bootstrap`の参照範囲などは規約でありlintでは強制されない（レビューで担保する）。repositoryが返すセンチネルエラーのうちhandlerが分類する必要があるもの（例: `domain.ErrPositionNotFound`）は`domain/`に定義し、repositoryはそれを返す
- `web/{atoms,molecules,organisms,pages,layout}`（Templ層）: `domain/`・基盤パッケージ・下位のTempl層にのみ依存し、`service/**` をimportしない（depguard `templ-no-service`が lint で拒否する）。`service`の戻り値型（`backtest.Metrics`・`insight.Performance`・`execution.SymbolState`等）は`web/handler`が表示用のplain props（例: `organisms.PerformanceSummary`・`pages.PerformanceResult`）へ写像して渡す。service側の型変更はhandlerのコンパイルエラーで止まり、テンプレートへ波及しない（`components/overview.md` §3、issue #380）
- `bootstrap/**`・`cmd/*`: 組み立て役として全レイヤー（`repository/**`含む）を参照してよい。`bootstrap`の子パッケージは親を参照しない（`internal/config`・`internal/safego`等の基盤パッケージは参照してよい）
- `router/`: `web/handler/**` を参照してルートを定義する。あわせて`web/apierror`・`web/insightapi`・`web/middleware`・`internal/config`・`static/src`（go:embedされた静的アセット）も参照する。`router`のテストは`repository/sqlitedb`・`repository/system`を実DB準備のためimportしてよい（depguard対象外）

## 4. コンポーネント責務

| コンポーネント | 責務 | 実装場所 |
|---------------|------|---------|
| Market Data Client | kabuステーションAPIからの1分足・板・約定データ取得（REST）、リアルタイム価格のPUSH WebSocket受信、トークン管理。情報API・銘柄登録は`infolimit`でプロセス全体の秒間上限を守る。`MeasureRanking`（`ranking.go`）は`GET /ranking`の応答を件数・同順位の重複数・`CurrentPriceTime`だけへ縮約する計測専用の取得で、価格はデコードしない（FR-SCHED-8、#652）。`rateflow`はテスト専用 | `internal/service/marketdata`（`infolimit`, `rateflow`） |
| Feature Engine | 価格・VWAP・出来高・ボラティリティ・板/約定・市場コンテキスト特徴量の算出（`requirements/functional.md` §4.1）。`marketcontext`は市場コンテキストの算出とキャッシュ | `internal/service/featureengine`（`eventtrigger`, `marketcontext`） |
| Fast Screener | 数値フィルター・screen_score算出・上位N銘柄選定（§4.2） | `internal/service/screener` |
| Jev Adapter (Scout/Trader) | 構造化状態と型付き質問（`noul`/`choice`）をTypeSafe AI公式API（`POST /v1/systemone`）へ送信し、回答をScoutResponse/TraderResponseへ変換する（§4.4, §4.5, §6）。ワイヤ層は`systemone`、テスト用フェイクは`jevtest`、`clientflow`はテスト専用 | `internal/service/jev`（`systemone`, `jevtest`, `clientflow`） |
| RAG Context Builder | 現在の状態ベクトルからsqlite-vecで類似過去局面を検索し、Jevへのfew-shot文脈を構築する（§7、FR-RAG-1〜4。FR-RAG-5は将来拡張で未実装）。類似判断の`regime`復元に`execution/enrich`を利用する | `internal/service/rag` |
| Policy Engine | Jev出力をトレードシグナルへ変換（§4.6） | `internal/service/policy` |
| Market Calendar | 東証の立会時間（前場/後場）・祝日判定。立会時間外の市場データ取得・Jev呼び出し・新規発注停止（Scheduler SessionGate）、FR-RISK-6のハートビート判定、FR-EXIT-1の引け前強制決済が参照する。`PhaseAt`は時刻を昼休み・立会時間外/寄り（9:00・12:30の最初の1分足）/ザラ場/引けのクロージング・オークション（15:25〜）に分類し、約定モデルが参照する（`requirements/non-functional.md` §3、FR-ENTRY-8） | `internal/service/marketcalendar` |
| Risk Engine | ポジションサイズ・損失上限・Kill Switch（§4.7）。全レイヤーの中で最終拒否権を持つ。サイズ算出（FR-ENTRY-3）・同方向ゲート（FR-RISK-1、`correlation`）・ポートフォリオ状態の導出・複数チャネル通知はサブパッケージ。`killswitchflow`・`checkflow`・`monitorflow`はテスト専用 | `internal/service/risk`（`sizing`, `correlation`, `repoportfolio`, `multinotify`, `killswitchflow`, `checkflow`, `monitorflow`） |
| Execution | Paper Entry/Exit・kabuステーションAPI発注（実売買移行時）。約定価格・手数料は`fillmodel`の約定モデル（呼値・スプレッド・滑り・手数料・昼休み・寄り引け。FR-ENTRY-8）で決め、バックテスト（`backtest`）と同じモデルを共用する。Jev Trader応答項目の復元は`enrich`（`execution`のExit条件・Symbol Detail、`bootstrap/backtestsource`のバックテスト入力、`rag`の類似判断が共用）、FR-EXIT-1 VWAP逆クロス判定は`vwapcross`。`closerace`・`closeflow`・`pendingfill`・`latestdecision`・`fillflow`・`exitflow`はテスト専用（`latestdecision`は最新Trader判断の単一定義の回帰テスト、`fillflow`は約定モデル・立会時間のテスト） | `internal/service/execution`（`enrich`, `vwapcross`, `closerace`, `closeflow`, `pendingfill`, `latestdecision`, `fillflow`, `exitflow`）・`internal/service/fillmodel` |
| Calibration | Outcome Labeling、Brier Score/Log Loss/ECE算出（§4.12） | `internal/service/calibration` |
| Self-Improvement Governor | Sol提案の受理、Opusレビュー依頼、シャドーバックテスト実行、`runtime_settings`への適用・ロールバック（§8、FR-SELFIMPROVE-1〜7）。`governorflow`はテスト専用 | `internal/service/selfimprove`（`governorflow`） |
| Luna/Sol/Opus Adapter | ニュース分類（Luna）・振り返り分析（Sol）・提案レビュー（Opus）のAPI呼び出し | `internal/service/assist` |
| Scheduler/Worker | `jobs`テーブルを介した自前Workerプールによるキュー処理・周期実行トリガー（§4.10）。`scan.full_scan_enabled: false`のとき`WithFullScanDisabled`（`fullscan.go`）で60秒フルスキャンのcronトリガーを登録せず`market-data`全件投入を行わない（FR-SCHED-7、#652）。`updatecheck`はアップデート確認ジョブの再試行、`orphans`は孤児`running`ジョブの`failed`回復、`maintenanceflow`・`outcomeflow`はテスト専用 | `internal/service/scheduler`（`updatecheck`, `maintenance`, `maintenanceflow`, `outcomeflow`, `orphans`） |
| Activity Feed | `jobs`/`jev_decisions`/`kill_switch_events`を集約し、System Activity Log向けのキュー状況・直近アクティビティを提供（新規永続テーブルなし、§12）。HTTP/WebSocket公開は`web/handler/activity` | `internal/service/activityfeed`・`internal/web/handler/activity` |
| Backtest Engine | Walk Forward評価とGovernor用シャドーバックテスト（未来情報混入の検査・損益指標算出。§8）。再現範囲は簡略化されており、Exitは固定SL/TP/最大保有時間のみ・Risk Engine不適用。約定はPaper Tradingと同じ約定モデル（呼値・スプレッド・滑り2bps/板寄せ5bps・手数料0bps・昼休み・寄り引け、`fillmodel.Default`）で行う（`requirements/functional/components-platform.md` FR-BT-4） | `internal/service/backtest` |
| Notifier | Slack Incoming Webhookによる即時アラート送信（Kill Switch発動・障害等。§10.3） | `internal/service/notify` |
| Updater | GitHub Releasesの新版検知・安全ゲート（建玉なし・Kill Switch非発動・直近発注なし）・インストーラ検証。desktopビルドのみ配線（§9）。`checkflow`はテスト専用 | `internal/service/updater`（`checkflow`） |
| Insight | 判断履歴・シグナル・実績サマリーの読み取り専用クエリ（`api/endpoints.md` §5）。HTTP公開は`internal/web/insightapi` | `internal/service/insight` |
| Backup | 日次SQLiteバックアップ（daily 90日保持 + ISO週ごとのweekly gzip、`requirements/non-functional.md` §3） | `internal/service/backup` |
| Retention | `jobs`（成功7日・失敗30日）・`market_snapshots`（90日）の期限切れ行のパージ。監査系テーブルは対象外 | `internal/service/retention` |
| Background Task Guard | 常駐goroutine（候補更新・保有監視・PushFeed・News Ingest・トークン再発行）のpanic回復（FR-SCHED-6）。`Recover`（defer用）・`Run`（panic有無を返す）・`Try`（panicをerrorに変換）・`Loop`（待機→1サイクルを`Try`で保護し、panicもエラーもログに残して継続）を提供し、panicは`slog`にスタックトレース付きで記録する。`cmd/server`・`bootstrap`・`bootstrap/candidates`・`bootstrap/heldposition`・`service/marketdata`・`service/pushfeed`・`service/scheduler`（`updatecheck`含む）から使う | `internal/safego` |
| Bootstrap | desktop/server共通の起動処理（DB open+マイグレーション、`config/*.yaml`解決、secrets読込・routerオプション共通化、サービス組み立て・ジョブ登録。§10.1）。サブパッケージ: `candidates`（候補銘柄の定期更新）、`marketdatajob`（market-dataジョブ、互換用の空feature-calcジョブ）、`backtestsource`（Backtest用DB読み出し）、`heldposition`（FR-SCHED-4 保有ポジション5〜15秒Exit監視）、`rankingmeasure`（FR-SCHED-8 kabu `/ranking`計測ループ。`scan.ranking_measure`でオプトイン、計測値のみログ出力）、`paperexec`（Policy→Execution Paperアダプタ）、`alerts`（アラート宛先）、`startup`（desktop/server共通の`RunMain`・ログディレクトリ解決）、`universe`（銘柄マスタCSVのパースと`instruments`へのupsert）。分割方針は§3 | `internal/bootstrap`（`candidates`, `marketdatajob`, `backtestsource`, `heldposition`, `rankingmeasure`, `paperexec`, `alerts`, `startup`, `universe`） |
| Logging | slog JSON出力・日次ローテーション・30日超のgzipアーカイブ・エラーログのエクスポート（`requirements/non-functional.md` §5・§5.3、フローは`overview/flows.md` §10.6） | `internal/logging` |
| Supervisor | 子プロセスの異常終了時の指数バックオフ再起動（1秒〜5分、1分安定でリセット）。終了コード0で監視終了。`cmd/desktop`の`--supervise`起動でのみ使う（`requirements/non-functional.md` §3） | `internal/supervisor` |
| Single Instance Guard | DBと同じディレクトリのロックファイル（`app.lock`／`supervisor.lock`）による多重起動防止。`app.lock`は`cmd/desktop`と`cmd/server`で共有し（取得は`bootstrap.AcquireInstanceLock`）、2つ目の起動は`bootstrap.Run`に到達する前に終了する（desktopは終了コード0、serverは非0）。Scheduler・Kill Switch・発注の二重稼働と、先行インスタンスのrunningジョブの回収を防ぐ | `internal/singleinstance`, `internal/bootstrap` |
| Headless Server | Wailsに依存しない`net/http`エントリーポイント（Updater非配線。§9） | `cmd/server` |
| Setup Guard Middleware | 必須認証情報（JEV_API_KEY/KABU_API_PASSWORD）が未設定の間、`/setup`・`POST`/`DELETE /settings/:key`・`/static/...`以外の全リクエストを`/setup`へ誘導する（ページ遷移は302、HTMXは`HX-Redirect`、`/api/v1`は503 JSON、WebSocketは403。§10.5、FR-SETUP-1） | `internal/web/middleware` |
| API Error Formatter | `/api/v1` のエラーボディ整形。`registerAPI`から`apierror.Install()`で`huma.NewError`を上書きし、4xxはバリデーション詳細を`errors[]`に残し、5xxは固定メッセージのみ返して原因を`slog`へ記録する（`api/endpoints.md` §7、issue #215） | `internal/web/apierror` |
| Repository | SQLiteの永続化（テーブルを所有するリソース群`market`/`jobqueue`/`judgement`/`calibration`/`trading`/`system`、共有ヘルパー`sqlutil`・接続`sqlitedb`、複数リソースの読み取り`decisiontrade`・列対応表`snapshotcols`。§3） | `internal/repository`（サブパッケージ群） |
| Web (HTMX/Templ/Lit) | UI提供（`components/overview.md`）。画面/APIハンドラは責務別サブパッケージ: `symbol`（Symbol List/Detail/Close・`/ws/symbols/:symbol`）、`system`（System状態・Kill Switch・`/ws/system`・自動アップデートUI・エラーログのダウンロード`/api/v1/logs/errors`）、`settings`（`/settings`・`/setup`画面。Setup Guard Middlewareの誘導先）、`activity`（Activity Feed画面/API）、`shared`（共通ヘルパー） | `internal/web`（`handler/{shared,symbol,system,settings,activity}`） |

## 5〜13. 分割章

以降の章と改訂履歴は `.linterly.yml` の300行/ファイル制限のため別ファイルに分割している（節番号・内容は分割前と同一）。

| 節 | ファイル |
|----|----------|
| §5 kabuステーションAPI連携 / §6 Jev API連携 / §7 RAG連携 / §8 自己改善ループ / §9 Wails統合 / §12 System Activity Feed連携 / §13 Luna ニュース分類・News Ingest連携 | `docs/architecture/overview/integrations.md` |
| §10 通信フロー（§10.1〜§10.6）/ §11 障害対応方針 | `docs/architecture/overview/flows.md` |
| 改訂履歴（1.0〜） | `docs/architecture/overview/history.md` |

## 改訂履歴

改訂履歴は `.linterly.yml` の300行/ファイル制限のため `docs/architecture/overview/history.md` に分割している（版番号・内容は分割前と同一。`overview.md` と `overview/` 配下の全体の改訂を追記する）。
