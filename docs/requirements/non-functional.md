# 非機能要件

## 1. 前提

- 単一 Windows ホスト構成（kabuステーションAPIとGoバックエンド＝Wailsデスクトップアプリが同一機で常駐）
- 単一ユーザー（個人トレーダー）による利用を前提とし、マルチテナント・水平スケールは扱わない
- 個人の自己資金運用のみを前提とし、金融商品取引業登録を要する第三者資金運用は対象外（詳細は §6 コンプライアンス）

## 2. 性能

### 2.1 スキャンサイクル

| 対象 | 周期目標 | 上限 |
|------|---------|------|
| 全体スキャン（全ユニバース特徴量算出＋Fast Screener） | 60秒ごと | 次サイクル開始までに完了しない場合はスキップしログ記録。「完了しない」とは、サイクル開始時点で`market-data`キューに`pending`/`running`のジョブが1件でも残っている状態を指し、その場合は当該サイクルのジョブ投入を行わず`slog.Warn`（`scheduler: full scan skipped: previous cycle still running`、`pending`＝未完了件数）を記録する。未完了ジョブが0件になった次のサイクルから通常どおり投入を再開する（`jobs`への未処理ジョブの無制限な積み増しを防ぐ）。ワーカーが完了書き込みに失敗して`running`のまま残った孤児行で永久にスキップしないよう、判定の直前に`started_at`が10分より古い`market-data`の`running`行を`failed`（`last_error`＝`orphaned: still running after 10m0s`）へ回復して判定から除外する。しきい値内の`running`行は従来どおり未完了として数える。この孤児回復は全キュー（`market-data`/`feature-calc`/`jev-scout`/`jev-trader`/`outcome-labeling`/`analytics`）に共通で、Schedulerが1分ごとに全キューへ実行する（`internal/service/scheduler/orphans`）ため、`jev-scout`の孤児行が候補更新の間引き（`scoutHeld`）で該当銘柄を再起動まで保留し続けること（`functional/components-pipeline.md` §4.3）や、Activity Logの`running`件数が実態と乖離し続けることも防ぐ。回復した件数はキュー名付きで`slog.Warn`（`orphans: failed orphaned running jobs`、`queue`・`count`・`threshold`）に記録する。しきい値は固定の10分で、既定の全体スキャン周期60秒の10倍に相当するが`scan.full_scan_interval_seconds`には連動しない（周期を変更しても10分のまま）。根拠: Jev呼び出しの最悪所要は約43秒（§2.2）、`market-data`は1銘柄の取得であり、いずれもこれを大きく上回る。想定より長く動いているだけのジョブを早期に`failed`としても、そのワーカーの完了書き込みが後から状態を上書きするため害はない |
| 候補銘柄（Jev Scout/Trader）再評価 | 15〜30秒ごと | Jev API合計呼び出しは1分あたり上位N銘柄（`top_n`。既定20、`config/strategy.yaml`）× 2（Scout+Trader）を上限とする。この上限は、候補更新サイクルからの`jev-scout`ジョブ投入を銘柄ごとに「`pending`/`running`ジョブがある間、および前回完了から`scan.jev_scout_min_interval_seconds`（既定60秒）未満の間はスキップ」することで担保する（Scoutは同一銘柄で1分あたり最大1回。FR-SCAN-1のイベント発火による即時再評価のみ例外。詳細は`functional/components-pipeline.md` §4.3）。Nを引き上げるほど呼び出しコストが比例して増える |
| 保有ポジション監視・Exit評価 | 5〜15秒ごと | Risk EngineのExit判定はJev応答を待たずコード側で即時評価する |

### 2.2 レイテンシ目標

| 区間 | 目標 |
|------|------|
| kabuステーションAPI取得 → Feature Engine算出 | 500ms以内 / 銘柄バッチ |
| Fast Screener（全銘柄→上位N） | 2秒以内 |
| Jev Scout 1回呼び出し | 3秒以内（タイムアウト5秒） |
| Jev Trader 1回呼び出し | 3秒以内（タイムアウト5秒） |
| Risk Engine判定 | 100ms以内（外部呼び出しなし） |
| Paper発注〜約定シミュレーション | 200ms以内 |
| UI（Scanner Dashboard）へのライブ反映 | WebSocket経由で1秒以内 |

