# 非機能要件

## 1. 前提

- 単一 Windows ホスト構成（Goバックエンド＝Wailsデスクトップアプリが常駐。市場データは選択したブローカーから取得する）。kabu選択時はkabuステーションAPIが同一機で常駐する。立花証券選択時は常駐アプリは要らず、インターネット直結（IPv4のみ）でe支店・APIへ接続し、PC時計のNTP同期が必須（`p_sd_date`のずれは30秒まで。ずれて`p_errno=8`で拒否されると接続バナーにNTP同期の案内を出す。`architecture/overview/integrations.md` §5.3）
- ブローカーは1プロセスで1つを選ぶ（`broker.provider`、既定kabu。切替は手動＝Settings＋再起動。自動フェイルオーバーと2社同時接続は対象外。kabuはフォールバックとして実装と設定を残す。同 §5.1）
- 単一ユーザー（個人トレーダー）による利用を前提とし、マルチテナント・水平スケールは扱わない
- 個人の自己資金運用のみを前提とし、金融商品取引業登録を要する第三者資金運用は対象外（詳細は §6 コンプライアンス）

## 2. 性能

板・PUSH・kabu情報API（`/board`・`/symbol`・`/ranking`・429/`4001006`）に触れる以下の目標値・流量・既知制限は**kabu選択時**のものである（既定。立花証券選択時の流量・負荷方針は§2.3末尾の「立花証券選択時の流量・負荷方針」と`architecture/overview/integrations.md` §5.3）。

### 2.1 スキャンサイクル

| 対象 | 周期目標 | 上限 |
|------|---------|------|
| 全体スキャン（全ユニバース特徴量算出＋Fast Screener。`scan.full_scan_enabled: true`を明示したときだけ。既定はオフでランキング監視＝毎分、FR-SCHED-9） | 60秒ごと | 次サイクル開始までに完了しない場合はスキップしログ記録。「完了しない」とは、サイクル開始時点で`market-data`キューに`pending`/`running`のジョブが1件でも残っている状態を指し、その場合は当該サイクルのジョブ投入を行わず`slog.Warn`（`scheduler: full scan skipped: previous cycle still running`、`pending`＝未完了件数）を記録する。未完了ジョブが0件になった次のサイクルから通常どおり投入を再開する（`jobs`への未処理ジョブの無制限な積み増しを防ぐ）。ワーカーが完了書き込みに失敗して`running`のまま残った孤児行で永久にスキップしないよう、判定の直前に`started_at`が10分より古い`market-data`の`running`行を`failed`（`last_error`＝`orphaned: still running after 10m0s`）へ回復して判定から除外する。しきい値内の`running`行は従来どおり未完了として数える。この孤児回復は全キュー（`market-data`/`feature-calc`/`jev-scout`/`jev-trader`/`outcome-labeling`/`analytics`）に共通で、Schedulerが1分ごとに全キューへ実行する（`internal/service/scheduler/orphans`）ため、`jev-scout`の孤児行が候補更新の間引き（`scoutHeld`）で該当銘柄を再起動まで保留し続けること（`functional/components-pipeline.md` §4.3）や、Activity Logの`running`件数が実態と乖離し続けることも防ぐ。回復した件数はキュー名付きで`slog.Warn`（`orphans: failed orphaned running jobs`、`queue`・`count`・`threshold`）に記録する。しきい値は固定の10分で、既定の全体スキャン周期60秒の10倍に相当するが`scan.full_scan_interval_seconds`には連動しない（周期を変更しても10分のまま）。根拠: Jev呼び出しの最悪所要は約43秒（§2.2）、`market-data`は1銘柄の取得であり、いずれもこれを大きく上回る。想定より長く動いているだけのジョブを早期に`failed`としても、そのワーカーの完了書き込みが後から状態を上書きするため害はない |
| 候補銘柄（Jev Scout/Trader）再評価 | 15〜30秒ごと | Jev API合計呼び出しは1分あたり上位N銘柄（`top_n`。既定20、`config/strategy.yaml`）× 2（Scout+Trader）を上限とする。この上限は、候補更新サイクルからの`jev-scout`ジョブ投入を銘柄ごとに「`pending`/`running`ジョブがある間、および前回完了から`scan.jev_scout_min_interval_seconds`（既定60秒）未満の間はスキップ」することで担保する（Scoutは同一銘柄で1分あたり最大1回。FR-SCAN-1のイベント発火による即時再評価のみ例外。詳細は`functional/components-pipeline.md` §4.3）。Nを引き上げるほど呼び出しコストが比例して増える |
| 保有ポジション監視・Exit評価 | 5〜15秒ごと | Risk EngineのExit判定はJev応答を待たずコード側で即時評価する |

### 2.2 レイテンシ目標

| 区間 | 目標 |
|------|------|
| 板の取得 → Feature Engine算出（1銘柄。既定のランキング監視。板はPUSH優先で、直近30秒以内のPUSH板が無いときだけREST `/board`で補う。下記の登録直後の初回板を除く） | 500ms以内 / 銘柄バッチ（監視銘柄＝最大45件＋指数行。`scan.full_scan_enabled: true`のフルスキャンは約4,000銘柄で§2.3の保証範囲に従う） |
| Fast Screener（監視銘柄→上位N。`scan.full_scan_enabled: true`では全銘柄→上位N） | 2秒以内 |
| Jev Scout 1回呼び出し | 3秒以内（タイムアウト5秒） |
| Jev Trader 1回呼び出し | 3秒以内（タイムアウト5秒） |
| Risk Engine判定 | 100ms以内（外部呼び出しなし） |
| Paper発注〜約定シミュレーション | 200ms以内 |
| UI（Scanner Dashboard）へのライブ反映 | WebSocket経由で1秒以内 |

