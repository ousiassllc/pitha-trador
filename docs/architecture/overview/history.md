# アーキテクチャ設計: 改訂履歴

`docs/architecture/overview.md` から分割した改訂履歴。`overview.md` と `overview/` 配下（`integrations.md`・`flows.md`）の全体の改訂を、本ファイルへ1行ずつ追記する（`.linterly.yml` の300行/ファイル制限のため分割。版番号・内容は分割前と同一）。

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
| 1.34 | 2026-10-01 | §5・§6: `JEV_BASE_URL`を任意の上書きにして既定値`https://api.typesafe.ai`を導入し、Setup Guardの必須キーを`JEV_API_KEY`・`KABU_API_PASSWORD`の2つに変更（保存済みの値は既定値より優先、移行処理なし）。Settings画面の入力項目を通常入力と折りたたみの「詳細設定（任意）」に再構成し、Jevモデル名の任意上書き`JEV_MODEL`（既定`jev-latest`）を追加。§4 Setup Guard Middleware行を更新 | issue #271・#272・#274 |
| 1.35 | 2026-10-02 | §8（`overview/integrations.md`）: 自動アップデートの更新確認用GitHubトークン（`UPDATE_GITHUB_TOKEN`、APIアセット取得、`ErrorAuth`）を削除し、`browser_download_url`の直接取得へ戻した。401/403/404は`ErrorAccess`（再試行なし）、`Retry-After`付き403・429は`ErrorRateLimit`（一時エラー）の分類は維持 | 更新確認用トークン機能の廃止（リポジトリのpublic化） |
| 1.36 | 2026-10-02 | §8（`overview/integrations.md`）: 最新リリース取得の404（リリース未公開／アクセス不可）を`ErrorAccess`のエラーから`Status.NoRelease`（成功扱い・Infoログのみ・再試行なし）へ変更し、Settings画面に「公開されているリリースが見つかりませんでした」を表示。リリース取得の401/403とアセット取得の401/403/404は`ErrorAccess`のまま | issue #296（リリース未公開期間にERRORログとバックオフ再試行が連打されていた） |
| 1.37 | 2026-10-02 | §6（`integrations.md`）: kabuステーションAPIトークン発行失敗時の継続起動・バックグラウンド再試行・エラーコード別の原因表示（市況データ接続バナー）を追記 | issue #295 |
| 1.38 | 2026-10-03 | §10 通信フロー（`overview/flows.md`）: `/setup`がSettingsと同じ接続先一覧・モーダル（`ConnectionList`）を使うことを追記 | issue #302 |
| 1.39 | 2026-10-03 | §3のツリーに、`internal/router`直下が行数上限（2000行/ディレクトリ）を超過したため外部テストを移したテスト専用サブパッケージ`router/{apiroutes,staticroute,systemheader}`を追記 | issue #314 |
| 1.40 | 2026-10-03 | §3の`.linterlyignore`方針に、ライセンス全文`LICENSE`（手書きソースではない定型文）を許容する除外として追記 | issue #316 |
| 1.41 | 2026-10-03 | §5・§6（`overview/integrations.md`）: Settings画面の`JEV_BASE_URL`/`JEV_MODEL`の入力先を、廃止済みの折りたたみ「詳細設定（任意）」からJev接続先モーダル内の任意項目へ訂正（1.34の「詳細設定」は当時の記録）。「上書き値があるときだけ詳細設定を開く」記述を削除 | issue #315（#302 とのdoc-drift解消） |
| 1.42 | 2026-10-03 | §2の「DBアクセス」行を、sqlc前提から実装どおりの`database/sql`＋手書きSQL（`internal/repository/**`）へ訂正 | issue #317（リポジトリにsqlcの設定・生成コード・依存が存在しない） |
| 1.43 | 2026-10-03 | §3のツリー・テスト専用ディレクトリ一覧・§4のScheduler/Worker行に、実在する`router/ws_listener.go`・`router/wslistener`（テスト専用）・`scheduler/updatecheck`を追記 | issue #319 |
| 1.44 | 2026-10-03 | §3のツリー・レイヤー依存ルールに基盤パッケージ`internal/httpbody`（外部API応答ボディの4 MiB上限付き読み取り）を追記。`overview/integrations.md` §5・§6にkabuステーション・Jevの応答ボディ上限と超過時（`httpbody.ErrTooLarge`）の扱いを追記し、Slack Webhookのエラー応答は`httpbody`ではなく`service/notify`内で先頭4 KiBに切り詰めることを明記 | issue #333（#313・#324） |
| 1.45 | 2026-10-03 | 改訂履歴を`overview/history.md`へ分割し、`overview.md`には分割章表と参照のみを残した（版番号・内容は分割前と同一）。`overview.md`が300行/ファイル上限を超過したため | issue #335 |
| 1.46 | 2026-10-03 | `overview/integrations.md` §5「異常時」・`overview/flows.md` §11 Market Data欠損行を実装の現状へ訂正（`StatusTracker`は記録のみで`IsStale`の呼び出し元なし。銘柄単位の新規取引禁止は未実装で、有効なのは全体の`market_data_down` Kill Switchのみ） | issue #337（仕様は実装を要求していたが、鮮度閾値が仕様に無く取引経路のリスクゲート変更になるため仕様側を現状に合わせた） |
| 1.47 | 2026-10-03 | §4 Backtest Engine行と`overview/integrations.md` §8に、バックテスト/シャドーバックテストの再現範囲（Exitは固定SL/TP/最大保有時間のみ・Risk Engine不適用・スリッページ5bps/手数料0bps固定）を追記（`requirements/functional/components-platform.md` FR-BT-4新設、FR-SELFIMPROVE-4から参照）。`internal/service/backtest/runner.go`の古いRisk Engineコメントも訂正 | issue #344 |
| 1.48 | 2026-10-03 | §3のツリー・§4のExecution行にテスト専用サブパッケージ`execution/pendingfill`（PENDING指値Entry関連の外部テスト）を追記。`internal/service/execution`が2000行/ディレクトリ上限を超過したため | issue #348 |
| 1.49 | 2026-10-03 | `overview/integrations.md` §5のトークン失敗`4001007`/`4001017`（未ログイン）の案内をログイン状態の確認と再ログインに限定（`requirements/functional/components-platform.md` FR-SETTINGS-5も同様）。`overview/flows.md` §10.4のハートビート更新対象外に、`kill_switch`/`state_changed` push受信時の`pitha-kill-switch-panel`の再同期を追記 | issue #305, #363 |
| 1.50 | 2026-10-04 | `overview/integrations.md` §5に銘柄マスタ（`instruments`）の起動時CSV投入（`internal/bootstrap/universe`）を追記し、`overview/flows.md` §10.1の起動時フローに反映 | issue #389 |
| 1.51 | 2026-10-04 | §3のツリー・`web/handler`行を更新し、直下に残っていた`scanner`/`performance`/`calibration`/`proposals`/`swagger`を責務別サブパッケージへ移動（直下は`doc.go`のみ）。`internal/web/handler`が2000行/ディレクトリ上限を超過したため | issue #370 |
| 1.52 | 2026-10-05 | §3のツリー・責務規約表のbootstrap行・§4のBootstrap行に`bootstrap/universe`サブパッケージと直下の`universe.go`・`router_options.go`、`router_options.go`由来の依存先（`internal/router`・`web/handler`）を追記。§3のmiddleware列挙に`SecurityHeaders`を追記。`overview/flows.md` §10.1の起動時フローを`Services.Start`の実行順（ジョブ復帰→CSV同期→トークン発行→周期ジョブ登録→PUSH購読）に並べ替え | issue #396, #405, #411, #412, #413 |
| 1.53 | 2026-10-05 | `overview/flows.md`のフルスキャン記述を、`feature-calc`はフルスキャンが投入せず旧ジョブを消化する互換ハンドラとして残る実装に合わせて訂正（issue #417）。`overview/integrations.md` §5のPUSH購読に、登録対象が`symbol`昇順の先頭50件であること・REST `GetBoard`が`market-data`ワーカー1本からの直列呼び出し（並列度1）であることを追記（issue #391）。同§のActivity集計の「直近failed件数」の窓（過去1時間固定）を明記（issue #427） | issue #391, #417, #427 |
| 1.54 | 2026-10-05 | §3のツリー・§4のScheduler/Worker行に`scheduler/orphans`サブパッケージ（全キュー共通の孤児`running`ジョブの`failed`回復。固定10分、Schedulerが1分ごとに実行）を追記。`internal/service/scheduler/doc.go`の周期トリガー列挙に毎分の`orphans.FailAll`を追記 | issue #424, #425, #429 |
| 1.55 | 2026-10-05 | §3の`static/src`ツリーに`csp/`（`lightweight-charts-style-hash.test.ts`）を追記し、実体（`components`/`css`/`csp`/`img`/`vendor`/`embed.go`/`dist`）と一致させた | issue #432 |
| 1.56 | 2026-10-05 | `architecture/er.md` §型・規約「日時」の小数秒固定幅化（マイグレーション000021）に伴う記述変更。本ファイルが参照する`overview.md`・`overview/`配下の本文は変更なし | issue #430 |
| 1.57 | 2026-10-05 | `components/overview.md` §2の`static/src/components`ツリーに`scanner-table/`・`activity-feed/`の補助ファイル・`scanner-contract.json`・`dist/js/chunks/`を追記（`components/overview.md`の改訂履歴1.52）。本ファイルが参照する`overview.md`・`overview/`配下の本文は変更なし | issue #436 |
| 1.58 | 2026-10-05 | §7（`integrations.md`）に実装済みのRAG文脈を追記: `calibration_outcomes`の結合（最短horizonの`future_return`・`was_direction_correct`・`regime`）、outcome紐付き済み判断を優先する再ランク（k×4候補）、`stateGuide`改訂に伴う`question_version`の`scout-v3`/`trader-v3` | issue #437 |
| 1.59 | 2026-10-05 | §6（`integrations.md`）の現行`question_version`を`scout-v3`/`trader-v3`に更新し（v2はRAG文脈に実結果が無いと案内していた旧プロンプト）、同節の「Calibrationのコホートを分離するため」を実態に訂正（版は記録・表示用で、Calibration集計は版で分離せず全版を混在して集計する） | issue #439, #440, #443, #444, #447, #448 |
| 1.60 | 2026-10-05 | §7（`integrations.md`）のRAG候補取得を、`calibration_outcomes`紐付き済みのみを対象にした近傍検索（最大k件）＋不足時のk×4件の距離順プールへ変更し、判断本体・outcomeの取得を固定クエリ数の一括取得へ変更。§3/§4（`overview.md`）の`enrich`の利用者に`rag`（類似判断の`regime`復元）・`bootstrap/backtestsource`（バックテスト入力）を追記 | issue #441, #442, #445, #446 |
| 1.61 | 2026-10-05 | §8（`integrations.md`）に自己改善のロールバック判定の境界を追記: 適用前が負/ゼロでも適用前の絶対値を基準にした相対20%悪化で判定し、改善・同値では非ロールバック。いずれかの窓にクローズ済みポジションが無ければ判定不能として非ロールバック（`status=applied`のまま日次で再評価） | issue #449 |