- Jev API（Scout/Trader）の「タイムアウト5秒」は**HTTP 1試行あたり**の上限とする（`internal/service/jev` の `defaultHTTPTimeout`）。失敗時は最大4試行（初回＋リトライ3回。1回目リトライは即時、2回目以降は500ms・1秒の指数バックオフ）で、全試行失敗時の1呼び出しあたり最悪所要時間は 4×5秒＋1.5秒 = 21.5秒。候補再評価周期（15〜30秒、§2.1）の下限を超え得るが、Scout/TraderのJev呼び出しはJobキュー（`jev-scout`/`jev-trader`）経由の非同期処理で周期を塞がない（両呼び出しがともに全試行失敗した場合の合計は最悪43秒）。「最大4試行・1回目リトライは即時・500ms/1秒バックオフ」は実装（`internal/service/jev/client.go` の `defaultMaxAttempts`/`defaultRetryBaseDelay`）の値であり、`architecture/overview/integrations.md` §6 にも同内容を明記する 429（レート制限）/529（過負荷）は即時再試行せず1回目リトライからbackoff（500ms・1秒・2秒）するため最悪 4×5秒＋3.5秒 = 23.5秒。401/422と不正応答は再試行しない（`architecture/overview/integrations.md` §6）。

### 2.3 スループット・スケーラビリティ

- 対象ユニバースは東証上場銘柄（最大 約4,000銘柄。運用者が銘柄マスタCSVで供給する。`environment/setup.md`「銘柄マスタの投入」）を想定し、Fast Screener段階まで全銘柄を60秒サイクルで処理できること（現行実装が保証する範囲は下記）。フルスキャンの対象は有効な全銘柄で、市場コンテキスト算出用の`market_index`/`sector_index`行も同じ`market-data`ジョブで処理するため、以下の銘柄数・銘柄あたりコストの試算にはこれらの指数行も含まれる（`functional/components-platform.md` FR-SCHED-2）
  - **保証できる範囲（issue #391 / #514）**: `market-data`キューのワーカーは1本で、ジョブを1銘柄ずつ直列処理する（`ClaimNext`→板取得→特徴量算出・スナップショット保存→`MarkSucceeded`。SQLiteは単一ライターで、各コミットがfsyncを伴う）。板取得が即時の場合のDBコストは`BenchmarkFullScanCycle`（`internal/bootstrap/marketdatajob`。フェイク板・SQLite実DB、Ryzen 9 5950X・Linux、他プロセスと共有のSSD。`go test ./internal/bootstrap/marketdatajob -run '^$' -bench FullScanCycle -benchtime 1x`）で計測した: 500銘柄≒19.5ms/銘柄（約9.7秒）、1,000銘柄≒18.8ms/銘柄（約18.8秒）、4,000銘柄≒27.8ms/銘柄（約111秒。同一ホストの負荷により100〜240秒の範囲でばらつく）。市場コンテキスト（指数リターン・市場ブレッドス）は全銘柄共通のため`featureengine/marketcontext.Loader`が30秒キャッシュして全銘柄のジョブで共有する（issue #622。以前は1ジョブごとに指数履歴と全アクティブ銘柄の`return_5m`を読み直しており、4,000銘柄でブレッドス用クエリだけで1回約42msかかっていた）。共有後の同一コマンドの再計測（他エージェントのビルド・テストと並走した負荷の高いホストで、1回ずつ）は、キャッシュ無し→有りで500銘柄≒23.0→35.0ms/銘柄、1,000銘柄≒41.5→33.7ms/銘柄、4,000銘柄≒56.7→39.5ms/銘柄（約227秒→約158秒）で、ホスト負荷のばらつきが大きく銘柄数の少ない条件では差が逆転したが、キャッシュ無しの試算が負荷の影響を受けやすい4,000銘柄ではジョブあたり約17ms短縮した。**4,000銘柄の全体スキャンを60秒以内に完了することは、板取得が即時でも現行の単一ワーカー設計では保証できない**。加えて本番のkabuステーションはローカルRESTで約1.5ms応答するため、レート制御なしでは情報APIの公式上限（10件/秒）を超えて`429`/`4001006`が一斉発生する（issue #514）。**REST板の実効上限は情報APIのプロセス全体レート制限で決まる**
  - **kabu情報APIの流量（issue #514）**: 公式FAQは「発注APIは5件/秒、取引余力APIや情報API、銘柄登録APIは10件/秒」。本システムは発注APIを呼ばない。`GetBoard`（板）・`GetSymbol`（銘柄情報）・`RegisterSymbols`（銘柄登録）はプロセス全体で1つのスライディング1秒窓を共有し、既定は`scan.kabu_info_api_max_per_second`（同梱8。0以下は8に補完、10超は公式上限10に切り下げ）。トークン発行（`/token`）はこの上限の対象外。PUSHで直近30秒以内の板がある銘柄はRESTを消費しない（最大50銘柄、`architecture/overview/integrations.md` §5）。保有ポジション監視の`GetBoard`と銘柄情報キャッシュ未ヒットの`GetSymbol`も同じ窓を消費する
  - **1サイクルで板を取れる銘柄数と残り**: 既定8件/秒×60秒＝最大480回の情報API枠。うちPUSH未着の板・保有監視・銘柄登録・銘柄情報が枠を分け合うため、RESTで新規に板を取れるのはおおよそ400〜480銘柄/60秒。PUSH対象50銘柄を足しても、約4,000銘柄のユニバースを60秒で全件更新することはできない。フルスキャンは有効な全銘柄の`market-data`ジョブを1トランザクションで投入し（空ハンドラの`feature-calc`は投入しない）、レート制限に従って消化する。60秒以内に終わらない分は同じサイクルの続きとして処理し、次の60秒tickは§2.1のとおり投入をスキップする（`scheduler: full scan skipped: previous cycle still running`）。周期は処理時間まで間延びするが、未処理ジョブは積み増されない。REST約3,950銘柄なら既定8件/秒で約8分で一周し、その後に次の全件投入が走る。候補・保有の優先順位付けやREST対象の間引きは行わない（PUSH登録も`symbol`昇順の先頭50件のまま）
  - **429 / 4001006**: 流量超過として扱う。クライアントは1秒待って最大3回再試行し、尽きたら`ErrRateLimited`を返す（`market_data_down`の連続失敗にもstaleにも数えない）。`market-data`ジョブは失敗にせず当該銘柄のスナップショットを作らず次サイクルへ回す（`slog.Warn` `marketdatajob: defer board fetch to next cycle`）。リミッタ飽和時は`slog.Warn`（`marketdata: kabu info api rate limiter saturated`、最大1回/秒）を出す。ワーカーの並列化は実施しない（SQLite単一ライターと公式10件/秒の両方に律速されるため）