- **登録直後の初回板〜5秒は既知制限（現状許容、issue #713）**: PUSH登録していない銘柄へのREST `/board`は、kabuステーション側で約5秒かかる（#650実測p50≒5,006ms、[kabusapi#656](https://github.com/kabucom/kabusapi/issues/656)）。ランキング監視＋PUSH前提では監視リストの入れ替えは毎分最大5銘柄なので、新規に監視入りした銘柄の最初の板（PUSH初回到達までのREST補完を含む）がこの遅延を受けることを既知コストとして許容し、上の板取得の目標値には含めない。いまは最適化の対象にしない。**再検討のトリガー**: 監視リストのchurnが高く初回板待ちが実害になる、初回板待ちが直列の`market-data`ワーカーや候補更新・Jev Scoutのスループットを毀損する（`marketdatajob: slow market-data job`の`latest_ms`が常態化する等）、#652の実機計測で未登録`/board`の遅延が変わった、のいずれか（`architecture/overview/integrations.md` §5）。
- **レート逼迫時の方針（issue #709）**: kabu情報APIのレート逼迫（429/`4001006`）への第一手段は流量の平準化（`scan.kabu_info_api_max_per_second`・backoff再試行・次サイクルへの持ち越し。§2.3）であり、監視リスト件数（最大45件）・ランキング種別・ユニバースの削減は主経路にしない。REST `/board`はPUSH登録済み銘柄の補完に限り、PUSH外ユニバースの定期フルRESTは既定にしない（FR-SCHED-7）。補完頻度（`BoardMaxAge`＝30秒等）は#652の実機計測結果で見直せる暫定値（`architecture/overview/integrations.md` §5）。
- Jev API（Scout/Trader）の「タイムアウト5秒」は**HTTP 1試行あたり**の上限とする（`internal/service/jev` の `defaultHTTPTimeout`）。失敗時は最大4試行（初回＋リトライ3回。1回目リトライは即時、2回目以降は500ms・1秒の指数バックオフ）で、全試行失敗時の1呼び出しあたり最悪所要時間は 4×5秒＋1.5秒 = 21.5秒。候補再評価周期（15〜30秒、§2.1）の下限を超え得るが、Scout/TraderのJev呼び出しはJobキュー（`jev-scout`/`jev-trader`）経由の非同期処理で周期を塞がない（両呼び出しがともに全試行失敗した場合の合計は最悪43秒）。「最大4試行・1回目リトライは即時・500ms/1秒バックオフ」は実装（`internal/service/jev/client.go` の `defaultMaxAttempts`/`defaultRetryBaseDelay`）の値であり、`architecture/overview/integrations.md` §6 にも同内容を明記する 429（レート制限）/529（過負荷）は即時再試行せず1回目リトライからbackoff（500ms・1秒・2秒）するため最悪 4×5秒＋3.5秒 = 23.5秒。401/422と不正応答は再試行しない（`architecture/overview/integrations.md` §6）。

### 2.3 スループット・スケーラビリティ

- 対象ユニバースは東証上場銘柄（最大 約4,000銘柄。運用者が銘柄マスタCSVで供給する。`environment/setup.md`「銘柄マスタの投入」）を想定し、`scan.full_scan_enabled: true`（明示オプトイン。既定はランキング監視で、処理対象は監視銘柄＝最大45件）のときの設計目標として、Fast Screener段階まで全銘柄を60秒サイクルで処理できること（現行実装が保証する範囲は下記）。フルスキャンの対象は有効な全銘柄で、市場コンテキスト算出用の`market_index`/`sector_index`行も同じ`market-data`ジョブで処理するため、以下の銘柄数・銘柄あたりコストの試算にはこれらの指数行も含まれる（`functional/components-platform.md` FR-SCHED-2）
  - **保証できる範囲（issue #391 / #514。`scan.full_scan_enabled: true`のフルスキャン）**: `market-data`キューのワーカーは1本で、ジョブを1銘柄ずつ直列処理する（`ClaimNext`→板取得→特徴量算出・スナップショット保存→`MarkSucceeded`。SQLiteは単一ライターで、各コミットがfsyncを伴う）。板取得が即時の場合のDBコストは`BenchmarkFullScanCycle`（`internal/bootstrap/marketdatajob`。フェイク板・SQLite実DB、Ryzen 9 5950X・Linux、他プロセスと共有のSSD。`go test ./internal/bootstrap/marketdatajob -run '^$' -bench FullScanCycle -benchtime 1x`）で計測した: 500銘柄≒19.5ms/銘柄（約9.7秒）、1,000銘柄≒18.8ms/銘柄（約18.8秒）、4,000銘柄≒27.8ms/銘柄（約111秒。同一ホストの負荷により100〜240秒の範囲でばらつく）。市場コンテキスト（指数リターン・市場ブレッドス）は全銘柄共通のため`featureengine/marketcontext.Loader`が30秒キャッシュして全銘柄のジョブで共有する（issue #622。以前は1ジョブごとに指数履歴と全アクティブ銘柄の`return_5m`を読み直しており、4,000銘柄でブレッドス用クエリだけで1回約42msかかっていた）。共有後の同一コマンドの再計測（他エージェントのビルド・テストと並走した負荷の高いホストで、1回ずつ）は、キャッシュ無し→有りで500銘柄≒23.0→35.0ms/銘柄、1,000銘柄≒41.5→33.7ms/銘柄、4,000銘柄≒56.7→39.5ms/銘柄（約227秒→約158秒）で、ホスト負荷のばらつきが大きく銘柄数の少ない条件では差が逆転したが、キャッシュ無しの試算が負荷の影響を受けやすい4,000銘柄ではジョブあたり約17ms短縮した。**4,000銘柄の全体スキャンを60秒以内に完了することは、板取得が即時でも現行の単一ワーカー設計では保証できない**。加えて本番のkabuステーションはローカルRESTで約1.5ms応答するため、レート制御なしでは情報APIの公式上限（10件/秒）を超えて`429`/`4001006`が一斉発生する（issue #514）。**REST板の実効上限は情報APIのプロセス全体レート制限で決まる**
  - **kabu情報APIの流量（issue #514）**: 公式FAQは「発注APIは5件/秒、取引余力APIや情報API、銘柄登録APIは10件/秒」。本システムは発注APIを呼ばない。`GetBoard`（板）・`GetSymbol`（銘柄情報）・`RegisterSymbols`（銘柄登録）はプロセス全体で1つのスライディング1秒窓を共有し、既定は`scan.kabu_info_api_max_per_second`（同梱8。0以下は8に補完、10超は公式上限10に切り下げ）。トークン発行（`/token`）はこの上限の対象外。PUSHで直近30秒以内の板がある銘柄はRESTを消費しない（`scan.full_scan_enabled: true`では最大40銘柄＝API登録上限50のうちREST回転用に10件を空ける。既定のランキング監視では監視リスト最大45件で、残り5件がREST回転用。`architecture/overview/integrations.md` §5）。保有ポジション監視の`GetBoard`と銘柄情報キャッシュ未ヒットの`GetSymbol`も同じ窓を消費する
  - **1サイクルで板を取れる銘柄数と残り**: 既定8件/秒×60秒＝最大480回の情報API枠。うちPUSH未着の板・保有監視・銘柄登録・銘柄情報が枠を分け合うため、RESTで新規に板を取れるのはおおよそ400〜480銘柄/60秒。PUSH対象40銘柄（`full_scan_enabled: true`）を足しても、約4,000銘柄のユニバースを60秒で全件更新することはできない。フルスキャンは有効な全銘柄の`market-data`ジョブを1トランザクションで投入し（空ハンドラの`feature-calc`は投入しない）、レート制限に従って消化する。60秒以内に終わらない分は同じサイクルの続きとして処理し、次の60秒tickは§2.1のとおり投入をスキップする（`scheduler: full scan skipped: previous cycle still running`）。周期は処理時間まで間延びするが、未処理ジョブは積み増されない。REST約3,950銘柄なら既定8件/秒で約8分で一周し、その後に次の全件投入が走る。候補・保有の優先順位付けやREST対象の間引きは行わない（PUSH登録も`symbol`昇順の先頭40件のまま）
  - **429 / 4001006**: 流量超過として扱う。クライアントは1秒待って最大3回再試行し、尽きたら`ErrRateLimited`を返す（`market_data_down`の連続失敗にもstaleにも数えない）。`market-data`ジョブは失敗にせず当該銘柄のスナップショットを作らず次サイクルへ回す（`slog.Warn` `marketdatajob: defer board fetch to next cycle (kabu info api rate limited)`）。リミッタ飽和時は`slog.Warn`（`marketdata: kabu info api rate limiter saturated`、最大1回/秒）を出す。ワーカーの並列化は実施しない（SQLite単一ライターと公式10件/秒の両方に律速されるため）
  - **全銘柄RESTスキャンは既定で停止（issue #652、#651の方針転換）**: `scan.full_scan_enabled`が`false`または省略（既定）のとき、60秒ごとの`market-data`全件投入（上記の約480銘柄/サイクル上限・孤児回復・スキップ判定の対象）を行わない。`true`を明示したときだけ全件投入する（`functional/components-platform.md` FR-SCHED-7）。オフ時はフルスキャンのcronトリガー自体を登録しないため、REST板取得の流量は、ランキング監視（FR-SCHED-9。kabu `GET /ranking`を毎分7種別、監視銘柄は最大45件のPUSH登録＋その`market-data`ジョブ）・保有ポジション監視・PUSH未到達時のフォールバック・`scan.ranking_measure`の計測呼び出しのみとなる。この節の「約4,000銘柄」「1サイクル480回」前提の数値はフルスキャンを明示オンにした場合のものである。`/ranking`の7回/分は情報APIの共有リミッタ（既定8件/秒）に対して無視できる量で、ランキングが空・失敗のサイクルは候補0件でプロセスを止めない
  - **全銘柄REST有効時の足の鮮度（issue #686）**: `scan.full_scan_enabled: true`では各銘柄の足が全件RESTの1周（約4,000銘柄÷8件/秒≒約8分）に1回しか更新されないため、立会中の`stale_snapshot`（FR-SCAN-5/7。Scanner・Jev Scout/Trader・Paper Entryが古い足を使わない）の閾値をランキング監視の3分（`domain.MaxSnapshotAge`）のままにすると、ほぼ全銘柄が常時除外されてしまう。そこでfull scan時だけ`scan.full_scan_max_snapshot_age_seconds`（同梱620秒＝`ceil(4000/kabu_info_api_max_per_second)`＋`2×full_scan_interval_seconds`。未設定・0以下は同じ式で補完し、`kabu_info_api_max_per_second`を下げて1周が延びる場合は引き上げる）を使う。ランキング監視は60秒周期で更新されるため3分のまま
  - **全銘柄REST有効時の履歴ベース特徴量の欠損（issue #693/#694/#696）**: 上記の620秒が緩めるのは最新の足の鮮度判定だけである。約8分間隔の足は窓の基準バーの許容（`max(窓幅/2, 90秒)`。FR-FE-5）を超えるため、`scan.full_scan_enabled: true`では`return_1m/3m/5m`・`volume_*`・`turnover_*`・`market_return_*`・`sector_return_5m`・`market_breadth`が常に欠損となり、Fast Screenerの通過が0件（候補が空）・`market_adverse_to_direction`ゲートが不活性になる。サポートする運用は既定のランキング監視（FR-SCHED-9）で、全件スキャンを有効にすると起動時に警告ログを出す（FR-SCHED-7）