- Scanner Dashboardのスキャン状況パネル用に保持する銘柄別の判定結果（FR-SCAN-3〜6）は最新1サイクル分のメモリ上のみ（約4,000銘柄×数十バイト）で、サイクル周期・Fast Screener 2秒以内の目標に影響させない。4,000銘柄での計測（issue #303、`go test -bench`・Ryzen 9 5950X・SQLite実DB）: Fast Screener段階（`screener.Run`→`Screen`）は約0.28ms→約0.33ms/サイクル（追加確保は約8KB=銘柄あたり2バイトの理由ビットマスクのみ）、候補更新サイクル全体（DB読込・特徴量入力組立・保持込み）は確保量が約45.0MB→約45.4MB（+約1%、確保回数は+10回）で所要時間は計測ばらつきの範囲内（約1.1〜1.5秒、大半は既存のSQLite読込）。どちらも60秒周期・Fast Screener 2秒以内の目標に影響しない
- Jev API呼び出しは全銘柄ではなくFast Screener通過銘柄の上位N（`top_n`。既定20。50〜200は引き上げ時の目安）→ Scout通過銘柄（Nの一部）に限定し、API呼び出しコストを抑制する（§4.3 再評価抑制も適用）
- 単一Windowsホスト・単一プロセス（Wails）構成のため、将来的にスキャン対象拡大や複数戦略同時運用が必要になった場合は、Scheduler/Worker層をプロセス分離・別ホスト化できるよう、内部レイヤー（domain/repository/service）をWailsプロセスに直接依存させない設計とする（詳細は`architecture/overview.md`）

## 3. 可用性

- 目標: 24/365常時稼働（夜間・週末を含む）。ただし発注・新規エントリーは東証立会時間（9:00-11:30 / 12:30-15:30 JST）のみ行う
- 立会時間外は市場データ取得・Jev呼び出し・新規発注を停止し、Scheduler/Workerは待機状態に入る（無駄なAPI課金を避ける）
  - 引け前強制決済ウィンドウ（大引け − `force_flat_before_market_close_minutes` 以降）でも新規発注は停止する（建てた直後に強制決済されるため。`execution.ErrOutsideTradingSession`）
- 立会時間の判定は `internal/service/marketcalendar`（東証カレンダー）が行う: 前場 9:00-11:30・後場 12:30-15:30 JST（開始時刻を含み終了時刻を含まない。11:30-12:30の昼休みは立会外）。土日・国民の祝日（振替休日・国民の休日を含む。2020年以降の祝日法に基づく判定）・年末年始休場（12/31, 1/1-1/3）は終日立会外とする。臨時休場（システム障害等）は扱わない
- 立会時間外に停止する対象: フルスキャン・イベント再評価のenqueue、候補更新に伴うJev Scoutのenqueue、保有ポジション監視の板取得、新規エントリー（`execution.ErrOutsideTradingSession`）、市場データ停止/Jev API停止の検知、操作者ハートビート判定。バックアップ・データ削除・ログローテーション等のメンテナンスジョブとOutcome Labelingは保存済みデータのみを扱うため立会時間外も実行する
  - 停止中であることはScanner Dashboardのスキャン状況パネルに通知する（`functional.md` FR-SCAN-7）。候補リストは保存済みデータで更新され続けるため、通知は「市場データ取得・フルスキャン・Jev Scoutは停止中、表示は保存済みデータに基づく」と案内し、次回の立会開始時刻（`marketcalendar.Calendar.NextOpen`）を併記する
- Windowsへのログイン時にWailsアプリを自動起動し、クラッシュ時は自動再起動する。実装: NSISインストーラー（`cmd/desktop/build/windows/installer/project.nsi`）のコンポーネントページ「Start at login and restart after a crash」（既定でON）が、スタートアップフォルダに`--supervise`付きのショートカットを作成する（アンインストールで削除。無人自動更新`/S`では初回インストール時の選択を維持する）。`--supervise`で起動した`pitha-trador.exe`は自身を子プロセスとして再実行して監視し（`internal/supervisor`）、終了コードが非0またはkillされた場合に指数バックオフ（1秒〜最大5分、1分以上安定稼働したらリセット）で再起動する。終了コード0（操作者による終了・自動更新による終了・起動失敗ダイアログ後の終了）では監視を終了し、意図した停止と競合しない。再起動中はプロセスごと停止しているためSchedulerによる新規エントリーは行われず、再起動後は既存ポジションのExitルールが再開する。Wailsアプリはデスクトップセッションを要するため、起動契機は「Windowsホスト起動」ではなく「ユーザーログイン」であり、ホスト再起動後の無人復帰にはWindowsの自動ログオン設定を前提とする。自動起動とデスクトップアイコン起動の併存による二重稼働（Scheduler・Kill Switch・発注の重複）を防ぐため、アプリ本体・`--supervise`監視プロセスはそれぞれDBと同じディレクトリのロックファイル（`app.lock`／`supervisor.lock`。`internal/singleinstance`）を`bootstrap.Run`より前に取得し、取得できない2つ目の起動は終了コード0で即終了する（OSがプロセス終了時にロックを解放するため、クラッシュ後の残留ロック解除は不要）
- kabuステーションアプリ本体はサードパーティ製でログイン操作を要するため、pitha-trador側からは起動・再起動しない。自動起動はkabuステーション自身の設定（またはオペレーターによるWindowsタスクスケジューラ登録）で行う。kabuステーションAPIの停止・異常はKill Switch発動条件（`architecture/overview/flows.md` §11）として扱う
- SQLiteのDBファイル（アプリ内蔵）を日次でバックアップし、ローカルディスク外（外部ストレージ/クラウド）へ退避する。保持期間は直近90日分のフルバックアップ＋週次アーカイブ。バックアップ時はWALチェックポイント（`PRAGMA wal_checkpoint(TRUNCATE)`）を実行してから複製する
  - 実装: Schedulerの日次ジョブ（`internal/service/backup`）。退避先は環境変数`PITHA_BACKUP_DIR`（`environment/setup.md`）で指定し、未設定時は無効（起動ログに警告）。退避先ディレクトリ自体は作成せず、存在しない場合（外付けドライブ/クラウド同期フォルダ未マウント等）はローカルディスクへ静かに退避せずエラーにする。書き込み中の生ファイルコピーは不整合になり得るため、複製はチェックポイント後の`VACUUM INTO`（単一スナップショット）で行い、確定前に`secrets`テーブル（アプリ埋め込み鍵で暗号化されたAPIキー/パスワード）を空にして`PRAGMA integrity_check`で検証する。復元後はSetup画面で秘密情報を再入力する。ディレクトリは`0700`、ファイルは`0600`で作成する。日次分は`daily/pitha-YYYY-MM-DD.db`に保存し90日超を削除、各ISO週の最初のバックアップ（起動が平日のみでも週1つ生成される。週の月曜日付）を`weekly/pitha-YYYY-MM-DD.db.gz`（gzip）として52週保持し超過分を削除する
  - 実行タイミング: デスクトップアプリは日中のみ起動する運用のため、深夜0時固定のcronは実行されない。バックアップ・データ保持パージ・ログアーカイブは、最終成功日を`runtime_settings`（`system.maintenance.<task>.last_success_date`）に保存し、起動直後および10分ごとに「本日未成功なら実行」（catch-up）する。バックアップは加えて毎日16:00（ローカル時刻）にも実行する。失敗時は30分後に再試行し、3回連続で失敗した時点でSlack（未設定時は構造化ログ）へ通知する