- Scanner Dashboardのスキャン状況パネル用に保持する銘柄別の判定結果（FR-SCAN-3〜6）は最新1サイクル分のメモリ上のみ（約4,000銘柄×数十バイト）で、サイクル周期・Fast Screener 2秒以内の目標に影響させない。4,000銘柄での計測（issue #303、`go test -bench`・Ryzen 9 5950X・SQLite実DB）: Fast Screener段階（`screener.Run`→`Screen`）は約0.28ms→約0.33ms/サイクル（追加確保は約8KB=銘柄あたり2バイトの理由ビットマスクのみ。これは#303計測時点の`uint16`の値で、#511で`uint32`化したため現行は約16KB=銘柄あたり4バイト）、候補更新サイクル全体（DB読込・特徴量入力組立・保持込み）は確保量が約45.0MB→約45.4MB（+約1%、確保回数は+10回）で所要時間は計測ばらつきの範囲内（約1.1〜1.5秒、大半は既存のSQLite読込）。どちらも60秒周期・Fast Screener 2秒以内の目標に影響しない
- Jev API呼び出しは全銘柄ではなくFast Screener通過銘柄の上位N（`top_n`。既定20。50〜200は引き上げ時の目安）→ Scout通過銘柄（Nの一部）に限定し、API呼び出しコストを抑制する（§4.3 再評価抑制も適用）
- 単一Windowsホスト・単一プロセス（Wails）構成のため、将来的にスキャン対象拡大や複数戦略同時運用が必要になった場合は、Scheduler/Worker層をプロセス分離・別ホスト化できるよう、内部レイヤー（domain/repository/service）をWailsプロセスに直接依存させない設計とする（詳細は`architecture/overview.md`）
- **立花証券選択時の流量・負荷方針（issue #721。#720の決定。仕様は`architecture/overview/integrations.md` §5.3）**: REQUEST I/Fは同時1要求・最大10件/秒（設計上限であり保証値ではない。**本システムのキュー全体の既定は1件/秒**で、Settingsの`broker.tachibana.request_max_per_second`（1〜10）で変更する。REQUEST/MASTER/PRICEの3仮想URLをまたぐ1本の直列キューで、優先度は セッション＞保有銘柄の時価＞監視銘柄の時価＞マスタ＞夜間の日足取得。夜間の日足取得は8:00〜15:30にキューから出さない。環境変数では設定しない。#727）で、8:00〜15:30は大量・頻繁な時価取得（`CLMMfdsGetMarketPrice`）とポーリングを控える。マスタは5:30〜8:00に朝1回だけ取得し、ポーリングではなくEVENT（WebSocket）を使う。**日中に全銘柄の時価を巡回しない**（全銘柄の絞り込みは夜間18:00以降の日足で行い、翌日の120銘柄を選ぶ。#726）。日中はEVENTで120銘柄を常時受信し、REST時価は補完・保有確認だけ（既定60秒に1要求以下）とする。EVENTの接続・切断は1日10回程度までとし、ログインは毎朝1回（5:30以降）で仮想URLを当日使い回す（WebSocket切断は再ログインせず同じ仮想URLで再接続。再認証は`p_errno=2`等のセッション失効のときだけ）。これらの既定値はSettings画面で変えられ、環境変数では持たない。上のkabu情報APIの流量（`scan.kabu_info_api_max_per_second`・PUSH登録上限）はkabu選択時のもの