- 高頻度書き込みテーブルはDBの無制限な肥大を防ぐため保持期間を設け、Schedulerの日次ジョブ（上記catch-up方式）で期限切れ行を削除する（`internal/service/retention`）。`jobs`は完了行のみ対象で`succeeded`は7日・`failed`は30日（`pending`/`running`は削除しない）、`market_snapshots`は90日（対応する`market_snapshot_vectors`行も同時に削除）。削除は1000行単位のバッチで行い、ワーカーの書き込みを長時間ブロックしない。監査対象テーブル（`kill_switch_events`/`kill_switch_resolutions`/`jev_decisions`/`paper_orders`等）は削除対象外。削除済みページは以後の書き込みで再利用されるためファイルは増え続けないが縮小はしない（バックアップの`VACUUM INTO`は縮小済みで出力される）

## 4. セキュリティ

- Jev APIキーはGoバックエンドプロセス内のみで保持し、フロントエンド（Templ/HTMX/Lit）に露出させない
- kabuステーションAPIのAPIパスワード（`KABU_API_PASSWORD`）はSQLiteの`secrets`テーブルにAES-256-GCMで暗号化して保存する（鍵はアプリ埋め込みシード由来で、保護対象はDBファイル単体の複製・共有時の平文流出。OS資格情報ストアは使用しない。`architecture/er/tables-system.md` §secrets）。kabuステーションAPIの発行トークンはメモリ上にのみ保持し、ディスク（設定ストア・`secrets`テーブル含む）へは保存しない（`architecture/overview/integrations.md`）
- Broker（kabuステーション）APIキー/パスワードはProduction用とPaper Trading用を分離して管理する。Phase 7（実売買）までは、`KABU_API_PASSWORD`（単一キー）はkabuステーションAPIのトークン発行・銘柄登録・板情報取得（市場データ読み取り）にのみ使用し、注文エンドポイント（発注・取消）は呼び出さないため発注可能な認証情報は保持しない。Phase 7で注文エンドポイントを呼び出す前に、Production用とPaper Trading用のキー・接続先（Base URL）を別々のsecretsキーとして追加し、Paper Tradingが実口座の認証情報を参照できない構成にする（現時点では未実装）
- Secrets（APIキー・パスワード・DB接続情報）はGitに保存しない。`.env`はコミット対象外とし、`.env.example`のみをリポジトリに含める
- Wailsアプリのページ・APIはWails AssetServer（プロセス内、ネットワークポートなし）経由でのみ配信する。Windows（WebView2）のみ、AssetServerがWebSocketを扱えないため`/ws/...`のUpgradeだけを受ける専用のループバックリスナー（`127.0.0.1`と`[::1]`の同一ランダムポート。`[::1]`が使えない環境は`127.0.0.1`のみ。ページ・APIは配信しない）を追加で起動する。いずれもループバックにのみバインドし、外部ネットワークからアクセス不可とする（`api/endpoints.md` §6、`cmd/desktop/ws_listener.go`）
- HTMXフォームにはCSRFトークンを付与する（HALT標準構成に準拠。ローカル単一ユーザーでも実装は省略しない）
- 実売買（Phase 7）へ移行しても、発注確定・Kill Switch操作に人手の追加認証は要求しない（完全自動運用）。安全性はRisk Engine側のLive専用の厳格なリミット（`requirements/functional.md` §4.7）と、操作者ハートビートが一定時間途絶した場合に自動で新規エントリーを停止するdead-man's switch（FR-RISK-6）で担保する
- Risk Engineの拒否・Kill Switch発動・実売買発注はすべて監査ログ（追記専用ログ）に記録する。`kill_switch_events` / `kill_switch_resolutions`はDBトリガーで`UPDATE`/`DELETE`を拒否して追記専用を強制する（`architecture/er.md` §kill_switch_events）。ハッシュチェーン等によるDBファイル自体の改ざん検知は行わない

## 5. 監視・アラート

MVPでは構築コストを抑え、構造化ログ＋Slack Webhook通知のみで運用する（将来Prometheus等への拡張を妨げない形でログ出力を設計する）。

### 5.1 必須ログ（構造化JSON、レベル別）

- Market data fetch latency / エラー率
- Jev API latency / エラー率
- スキャン対象銘柄数、Scout呼び出し回数、Trader呼び出し回数
- Signal count（生成シグナル数。`policy: trade signal decided`ログ。永続化する`Evaluate`のみが出力し、バックテスト再現の`Decide`は出力しない）
- Risk拒否件数
- kabuステーションAPI（Broker）latency / エラー
- DB latency / エラー（エラーはERROR、100ms以上の低速クエリはWARN、それ未満の正常クエリはDEBUG。ワーカーのアイドルポーリングでログが肥大化しないよう、正常クエリはINFOで記録しない）

### 5.2 即時Slack通知対象

- 市場データ停止（一定時間データ更新なし）
- Jev APIエラー率上昇（しきい値超過）
- kabuステーションAPI異常
- Kill Switch発動（自動再開可否・発動理由を含む）
- Kill Switch自動再開・手動再開待ち（Live専用、`functional.md` FR-RISK-7）
- 操作者ハートビートタイムアウトによる新規エントリー自動停止（Live専用、FR-RISK-6）
- 想定外ポジション検知
- 日次損失上限接近（例: 上限の80%到達）

- ログは日次ローテーションし、直近30日分をローカル保持、それ以前は圧縮アーカイブする

### 5.3 エラーログのエクスポート

- §5.1の構造化ログのうちエラー（`level=ERROR`、任意で`WARN`以上）を、運用者がUIからダウンロードして調査・共有できる（`requirements/functional/components-platform.md` §4.19 FR-ERRLOG-1〜7）。ログの日次ローテーション・30日超のアーカイブ・保持は変更しない
- エクスポートは共有を前提に、認証情報（APIキー・パスワード・トークン・Slack Webhook URL等）を出力時にマスクする。ログ出力側で秘密情報を出さない方針は維持し、マスクは多層防御とする
- 出力は最大10MiB、読み取り専用で、ローカルループバック上のセッション認証必須（§4）

## 6. コンプライアンス・運用前提