## 3. 可用性

- 目標: 24/365常時稼働（夜間・週末を含む）。ただし発注・新規エントリーは東証立会時間（9:00-11:30 / 12:30-15:30 JST）のみ行う
- 立会時間外は市場データ取得・Jev呼び出し・新規発注を停止し、Scheduler/Workerは待機状態に入る（無駄なAPI課金を避ける）
  - 引け前強制決済ウィンドウ（大引け − `force_flat_before_market_close_minutes` 以降）でも新規発注は停止する（建てた直後に強制決済されるため。`execution.ErrOutsideTradingSession`）
- 立会時間の判定は `internal/service/marketcalendar`（東証カレンダー）が行う: 前場 9:00-11:30・後場 12:30-15:30 JST（開始時刻を含み終了時刻を含まない。11:30-12:30の昼休みは立会外）。土日・国民の祝日（振替休日・国民の休日を含む。2020年以降の祝日法に基づく判定）・年末年始休場（12/31, 1/1-1/3）は終日立会外とする。臨時休場（システム障害等）は扱わない
- 立会時間外に停止する対象: フルスキャン・イベント再評価のenqueue、候補更新に伴うJev Scoutのenqueue、保有ポジション監視の板取得、新規エントリー（`execution.ErrOutsideTradingSession`）、市場データ停止/Jev API停止の検知、操作者ハートビート判定。バックアップ・データ削除・ログローテーション等のメンテナンスジョブとOutcome Labelingは保存済みデータのみを扱うため立会時間外も実行する
  - 停止中であることはScanner Dashboardのスキャン状況パネルに通知する（`functional.md` FR-SCAN-7）。候補リスト（既定のランキング監視では直前の立会時間内サイクルの監視リスト＋保有・注文中。`functional/components-platform.md` FR-SCHED-9）は保存済みデータで更新され続けるため、通知は「市場データ取得・フルスキャン・Jev Scoutは停止中、表示は保存済みデータに基づく」と案内し、次回の立会開始時刻（`marketcalendar.Calendar.NextOpen`）を併記する
- Windowsへのログイン時にWailsアプリを自動起動し、クラッシュ時は自動再起動する。実装: NSISインストーラー（`cmd/desktop/build/windows/installer/project.nsi`）のコンポーネントページ「Start at login and restart after a crash」（既定でON）が、スタートアップフォルダに`--supervise`付きのショートカットを作成する（アンインストールで削除。無人自動更新`/S`では初回インストール時の選択を維持する）。`--supervise`で起動した`pitha-trador.exe`は自身を子プロセスとして再実行して監視し（`internal/supervisor`）、終了コードが非0またはkillされた場合に指数バックオフ（1秒〜最大5分、1分以上安定稼働したらリセット）で再起動する。終了コード0（操作者による終了・自動更新による終了・起動失敗ダイアログ後の終了）では監視を終了し、意図した停止と競合しない。再起動中はプロセスごと停止しているためSchedulerによる新規エントリーは行われず、再起動後は既存ポジションのExitルールが再開する。Wailsアプリはデスクトップセッションを要するため、起動契機は「Windowsホスト起動」ではなく「ユーザーログイン」であり、ホスト再起動後の無人復帰にはWindowsの自動ログオン設定を前提とする。自動起動とデスクトップアイコン起動の併存による二重稼働（Scheduler・Kill Switch・発注の重複）を防ぐため、アプリ本体・`--supervise`監視プロセスはそれぞれDBと同じディレクトリのロックファイル（`app.lock`／`supervisor.lock`。`internal/singleinstance`）を`bootstrap.Run`より前に取得し、取得できない2つ目の起動は終了コード0で即終了する（OSがプロセス終了時にロックを解放するため、クラッシュ後の残留ロック解除は不要）
- kabu選択時: kabuステーションアプリ本体はサードパーティ製でログイン操作を要するため、pitha-trador側からは起動・再起動しない。自動起動はkabuステーション自身の設定（またはオペレーターによるWindowsタスクスケジューラ登録）で行う。kabuステーションAPIの停止・異常はKill Switch発動条件（`architecture/overview/flows.md` §11）として扱う
- **立花証券選択時の可用性（issue #721）**: 立花の仮想URLは03:30の閉局で失効し、03:30〜05:30はログインできない。このため**毎朝1回、05:30以降（既定05:35。Settingsで変更可）に自動で再認証**し、当日は同じ仮想URLを使い回す（WebSocket切断は再ログインせず同じ仮想URLで再接続し、再認証は`p_errno=2`等のセッション失効のときだけ）。ログイン停止帯ではログインを試みない。実装（#727）: 03:30にセッションを閉じて`SessionStatus.Issue=out_of_hours`（バナーに「開局後に自動で再ログインします」）とし、05:35（既定）に再認証、失敗は5秒から倍々で上限5分のバックオフで再試行し、**8:30までに成功しなければSlack・Activity feedへ通知**する。日中の再認証は`p_errno=2`のときだけで、最小間隔30秒・1時間に3回まで（超過は次の定時再認証まで待つ）。ログイン直後に繰り返し切られる場合は「別プロセス・別ツールとの取り合い」として`rejected`＋通知する。アプリ終了時はログアウトして仮想URLを無効化する。再認証に失敗してもアプリは継続起動し、全ページ共通の市況データ接続バナーと§5.2のSlackで原因別に通知しながら再試行する。人手のログインは要らない
- **立花証券のAPI版数の廃止監視（issue #721）**: 立花は後続版を並行リリースしたのち旧版を廃止する（v4r10の並行リリース2026-08-29→v4r9廃止2026-09-27）。①ログイン応答の`sUpdateInformAPISpecFunction`/`sUpdateInformWebDocument`の変化を検知してSlackへ自動通知する。②API専用ページ「リリース＆改定情報」を定期確認する運用とする（例: 週1。§3.1）。③後続版の並行リリースから**約30日以内**に接頭辞・I/Fの変更へ追従する。④各種書面が未読の間は、再認証が正常応答でも仮想URLが発行されずAPIが止まるため、標準Webで書面を既読にする（手順は§3.1）
- SQLiteのDBファイル（アプリ内蔵）を日次でバックアップし、ローカルディスク外（外部ストレージ/クラウド）へ退避する。保持期間は直近90日分のフルバックアップ＋週次アーカイブ。バックアップ時はWALチェックポイント（`PRAGMA wal_checkpoint(TRUNCATE)`）を実行してから複製する
  - 実装: Schedulerの日次ジョブ（`internal/service/backup`）。退避先はSettings画面（`/settings`の「運用設定」> バックアップ先。`runtime_settings`の`system.backup_dir`、環境変数`PITHA_BACKUP_DIR`は廃止。`environment/setup.md`）で指定する絶対パスで、未設定時は無効（起動ログに「`/settings`で設定する」旨の警告。未設定の間はメンテナンスタスクがスキップされ、設定後の次回チェックで即実行される）。設定・変更・クリアは再起動不要で、Schedulerの次回メンテナンスチェック（10分以内）から反映される。退避先ディレクトリ自体は作成せず、存在しない場合（外付けドライブ/クラウド同期フォルダ未マウント等）はローカルディスクへ静かに退避せずエラーにする。書き込み中の生ファイルコピーは不整合になり得るため、複製はチェックポイント後の`VACUUM INTO`（単一スナップショット）で行い、確定前に`secrets`テーブル（アプリ埋め込み鍵で暗号化されたAPIキー/パスワード）を空にして`PRAGMA integrity_check`で検証する。復元後はSetup画面で秘密情報を再入力する。ディレクトリは`0700`、ファイルは`0600`で作成する。日次分は`daily/pitha-YYYY-MM-DD.db`に保存し90日超を削除、各ISO週の最初のバックアップ（起動が平日のみでも週1つ生成される。週の月曜日付）を`weekly/pitha-YYYY-MM-DD.db.gz`（gzip）として52週保持し超過分を削除する
  - 実行タイミング: デスクトップアプリは日中のみ起動する運用のため、深夜0時固定のcronは実行されない。バックアップ・データ保持パージ・ログアーカイブは、最終成功日を`runtime_settings`（`system.maintenance.<task>.last_success_date`）に保存し、起動直後および10分ごとに「本日未成功なら実行」（catch-up）する。バックアップは加えて毎日16:00（ローカル時刻）にも実行する。失敗時は30分後に再試行し、3回連続で失敗した時点でSlack（未設定時は構造化ログ）へ通知する