- 本システムは個人の自己資金運用を前提とし、第三者資金の運用・金融商品取引業登録を要する業務は対象としない
- AIモデル（Jev）の出力を発注権限そのものとして扱わず、必ずコードベースのRisk Engineを経由する
- Jevのconfidence/probabilityをそのまま実勝率として扱わない（Calibrationで独自検証する）
- 実資金投入前に十分なバックテスト・Paper Trading・スリッページ/手数料/流動性を含む検証を行う
- 免責: 短期売買は損失リスクが高く、本仕様はソフトウェア設計・研究用途を想定する

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-09-26 | 新規作成 | 初版 |
| 1.1 | 2026-09-26 | §4セキュリティのPhase 7追加認証要件を撤廃し、dead-man's switch/Live専用厳格リミットに置換。§5.2にハートビート/再開通知を追加 | Phase 7も含めた完全自動運用への方針変更 |
| 1.2 | 2026-09-29 | §4のBroker認証情報の分離について、Phase 7までは単一の`KABU_API_PASSWORD`を市場データ読み取り専用とし、Production/Paper別キーへの分離はPhase 7で注文エンドポイント実装前に行うと明記 | issue #103対応（仕様と実装の乖離解消） |
| 1.3 | 2026-09-29 | §3にDB日次バックアップの実装方式（Schedulerジョブ・`PITHA_BACKUP_DIR`・`VACUUM INTO`・日次/週次の保持）を追記 | issue #97対応（仕様と実装の乖離解消） |
| 1.4 | 2026-09-29 | §3に高頻度書き込みテーブル（`jobs`/`market_snapshots`）の保持期間と日次パージ、監査テーブルの削除対象外を追記 | issue #129対応（DB無制限増大の解消） |
| 1.5 | 2026-09-29 | §3のバックアップ/パージ/ログアーカイブを起動時catch-up方式に変更、バックアップからの`secrets`除外・`0700`/`0600`・週次52週保持・退避先必須・連続失敗通知を追記 | issue #137/#152/#159対応 |
| 1.6 | 2026-09-29 | §3の自動起動・クラッシュ時再起動の実装方式（スタートアップショートカット＋`--supervise`セルフ監視）とkabuステーションアプリの対象外を明記 | issue #204対応（仕様と実装の乖離解消） |
| 1.7 | 2026-10-01 | Jev APIの再試行を公式API（`/v1/systemone`）に合わせ、429/529は1回目リトライからbackoff・401/422は即失敗と追記 | issue #263 |
| 1.8 | 2026-10-01 | §5.3 エラーログのエクスポートを追加（UIからのダウンロード、秘密情報マスク、出力上限10MiB） | issue #267 |
| 1.9 | 2026-10-02 | §4のWailsアプリのバインド記述を、Windowsのみ`/ws/...`専用ループバックリスナー（`127.0.0.1`と`[::1]`）を起動する実態に合わせて修正（「`127.0.0.1`にのみバインド」を是正） | issue #300（#266/#285の実装との乖離解消） |
| 1.10 | 2026-10-03 | §2.3 にスキャン状況パネル用の結果保持コストと計測値を追記 | issue #303 |
| 1.11 | 2026-10-03 | §4のkabuステーションAPI認証情報の保存方式を実装に合わせて修正（APIパスワードは`secrets`テーブルにAES-256-GCM暗号化、発行トークンはメモリのみ保持で非永続、OS資格情報ストア不使用） | issue #306 |
| 1.12 | 2026-10-03 | §2.1/§2.3 の候補数表現を「上位N銘柄（`top_n`、既定20）」へ統一し、「50〜200」「10〜30」の固定値を削除 | issue #327 |
| 1.13 | 2026-10-03 | §3 に立会時間外のスキャン停止通知（FR-SCAN-7）と`NextOpen`を追記 | issue #367 |
| 1.14 | 2026-10-04 | §2.3 の対象ユニバースの供給元（銘柄マスタCSV）を明記 | issue #389 |
| 1.15 | 2026-10-04 | §2.1 のJev呼び出し上限（N×2/分）を担保する機構（銘柄別`scan.jev_scout_min_interval_seconds`と未完了ジョブの重複排除）を明記 | issue #388 |
| 1.16 | 2026-10-05 | §2.3 の対象ユニバースにフルスキャン対象の指数行（`market_index`/`sector_index`）が含まれる旨を追記 | issue #422 |
| 1.17 | 2026-10-05 | §2.1 の全体スキャン未完了判定に、10分超`running`の孤児ジョブを`failed`へ回復して判定から除外する規則を追記 | issue #416（#390修正の回帰） |
| 1.18 | 2026-10-05 | §2.1 の全体スキャンの前サイクル未完了時のスキップと`slog.Warn`を追記 | issue #390 |
| 1.19 | 2026-10-05 | §2.3 にフルスキャンの保証範囲（約2,000銘柄目安）とREST並列度1の根拠を追記 | issue #391 |
| 1.20 | 2026-10-05 | §2.1 の孤児`running`回復を全キュー共通（Schedulerが1分ごとに実行、`jev-scout`の`scoutHeld`保留も解消）へ拡張し、しきい値が固定10分で`scan.full_scan_interval_seconds`に連動しないこと・根拠・キュー名付きWarnログを明記 | issue #424/#425/#428 |
| 1.21 | 2026-10-05 | §2.3 の「RESTにレート制御を設けない」を撤回し、kabu情報APIのプロセス全体上限（既定8件/秒、公式10件/秒）、1サイクル約 REST 400〜480＋PUSH 50、残りは同一サイクル継続＋次tickスキップ、429/4001006はジョブ失敗にしないことを明記 | issue #514 |