- 高頻度書き込みテーブルはDBの無制限な肥大を防ぐため保持期間を設け、Schedulerの日次ジョブ（上記catch-up方式）で期限切れ行を削除する（`internal/service/retention`）。`jobs`は完了行のみ対象で`succeeded`は7日・`failed`は30日（`pending`/`running`は削除しない）、`market_snapshots`は90日（対応する`market_snapshot_vectors`行も同時に削除）。削除は1000行単位のバッチで行い、満杯のバッチごとに200ms待機して（SQLiteのbusy handlerは最大100msおきにしかリトライしないため、待機なしだとバッチ間の隙間に入れず並行INSERTが飢餓する）ワーカーの書き込みを長時間ブロックしない。監査対象テーブル（`kill_switch_events`/`kill_switch_resolutions`/`jev_decisions`/`paper_orders`等）は削除対象外。削除済みページは以後の書き込みで再利用されるためファイルは増え続けないが縮小はしない（バックアップの`VACUUM INTO`は縮小済みで出力される）

### 3.1 朝の起動チェック（kabu選択時: 手動再ログイン、立花証券選択時: 自動再認証の確認。issue #712・#721）

kabu選択時の方針: メンテナンス明け・セッション切れ後のkabuステーション再ログインは**操作者が寄り付き前（9:00前）に手動で行う**。アプリはkabuステーションへ自動ログインしない（自動GUIログインはスコープ外）。アプリ側は現行どおりトークンを再試行し（30秒から指数バックオフで再発行間隔まで。成功するまで無期限）、全ページ共通の市況データ接続バナー（`MarketDataBanner`。FR-SETTINGS-5）で原因と対処を示す。立花証券選択時の確認は下の「立花証券選択時の朝の確認」。

朝の起動チェック（kabu選択時。寄り前）:

1. kabuステーションを起動し、**ログイン済みか**（右上のAPIアイコンが緑か）を確認する。メンテ明けやセッション切れ後は未ログインのことがあるため、緑でなければ手動でログインする（一度ログアウトして再ログインしてもよい）。
2. 本アプリを起動（Windowsログイン時の自動起動を含む）し、ヘッダーに「市況データを取得できません。」のバナーが出ていないことを確認する。ログイン直後はアプリの次の再試行（最長で再発行間隔まで）で自動復旧しバナーが消えるため、再起動は要らない。
3. 寄り付き（9:00）までにバナーが消えない場合は下記の確認項目を順に辿る。

`4001007` / `4001017`（未ログイン／セッション切れ）が続くときの確認項目（バナーの`data-issue="not_logged_in"`。`4001007`・`4001017`が連続5回以上または初回失敗から5分以上続くと、継続時間と失敗回数つきの強調表示になり「再ログインしてから、そのままお待ちください」を示す）:

- **ログイン状態**: kabuステーション右上のAPIアイコンが緑か。緑でなければ（またはメンテ・長時間経過後は）一度ログアウトして再ログインする。「APIを利用する」オンでも`4001007`は出る。API利用設定は`4001008`、APIパスワード不正は`4001013`で別に通知されるため、`4001007`・`4001017`ではAPIパスワードやAPI設定を変更しない（issue #305）。
- **他プロセスの`/token`競合**: 同じkabuステーションの`/token`を呼ぶ他のAPIツール・スクリプトがないか確認し、あれば止める。`/token`の発行は新しいトークンで古いトークンを失効させるため、競合すると互いのトークンを失効させ合う（本アプリは情報系APIが`401`/`4001009`を返すとシングルフライトで再発行して1回再試行する。`architecture/overview/integrations.md` §5）。
- **二重起動**: 本アプリが二重に起動していないか（Windowsのタスクトレイ・タスクマネージャーで`pitha-trador`が複数ないか。自動起動と手動起動の併用、`--supervise`による再起動と手動起動の重なりに注意）。複数あれば1つだけ残して終了する。
- ログイン済み・競合なしでも`/token`は成功するのに情報系APIだけ`4001007`が続く場合は、バナーが`rejected`（認証サーキットブレーカー。`architecture/overview/integrations.md` §5）になる。上記の他プロセス確認と、kabuステーションの一度のログアウト→再ログインを行い、30秒ごとの再試行で復旧するのを待つ。
- 再ログイン後もバナーが消えない場合は、エラーログ（§5.3）の`marketdata: token issuance failed`/`token reissue failed`の`issue`・`error`を確認する。

#### 立花証券選択時の朝の確認（issue #721）

立花証券選択時（`broker.provider`が立花）はkabuのような手動ログインは要らない。アプリが毎朝05:30以降（既定05:35）に自動で再認証する。操作者は寄り付き前（9:00前）に次を確認する:

1. **再認証の成功**: ヘッダーに「市況データを取得できません。」のバナーが出ていないこと。出ている場合は原因別の案内に従う。多重ログイン（同じ認証IDを別のプログラム・別の本アプリ＝二重起動が使っている）なら、片方を止める（仮想URLは1顧客1つで、後からのログインが先を失効させる）。03:30〜05:30はログイン停止帯のため、その間のエラーは異常ではない。
2. **API版数の予告**: Slackに「API版数・書面更新の予告」の通知が来ていないか（ログイン応答`sUpdateInformAPISpecFunction`/`sUpdateInformWebDocument`の変化。§5.2）。来ていれば[API専用ページ「リリース＆改定情報」](https://www.e-shiten.jp/e_api/mfds_json_api_menu.html)を確認し、並行リリースから約30日以内（旧版の廃止前）に追従する。
3. **週1回の定期確認**: 上記の自動通知の取りこぼしに備え、「リリース＆改定情報」ページを週1回確認する運用とする。
4. **書面未読**: 再認証が正常応答でも仮想URLが発行されず、バナーが消えない場合は、標準Web（本番は`https://www.e-shiten.jp/`のログイン、デモは`https://demo.e-shiten.jp`）にパスキーでログインし、未読の各種書面（金商法交付書面等）を既読にしてから次の再認証（または再起動）を待つ。API利用設定が「利用しない」になっていないか、公開鍵が登録されているかも確認する。

## 4. セキュリティ

- Jev APIキーはGoバックエンドプロセス内のみで保持し、フロントエンド（Templ/HTMX/Lit）に露出させない
- kabuステーションAPIのAPIパスワード（`KABU_API_PASSWORD`）はSQLiteの`secrets`テーブルにAES-256-GCMで暗号化して保存する（鍵はアプリ埋め込みシード由来で、保護対象はDBファイル単体の複製・共有時の平文流出。OS資格情報ストアは使用しない。`architecture/er/tables-system.md` §secrets）。kabuステーションAPIの発行トークンはメモリ上にのみ保持し、ディスク（設定ストア・`secrets`テーブル含む）へは保存しない（`architecture/overview/integrations.md`）
- Broker（kabuステーション）APIキー/パスワードはProduction用とPaper Trading用を分離して管理する。Phase 7（実売買）までは、`KABU_API_PASSWORD`（単一キー）はkabuステーションAPIのトークン発行・銘柄登録・板情報取得（市場データ読み取り）にのみ使用し、注文エンドポイント（発注・取消）は呼び出さないため発注可能な認証情報は保持しない。Phase 7で注文エンドポイントを呼び出す前に、Production用とPaper Trading用のキー・接続先（Base URL）を別々のsecretsキーとして追加し、Paper Tradingが実口座の認証情報を参照できない構成にする（現時点では未実装）
- **立花証券選択時の認証情報**: 秘密鍵はファイルで保持し、パスとOS権限（本人のみ読み取り可）で保護する。DB（`secrets`テーブル・バックアップ含む）には秘密鍵の本文を置かない。認証ID（`sAuthId`）はデモと本番で別セットのため別のキーで管理し、取り違えないようにする。**仮想URLは発注・時価の権限を持つ秘密として扱い**、メモリ上のみで保持し、ログ・エラー・エラーログのエクスポートに出さない（URLを含むエラー文字列はマスクする。実装（#727）: 仮想URLは型が自分自身を`[redacted]`にし、`*url.Error`は取り除いて返す。認証ID・秘密鍵・仮想URLがログ・エラー・`SessionStatus`・通知・画面に出ないことはテストで保証している）。API側の固定IP登録は任意で、動的IP回線では使わない。第二暗証番号は、本番では発注（#55）を実装するまで保持しない
- 立花証券 e支店API（issue #723/#733）の認証情報: 認証ID（デモ/本番別）は`secrets`テーブルに暗号化して保存する。**秘密鍵（RSA 2048/4096のPEM）はファイルとして保管し、パスのみを`runtime_settings`に保存する**（ファイルはOSのユーザー権限で保護し、クラウド同期フォルダには置かない。保存時にPEMの秘密鍵であることを検証し、中身は画面・ログに出さない）。秘密鍵の本文を`secrets`へ保存する案は、暗号鍵がアプリ埋め込みシード由来で実質的な保護にならないため採らない。**第二暗証番号は本番環境では保持しない**（本番のキーを設けず、デモの第二暗証番号も本番環境では読み込まず・立花へ送る要求に含めない。本番の保持はProduction/Paper分離と共にissue #55で扱う）。したがって本番の第二暗証番号を持たない限り、立花経由の本番発注はできない
- Secrets（APIキー・パスワード・DB接続情報）はGitに保存しない。`.env`はコミット対象外とし、`.env.example`のみをリポジトリに含める
- Wailsアプリのページ・APIはWails AssetServer（プロセス内、ネットワークポートなし）経由でのみ配信する。Windows（WebView2）のみ、AssetServerがWebSocketを扱えないため`/ws/...`のUpgradeだけを受ける専用のループバックリスナー（`127.0.0.1`と`[::1]`の同一ランダムポート。`[::1]`が使えない環境は`127.0.0.1`のみ。ページ・APIは配信しない）を追加で起動する。いずれもループバックにのみバインドし、外部ネットワークからアクセス不可とする（`api/endpoints.md` §6、`cmd/desktop/ws_listener.go`）
- HTMXフォームにはCSRFトークンを付与する（HALT標準構成に準拠。ローカル単一ユーザーでも実装は省略しない）
- 実売買（Phase 7）へ移行しても、発注確定・Kill Switch操作に人手の追加認証は要求しない（完全自動運用）。安全性はRisk Engine側のLive専用の厳格なリミット（`requirements/functional.md` §4.7）と、操作者ハートビートが一定時間途絶した場合に自動で新規エントリーを停止するdead-man's switch（FR-RISK-6）で担保する
- Risk Engineの拒否・Kill Switch発動・実売買発注はすべて監査ログ（追記専用ログ）に記録する。`kill_switch_events` / `kill_switch_resolutions`はDBトリガーで`UPDATE`/`DELETE`を拒否して追記専用を強制する（`architecture/er.md` §kill_switch_events）。ハッシュチェーン等によるDBファイル自体の改ざん検知は行わない

## 5. 監視・アラート

MVPでは構築コストを抑え、構造化ログ＋Slack Webhook通知のみで運用する（将来Prometheus等への拡張を妨げない形でログ出力を設計する）。

### 5.1 必須ログ（構造化JSON、レベル別）

- Market data fetch latency / エラー率
- Market data job の区間別所要（1件が1秒を超えたとき`marketdatajob: slow market-data job`をWARNで出力し、`latest_ms`（板取得）・`history_ms`・`context_ms`・`symbol_ms`（銘柄情報）・`cycle_ms`（特徴量の保存とRAG索引）・`execution_ms`を併記する。約4,000銘柄を1サイクルで処理する前提のため、1件1秒超でサイクルが間に合わなくなる支配区間を特定するためのログ）
- kabu `/ranking`計測ログ（`scan.ranking_measure.enabled: true`のときだけ出力。`rankingmeasure: ranking measured`に`type`・`exchange`・`duration_ms`・`ok`・`result_count`・`duplicate_ranks`・`current_price_time`（`HH:mm`）、失敗時は`http_status`・`kabu_code`・`rate_limited`・`error`のみを記録し、ランキングの価格・出来高・銘柄コードなど時価データはログにもDBにも出さない。kabu利用規約。`functional/components-platform.md` FR-SCHED-8）
- ランキング監視ログ（`rankingwatch:`。FR-SCHED-9。既定で毎分出力）: `watch list updated`/`ranking is empty`/`ranking failed`/`ranking recovered`に`ranked_rows`（ランキング件数）・`ranked_in_universe`・`held`・`watch`・`added`・`removed`・`duration_ms`、失敗時は`error_code`（kabuコード）・`rate_limited`・`consecutive_failures`・`error`のみを記録する。ランキングの価格・出来高・銘柄コードはログにもDBにも出さない。失敗・空の繰り返しは10分に1回に間引く
- Jev API latency / エラー率
- スキャン対象銘柄数、Scout呼び出し回数、Trader呼び出し回数
- Signal count（生成シグナル数。`policy: trade signal decided`ログ。永続化する`Evaluate`のみが出力し、バックテスト再現の`Decide`は出力しない）
- Risk拒否件数
- kabuステーションAPI（Broker）latency / エラー
- 立花証券選択時（#727）: `tachibana:`ログ。ログイン成功（INFO。次回再認証時刻・有効期限）、03:30の閉局（INFO）、ログイン失敗（WARN。`issue`・`code`・`failures`・`retry_in`。認証ID・仮想URLは出さない）、`p_errno=2`による再ログイン（WARN）、`p_errno=6`（ERROR。採番バグ）、`p_errno=8`（WARN。NTP同期の案内）、運用者向け通知（WARN。`kind`＝`login_overdue`/`contention`/`documents_unread`/`api_spec_update`/`document_update`）、ログアウト（INFO）
- DB latency / エラー（エラーはERROR、100ms以上の低速クエリはWARN、それ未満の正常クエリはDEBUG。ワーカーのアイドルポーリングでログが肥大化しないよう、正常クエリはINFOで記録しない）

### 5.2 即時Slack通知対象

- 市場データ停止（一定時間データ更新なし）
- Jev APIエラー率上昇（しきい値超過）
- ブローカーAPI異常（kabuステーション／立花証券）
- 立花証券選択時: 毎朝の再認証の失敗（8:30までに成功しない場合。`login_overdue`）、API版数・書面更新の予告（ログイン応答`sUpdateInformAPISpecFunction`/`sUpdateInformWebDocument`が「予定日≧当日かつ前回値と異なる」とき。`api_spec_update`/`document_update`）、書面未読（`documents_unread`）、セッションの取り合い（多重ログインによる仮想URL失効。`contention`）。同じ通知はActivity feedに`broker_notice`イベントとして残る（インメモリ・直近50件）
- Kill Switch発動（自動再開可否・発動理由を含む）
- Kill Switch自動再開・手動再開待ち（Live専用、`functional.md` FR-RISK-7）
- 操作者ハートビートタイムアウトによる新規エントリー自動停止（Live専用、FR-RISK-6）
- 想定外ポジション検知
- 日次損失上限接近（例: 上限の80%到達）

- ログは日次ローテーションし、直近30日分をローカル保持、それ以前は圧縮アーカイブする。ファイルは全ログ`<YYYY-MM-DD>.log`（INFO以上）と、ERRORのみを同時に書き出す`<YYYY-MM-DD>-error.log`の2本をログディレクトリに置く（アーカイブ・保持は両方に同じ規則を適用）。エラーだけをPCのファイルとして直接開いて調べるための分割で、UIからのダウンロード（下記）は従来どおり全ログ側から抽出する

### 5.3 エラーログのエクスポート

- §5.1の構造化ログのうちエラー（`level=ERROR`、任意で`WARN`以上）を、運用者がUIからダウンロードして調査・共有できる（`requirements/functional/components-platform.md` §4.19 FR-ERRLOG-1〜7）。ログの日次ローテーション・30日超のアーカイブ・保持は変更しない
- エクスポートは共有を前提に、認証情報（APIキー・パスワード・トークン・Slack Webhook URL、立花証券の認証ID・秘密鍵・第二暗証番号・仮想URL等）を出力時にマスクする（対象のキー名・文字列パターンは`requirements/functional/components-platform.md` FR-ERRLOG-3）。ログ出力側で秘密情報を出さない方針は維持し、マスクは多層防御とする
- 出力は最大10MiB、読み取り専用で、ローカルループバック上のセッション認証必須（§4）

## 6. コンプライアンス・運用前提

- 本システムは個人の自己資金運用を前提とし、第三者資金の運用・金融商品取引業登録を要する業務は対象としない
- AIモデル（Jev）の出力を発注権限そのものとして扱わず、必ずコードベースのRisk Engineを経由する
- Jevのconfidence/probabilityをそのまま実勝率として扱わない（Calibrationで独自検証する）
- 実資金投入前に十分なバックテスト・Paper Trading・スリッページ/手数料/流動性を含む検証を行う
- 市場データの取得元は選択したブローカー（kabuステーション／立花証券）の利用規約・規程に従う。kabu選択時の`/ranking`の値を保存・出力しない方針はFR-SCHED-8・§5.1のとおり
- 免責: 短期売買は損失リスクが高く、本仕様はソフトウェア設計・研究用途を想定する
- **立花証券選択時の情報利用（issue #721。#720の決定。立花には問い合わせず、公開資料で判断した）**: 立花のAPI専用ページ「４．ご利用にあたって」は「当社提供情報は、蓄積、編集および加工等は禁止しております」とし、利用は[インターネット取引規程](https://www.e-shiten.jp/TorihikiRule/stipulation/pdf/int_kitei.pdf)の範囲内を求める。規程第18条は自己の証券投資目的に限る利用・第三者提供の禁止・過負荷の禁止を定める。本システムの方針は次のとおり。
  - **自己の証券投資目的に限り、取得した時価・板のローカル保存と分析（特徴量・バックテスト・キャリブレーション等）は可**とする（`market_snapshots`・`raw_data_json`等。`architecture/er/tables-market.md`）。
  - **第三者提供・再配信はしない**。エクスポート等で立花の情報が第三者に渡る経路を作らない（エラーログのエクスポートに時価データを出さない。§5.3）。ニュースは特に外に出さない。
  - 過負荷を避けるため、日中は全銘柄を巡回しない（§2.3）。
  - **根拠**: ①API専用ページ「４．ご利用にあたって」の上記の一文、②規程第18条が加工の禁止を「営業に使用したり、第三者へ提供する目的」に限っていること、③公式サンプルプログラムが取得データをCSVに保存していること。**残るリスク**: API専用ページの一文には自己利用の例外が明記されていない。立花から利用停止・是正の連絡があれば、保存と分析の範囲を見直す。

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
| 1.22 | 2026-10-06 | §5.1に`marketdatajob: slow market-data job`（1件1秒超の区間別所要）を追記し、§5.2のログ保持にエラー専用ファイル`<YYYY-MM-DD>-error.log`（ERRORのみ・全ログと同じ規則でアーカイブ）を追記 | 1件約6秒かかる原因の特定とエラーの確認のしやすさ |
| 1.23 | 2026-10-06 | §2.3に全銘柄RESTスキャンの停止設定（`scan.full_scan_enabled`）、§5.1にkabu `/ranking`計測ログ（`rankingmeasure: ranking measured`。計測値のみで価格・板は出さない）を追記 | issue #652（#651 段階0） |
| 1.24 | 2026-10-06 | §2.3の全銘柄RESTスキャンを既定オフ（`scan.full_scan_enabled`の省略時・同梱既定を`false`へ変更）に改め、ランキング監視（FR-SCHED-9）の流量を追記、§5.1にランキング監視ログ（`rankingwatch:`。件数・所要時間・kabuコードのみ）を追記 | PR #653 の方針変更（#651の結論に従いランキング方式を既定化。#652） |
| 1.25 | 2026-10-07 | §3の立会時間外の通知について、既定のランキング監視でも候補リストは直前の立会時間内の監視リストを保存済みデータで表示し続ける旨を明記 | issue #668 |
| 1.26 | 2026-10-07 | §2.3のPUSH登録上限を「最大50銘柄」「先頭50件」から実装（`pushfeed.MaxRegisterSymbols`＝API登録上限50−REST回転用10）と`architecture/overview/integrations.md` §5に合わせ40件へ訂正 | issue #672 |
| 1.27 | 2026-10-07 | §2.3に`scan.full_scan_enabled: true`時の足の鮮度（`stale_snapshot`の閾値を全件REST1周に合わせた`scan.full_scan_max_snapshot_age_seconds`、同梱620秒）を追記 | issue #686 |
| 1.28 | 2026-10-07 | §2.3のScanner用理由ビットマスクの追加確保量の注記を訂正（#303計測時点`uint16`の約8KBに対し、#511で`uint32`化した現行は約16KB=銘柄あたり4バイト） | issue #689 |
| 1.29 | 2026-10-07 | §2.3に、`scan.full_scan_enabled: true`では約8分間隔の足が窓の基準バーの許容（FR-FE-5）を超えるため履歴ベース特徴量が常に欠損となり候補が空・`market_adverse_to_direction`が不活性になること（`scan.full_scan_max_snapshot_age_seconds`は鮮度判定だけを緩める）と、サポートする運用は既定のランキング監視で起動時にWARNログを出すことを追記 | issue #693・#694・#696 |
| 1.30 | 2026-10-07 | §2.3の「全銘柄を60秒サイクルで処理できること」を`scan.full_scan_enabled: true`（明示オプトイン）のときの設計目標に限定し、PUSH登録40銘柄・「保証できる範囲」も同モードの値と明記、既定のランキング監視は監視リスト最大45件（残り5件がREST回転用）と補足 | issue #698 |
| 1.31 | 2026-10-08 | §3.1を追加し、メンテ明け・寄り前のkabuステーション手動再ログイン運用（朝の起動チェック）と`4001007`/`4001017`継続時の確認項目（ログイン状態・他プロセスの`/token`競合・二重起動）を明記 | issue #712 |
| 1.32 | 2026-10-08 | §2.2のレイテンシ表の板取得・Fast Screener行を既定のランキング監視前提（PUSH優先・REST補完。フルスキャンは`scan.full_scan_enabled: true`のとき）に直し、登録直後の初回板〜5秒の既知制限・再検討トリガーとレート逼迫時の方針（監視リスト件数・ランキング種別を削らない）を追記 | issue #709・#713 |
| 1.33 | 2026-10-08 | §3のバックアップ先を環境変数`PITHA_BACKUP_DIR`からSettings画面（`runtime_settings`の`system.backup_dir`）へ移行（未設定時は無効、設定は再起動不要、未設定の間はスキップ） | issue #708 |
| 1.34 | 2026-10-08 | ブローカー選択（kabu既定・立花証券。1プロセス1社、手動切替）を前提に整理: §1に前提（kabu＝同一Windows常駐アプリ、立花＝インターネット直結・IPv4のみ・NTP同期必須）、§2.3に立花の流量・負荷方針（日中の全銘柄巡回なし・EVENT120銘柄常時受信・REST補完は60秒に1要求以下・EVENT接続切断は1日10回程度・毎朝1回ログイン）、§3に毎朝の自動再認証とAPI版数の廃止監視、§3.1にkabu選択時／立花選択時の朝の確認、§4に秘密鍵・仮想URL・認証IDの扱い、§5.2に再認証失敗・版数予告・セッション取り合いの通知、§6に立花の情報利用制限と#720の決定（自己投資目的のローカル保存・分析は可、第三者提供・再配信は不可）を追記 | issue #721（#720の決定） |
| 1.35 | 2026-10-08 | §4に立花証券 e支店API（issue #733）の認証情報の扱いを追加（秘密鍵はファイル＋OS権限で保護しパスのみ保存、第二暗証番号は本番では保持しない） | issue #733 |
| 1.36 | 2026-10-08 | §5.3のエラーログのエクスポートのマスク対象に立花証券 e支店APIの認証ID・秘密鍵・第二暗証番号・仮想URLを追加（詳細はFR-ERRLOG-3） | issue #736 |
| 1.37 | 2026-10-08 | 立花証券アダプタの認証・セッション・REQUEST I/Fクライアント（issue #727）を反映: §1にp_errno=8のNTP案内、§2.3にキュー全体の既定1件/秒と優先度・夜間の日足取得の時間帯制限、§3に03:30閉局〜05:35再認証・バックオフ・8:30通知・日中再認証の制限・終了時ログアウト、§4に仮想URLの秘匿実装、§5.1/§5.2に立花のログと通知種別を追記 | issue #727 |
