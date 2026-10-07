# アーキテクチャ設計: 外部・内部連携（§5〜§9・§12〜§13）

`docs/architecture/overview.md` から分割した章。§5 kabuステーションAPI連携 / §6 Jev API連携 / §7 RAG連携 / §8 自己改善ループ / §9 Wails統合 / §12 System Activity Feed連携 / §13 Luna ニュース分類・News Ingest連携。節番号は分割前と同一で、コードコメント等の `overview.md §<番号>` は本ファイルの同番号の節を指す。

## 5. kabuステーションAPI連携

- kabuステーションは三菱UFJ eスマート証券（旧auカブコム証券）が提供するWindows常駐アプリで、`http://localhost:18080`（既定）にローカルRESTを公開する。Go側の `internal/service/marketdata` はこれをHTTPクライアントでラップする
- **トークン発行**: アプリ起動時に `/kabusapi/token` へAPIパスワードでPOSTしトークンを取得。トークンは有効期限があるため、Wailsアプリ起動時および定期的に再発行し、メモリ上にのみ保持する（ディスクへは保存しない）。さらに、kabuステーションの再ログイン・再起動や別プロセスの `/token` 呼び出しでトークンが失効した場合に備え、情報系API（板・銘柄情報・銘柄登録）が `401` または `4001009`（APIキー不一致）を返したら、シングルフライトで即時に再発行して元のリクエストを1回だけ新トークンで再試行する（同時に走る複数リクエストは再発行1回に束ね、再発行の連発は10秒以上空ける。再発行に失敗した場合は元のエラーを返し、市場データ停止の連続失敗カウントは従来どおり）。初回発行に失敗してもアプリは継続起動し（kabuステーション未起動の開発機でも他機能を使えるようにする）、トークンを保持するまでバックグラウンドで指数バックオフ再試行する（30秒から再発行間隔まで）。失敗原因は公式エラーコードで区別し（接続不可=kabuステーション未起動 / API未有効、`4001007`・`4001017`=未ログイン（「APIを利用する」オンでも出る。案内はログイン状態の確認と再ログインに限定。issue #305）、`4001008`=API利用不可、`4001013`=APIパスワード不正）、ログと全ページ共通の市況データ接続バナー（`GET /system/marketdata-status`）で対処を示す（issue #295）。`4001007`・`4001017`が連続5回以上または5分以上続く間（`TokenStatus.Persistent`）、バナーは「再ログインしてから待つ」を促す強調表示（`MarketDataPersistentBanner`）になる。自動GUIログインはスコープ外で、メンテ明け・寄り前の再ログインは操作者が手作業で行い、続くときの確認項目は`requirements/non-functional.md` §3.1に従う（issue #712）。接続先は仕様どおり`localhost:18080`のまま（`localhost`は`::1`・`127.0.0.1`の双方を試行するためIPv4固定にはしない）
- **銘柄マスタ**: kabuステーションAPIには上場銘柄一覧の取得エンドポイントが無いため、スキャン対象ユニバース（`instruments`）は運用者が用意する銘柄マスタCSVから`bootstrap.Services.Start`が起動時に冪等にupsertする（`internal/bootstrap/universe`、手順は`environment/setup.md`「銘柄マスタの投入」）。PUSH購読・スキャンより前に実行する。
- **銘柄マスタの確認付き自動取得**（issue #508）: 有効な`stock`が無い間だけ、Scanner Dashboardの操作（`POST /scanner/universe/import`、`router.WithUniverseImporter`）でJPXの東証上場銘柄一覧（`data_j.xlsx`）を1回取得し、`internal/bootstrap/universe`の`Importer`/`ParseJPX`（excelize）が株式のみをCSVと同じ検証（`checkIdentity`）で1トランザクションにupsertする。起動時・定期の取得はしない。詳細・対象区分・失敗時の扱い・JPXの利用上の注意は`environment/setup.md`「銘柄マスタの投入」
- **銘柄登録・PUSH購読**: スキャン対象銘柄をkabuステーションAPIの銘柄登録エンドポイントに登録し、価格・板情報はPUSH WebSocket（kabuステーションが提供するローカルWebSocket）で受信する。これによりREST側の60秒ポーリングに依存せず、Feature Engineが各サイクル開始時点の最新スナップショットを参照できるようにする。既定（`scan.full_scan_enabled`が`true`でない場合）のPUSH登録はランキング監視（後述「ランキング方式のPUSH登録」・FR-SCHED-9）で、以下のユニバース先頭40件の登録・REST回転の記述は`scan.full_scan_enabled: true`（明示オプトイン）のときだけの挙動である。`full_scan_enabled: true`のとき、実装（`bootstrap.Services.Start` → `pushfeed.Feed.Run`（`internal/service/pushfeed`））は起動時（およびPUSH切断後の再接続時）にアクティブな`stock`銘柄を最大40件（kabuステーションAPIの登録上限50のうち`marketdata.RestRotationSlots`=10件をREST用に空ける）登録してPUSHを購読し、直近30秒以内のPUSH板があれば`market-data`ジョブはそれを使い、無ければREST `GetBoard`へフォールバックする。上限超過分・PUSH未着の銘柄はRESTポーリングのみで取得する（`full_scan_enabled: true`では登録対象は`symbol`昇順の先頭40件であり、候補銘柄・保有銘柄の優先はしない。約4,000銘柄ではほとんどがRESTで取得される。既定のランキング監視では登録対象は監視リスト（最大45件）で、保有・注文中は固定枠として優先される）。RESTの`GetBoard`/`GetSymbol`/`RegisterSymbols`は`marketdata/infolimit`のプロセス全体レート制限（既定8件/秒、公式10件/秒未満。`scan.kabu_info_api_max_per_second`）を共有し、`market-data`ワーカー1本の直列実行と合わせて公式上限を超えない（issue #514）。1サイクルでREST板を取れる件数と、取り切れない銘柄（同一サイクル継続・次tickスキップ）は`requirements/non-functional.md` §2.3。429/`4001006`は流量超過として待って再試行し、尽きたジョブは失敗にせず次サイクルへ回す。現在値が0/未取得の板（寄り付き前・未約定）は価格欠損として扱い、`market-data`ジョブは失敗させ（スナップショットを永続化しない）、保有ポジション監視は当該銘柄をスキップする。`execution.Engine`の`OnSnapshot`/`TryFillPending`/`Close`も0以下の価格を`ErrInvalidPrice`で拒否する
- **API登録銘柄リストの50件上限とRESTの回転**: kabuステーションは情報系API（`/board`・`/symbol`）で要求した銘柄を自動でAPI登録銘柄リストへ登録し、その上限はREST/PUSH合算で50銘柄（公式`kabu_STATION_API.yaml` tag `info`/`register`）。超過すると`4002006`（レジスト数エラー）で板が取れず、約4,000銘柄のスキャンが全件失敗する。そのため (1) `scan.full_scan_enabled: true`のとき`pushfeed.Feed.RegisterUniverse`は`PUT /unregister/all`で前回起動の登録（kabuステーションは再起動後も保持する）を空にしてからPUSH用に40件を登録し（既定のランキング監視は`UseWatchlist`により同じく`PUT /unregister/all`のあと監視リスト（最大45件）を登録する。後述）、(2) `marketdata.Client`（`rotation.go`）はRESTで登録された銘柄を記憶し、`GetBoard`/`GetSymbol`が`4002006`を返したらそれらを1回の`PUT /unregister`でまとめて解除して1回だけ再試行する（10銘柄に1回の追加呼び出し。PUSH銘柄は解除しない。`4001020`/`4001021`は解除済みとして扱う）。解除できる銘柄が無い（枠を他が占有している）場合は元の`4002006`を返す。`4002006`は銘柄単位の4xxで`market_data_down`の連続失敗には数えない
- **ランキング方式のPUSH登録（既定、FR-SCHED-9）**: `scan.full_scan_enabled`が`true`でない限り、`pushfeed.Feed`は`UseWatchlist`でユニバース先頭40件の登録をやめ、`bootstrap/rankingwatch`が毎分決める監視リスト（最大45件。保有・注文中は固定枠、入れ替え毎分最大5件・最低5分保持）を`SetWatch`で登録する。`PUT /register`で新リストを登録し、外れた銘柄は`PUT /unregister`（`marketdata.Client.UnregisterSymbols`。未登録の4001020/4001021は成功扱い）で枠を空ける。PUSH再接続時は`PUT /unregister/all`のあと直近の監視リストを再登録する。45件を超える分は無く、残り5件をREST（`/board`・`/symbol`）の回転用に空ける。`GET /ranking`は情報APIのプロセス全体リミッタを共有し、ランキングの価格は一切デコードせず銘柄コードだけを返す（`marketdata.Client.RankingSymbols`）。ランキングが空・失敗のサイクルは監視リストが保有・注文中だけになり、次サイクルで自動復帰する。
- **PUSH/RESTの役割分担とレート逼迫時の方針（issue #709）**: 板の主経路はPUSH、REST `/board`は**PUSH登録済み銘柄に対する薄い補完**に限る。`pushfeed.Feed.Latest`は直近30秒（`pushfeed.BoardMaxAge`）以内のPUSH板があればRESTを呼ばず、古い・未着のときだけ`GetBoard`へフォールバックする。この`Latest`は`market-data`ジョブと保有ポジション監視（`heldposition.Monitor`。保有・注文中は監視リストの固定枠＝PUSH登録済み。5〜15秒周期でも毎回RESTを叩かない）の共通経路である。PUSH対象外のユニバース全件を定期的にRESTで回すことは既定で行わない（60秒フルスキャンは`scan.full_scan_enabled: true`の明示オプトインのみ。FR-SCHED-7）。REST板取得の例外は、PUSH登録できない市場コンテキスト用の`market_index`/`sector_index`行（有効な指数行と監視銘柄の`sector`に一致するものだけ。件数はユニバース規模に比例しない）を毎サイクル取得する分だけである。**レート逼迫（429/`4001006`）への第一手段は流量の平準化（`scan.kabu_info_api_max_per_second`のプロセス全体リミッタ・429/`4001006`のbackoff再試行・尽きたジョブの次サイクル持ち越し。#514）であり、監視リスト件数（最大45件）・ランキング種別（7種別）・ユニバースを減らすことではない**（これらはレート対策では縮めない。縮める判断は#652の実機計測結果を踏まえて別途行う）。補完頻度（`BoardMaxAge`＝30秒、ランキング監視の60秒周期、`scan.kabu_info_api_max_per_second`）の数値は#652の計測結果が出たら見直せる暫定値で、計測待ちで方針自体は保留しない。
- **登録直後の初回板〜5秒は既知制限（現状許容、issue #713）**: 未登録銘柄の`/board`（PUSH登録直後で最初のPUSH板が届く前にRESTへフォールバックする場合を含む）は、kabuステーション側で約5秒かかる（#650の実測p50≒5,006ms、[kabusapi#656](https://github.com/kabucom/kabusapi/issues/656)）。ランキング監視＋PUSH前提では監視リストの入れ替えが毎分最大5銘柄に限られるため、この初回板待ちはkabu側の既知コストとして許容し、いまは最適化の対象にしない（`requirements/non-functional.md` §2.2）。**再検討のトリガー**: (1) 監視リストのchurn（入れ替え頻度）が高く（入れ替え上限に毎サイクル張り付く等）初回板待ちが実害になる、(2) 初回板待ちが`market-data`ワーカー（1本の直列処理）や候補更新・Jev Scoutのスループットを毀損する（`marketdatajob: slow market-data job`の`latest_ms`が常態化する等）、(3) #652の実機計測で未登録`/board`の遅延が変わった、のいずれか。
- **情報系APIの認証拒否のフェイルファスト**: 情報系APIが`401`/`4001009`を返すと再発行して1回再試行する（上記）。再発行したトークンでも拒否される（`/token`は成功するが情報系だけ`4001007`等）間は、認証サーキットブレーカー（`tokenrefresh.go`の`enterInfo`）が30秒間リクエストを送らず即座に同じ`APIError`を返し（約4,000銘柄を毎秒8件で空回りさせない）、30秒ごとに1件だけ再試行する。成功で解除する。この間`GET /system/marketdata-status`のバナーは`rejected`（`TokenStatus.Issue`、`/token`自体の失敗`TokenStatus`が優先）として、ログイン状態の確認・`/token`を呼ぶ他プロセス（二重起動・他のAPIツール）の有無の確認を案内する
- **約定可否の入力**（issue #511）: 特別気配は板の`BidSign`/`AskSign`（`0102`特別気配・`0108`停止前特別気配、`marketdata.Board.IsSpecialQuote`）、ストップ高/安と貸借は銘柄情報（`GET /symbol/{symbol}@{exchange}`の`UpperLimit`/`LowerLimit`/`MarginSell`、`marketdata.Client.GetSymbol`）から得て、`marketdatajob`が`market_snapshots.special_quote`/`price_limit`/`lendable`に保存する。銘柄情報は`symbolcache.Cache`が銘柄ごとに1営業日（JST）1回だけ取得し、取得失敗時はフラグを不明のまま（制限なし）スナップショットを保存して次サイクルで再取得する。
- **板の売/買命名**: kabuステーションAPIの`BidPrice`/`BidQty`は最良**売**気配、`AskPrice`/`AskQty`は最良**買**気配（トレーダー目線の命名で一般的なbid/askと逆。公式`BoardSuccess`のサンプルは`BidPrice 2408.5 > AskPrice 2407.5`）。`marketdata.Board`は生の名前を保持し、入れ替えは`internal/service/marketdata/quote`が単一定義として担う（`quote.Bid(board)`=`AskPrice`=最良買気配、`quote.Ask(board)`=`BidPrice`=最良売気配、`quote.SpreadBps(board)`は両者から`featureengine.SpreadBps`で算出し、どちらかが無い・板が逆転している場合はnil）。`marketdatajob.readingFromBoard`は`quote.Bid`/`quote.Ask`を`featureengine.Reading`（Bid=最良買気配/Ask=最良売気配）へ渡し、数量・板深度はその場で入れ替える（`BidQty=AskQty`、`AskQty=BidQty`、`BidDepth=Buy1..10`、`AskDepth=Sell1..10`）。保有ポジション監視（`heldposition.Monitor`）の一時スナップショットも同じ`quote`でBid/Ask/SpreadBpsを作るため、永続化される1分足とExit評価で約定価格の前提が一致する。これにより`spread_bps`は正常な板で0以上、`orderbook_imbalance`は買い数量優勢で正になり、スプレッド上限ガード（Fast Screener/Risk Engineの`max_spread_bps`）が機能する（issue #458）。修正前に保存された過去分の`market_snapshots`の扱いは`er/tables-market.md`参照
- **発注**: Paper Trading中はExecutionサービス内でシミュレーションのみ行い、kabuステーションAPIへは発注しない。Phase 7（実売買移行）で初めてkabuステーションAPIの注文エンドポイントを呼び出す。それまでは`KABU_API_PASSWORD`（単一キー）は市場データ読み取り専用であり、Production用とPaper Trading用のキー・Base URLの分離（`requirements/non-functional.md` §4）はPhase 7で注文エンドポイントを実装する前に行う
- **異常時**: kabuステーションAPI無応答・エラー時、`marketdata.StatusTracker`が銘柄ごとのstale状態を記録する（`GetBoard`失敗で`MarkStale`、成功で`MarkFresh`）。ただし現状の実装はこの記録を新規取引の可否判定に使わない（`StatusTracker.IsStale`の呼び出し元は無い）。銘柄単位のstale判定（`StatusTracker`）による新規取引禁止は未実装だが、スナップショット`Timestamp`の鮮度は立会中に`domain.MaxSnapshotAge`（3分。ランキング監視の60秒周期の3倍、市況コンテキスト（`marketcontext.Loader`）の指数バー・ブレッドス許容も同じ値を注入する（issue #692）。`scan.full_scan_enabled: true`では全件REST1周が約8分のため`scan.full_scan_max_snapshot_age_seconds`＝同梱620秒）で確認する（issue #685/#686）: `candidates.Refresher`は古い最新足の銘柄を`stale_snapshot`で除外してScannerには残しJev Scoutへ投入せず、Jev Scout/Trader（`HandleJob`）は古い最新足のジョブを再試行なしでスキップし、Paper Entryは`Enter`のセッション判定（足の時刻）に加え壁時計も立会中であることを要求する。Riskは鮮度を見ない。新規取引を止める安全装置として有効なのは、`GetBoard`のフィード側失敗（通信エラー・5xx・認証系4xx。銘柄単位の`4002001`等は除く）の5回連続で発動する全体の`market_data_down` Kill Switch（FR-RISK-7、`requirements/functional/components-pipeline.md`）のみで、他銘柄の取得成功でカウンタがリセットされるため、特定の1銘柄だけ取得できない状態は検知されない。銘柄単位の禁止はPhase 7（実売買）移行前に実装する（鮮度の閾値は取引時間帯を考慮して別途定める）
- **応答ボディの上限**: 応答ボディは`internal/httpbody`の`ReadAll`で4 MiB（`httpbody.DefaultMaxBytes`）まで読み、超過は`httpbody.ErrTooLarge`を含む読み取りエラー（`marketdata: read response body`）として失敗させる（`APIError`にもステータス判定にも進まない）
- **認証情報の入力経路**: `APIPassword`は`.env`/環境変数ではなく、アプリ内のSettings画面（`/settings`）から入力し、`secrets`テーブル（`internal/repository/system.SecretsRepository`、AES-256-GCMで暗号化）にDB保存する（issue #57）。必須2キー（JEV_API_KEY/KABU_API_PASSWORD）が未設定でもアプリは起動するが、Setup Guard（§10.5）が全ページを`/setup`へ誘導する。接続先URL・モデル名などの上書き用キー（JEV_BASE_URL/JEV_MODEL等）は任意で、Settings画面では接続先別のモーダル内に必須キーと並べて任意項目として表示する。空欄（未設定）は既定値を意味し、保存済みの上書き値は項目ごとの削除（`DELETE /settings/:key`）で既定値へ戻せる（issue #271・#272・#302）。`/setup`には必須2キーと任意のSLACK_WEBHOOK_URLだけを表示する。Jev/kabuステーションAPI依存機能はエラーログを出しつつ動作を継続する。入力はキー単位で保存・削除する（`POST`/`DELETE /settings/:key`、`internal/config`のallow-list外のキーは400）ため、あるキーの操作が他キーの値に影響することはない（issue #79）。設定変更はアプリ再起動後に反映される（ホットリロードは範囲外）

## 6. Jev API連携

- Jevアダプタ（`internal/service/jev`）はAPIキーをGoプロセス内のみで保持し、TypeSafe AI公式API（<https://docs.typesafe.ai/api>）を呼び出す。`Client`は`POST {BaseURL}/v1/systemone`（`Authorization: Bearer <APIキー>`）の1エンドポイントだけを使い、Scout/Traderとも同じエンドポイントに質問セットだけを変えて送る
- リクエストは`{"state": {"market": <ScoutState>, "similar_past_cases": <RAG文脈>}, "model": <モデル名。既定は"jev-latest">, "questions": {"<id>": {"type", "instructions", "criteria"}}}`。`market`は特徴量由来の状態（`schemas.go`の`ScoutState`）、`similar_past_cases`はRAGの類似過去事例（§7）で、質問文はこの2フィールドをバッククォートで参照する
- 応答は`{"model": "jev-1.13.0", "answers": {"<id>": {...}}, "usage": {"input_tokens", "output_tokens"}}`。`Client`が回答を既存のdomain入口型（`ScoutResponse`/`TraderResponse`）へ変換するため、呼び出し側（Scout/Trader/Policy/RAG/Calibration）はワイヤ形式を知らない。`ModelID`は応答の`model`。応答に課金額は無いため`RequestCost`はnilのまま（`jev_decisions.request_cost`はNULL）
- 質問は型付きで`internal/service/jev/questions.go`（Scout）・`questions_trader.go`（Trader）に定義する（`instructions`と`criteria`の文言はレビュー対象）。ワイヤ層（リクエスト型・応答の検証）は`internal/service/jev/systemone`に分離する

| 用途 | question id | type | 備考 |
|---|---|---|---|
| Scout | `interesting_now` / `liquidity_ok` / `abnormal_activity` | `noul` | 0〜1（yesの確率） |
| Scout | `momentum_quality` | `choice` | `weak`/`moderate`/`strong`/`exceptional` |
| Trader | `direction` | `choice` | `LONG`/`SHORT`/`NONE`。`TraderResponse.Confidence`はこの回答の`confidence`（FR-TRADER-2: 検証済み確率ではない） |
| Trader | `regime` | `choice` | `TREND`/`RANGE`/`BREAKOUT`/`CHAOTIC` |
| Trader | `entry_quality` | `choice` | `poor`/`fair`/`good`/`strong`/`exceptional` |
| Trader | `toxic_flow` / `liquidity_stressed` / `continuation_probability` | `noul` | 0〜1（yesの確率） |

- 応答は厳格に検証する。必須answerの欠落・`type`不一致・`choice`が定義外の値・`noul`/`confidence`が0〜1の範囲外は`systemone.ErrInvalidResponse`として再試行せず失敗させ、`jev_decisions`に保存しない
- 応答ボディは`internal/httpbody`の`ReadAll`で4 MiB（`httpbody.DefaultMaxBytes`）を上限に読む。超過は`httpbody.ErrTooLarge`となり、`systemone.ErrInvalidResponse`と同様に不正な応答として再試行せず即失敗させる（ステータスコードの判定より前に読むため、エラー応答のボディ（`APIError.Body`）にも同じ上限がかかる）
- `prompt_version.go` でプロンプト/質問セットのバージョンを管理し、`jev_decisions.question_version` に記録する（`architecture/er.md` 参照）。質問の文言・構成を変えたら必ず上げる（現行は`scout-v3`/`trader-v3`。`scout-v2`/`trader-v2`はRAG文脈に実結果が無いと案内していた旧プロンプト、v1は旧独自スキーマ）。版は記録・表示（Decision history/Activity）用で、Calibration集計（`calibration_outcomes`の指標・自己改善の日次分析）は`question_version`で分離せず全版の判断を混在して集計する（プロンプト改訂直後は旧版と新版の判断が同じ指標に混ざる）
- 失敗時の再試行: 通信エラーと5xxは1回目リトライ即時、2回目以降exponential backoff、継続失敗でnew entry停止。429（レート制限）と529（過負荷）も再試行するが、即時再試行はせず1回目リトライからbackoffする。401（キー不正）・422（スキーマ違反）・その他の4xx・不正な応答は再試行せず即失敗（`APIError`／`ErrInvalidResponse`）。実装（`client.go`の`defaultMaxAttempts`/`defaultRetryBaseDelay`、`evaluate.go`）は最大4試行（初回＋リトライ3回）、5xxの1回目リトライは即時で2回目・3回目リトライは500ms・1秒のバックオフ（429/529は1〜3回目リトライが500ms・1秒・2秒）、HTTPタイムアウトは1試行あたり5秒（全試行失敗時の最悪所要時間は5xxで21.5秒、429/529で23.5秒、`requirements/non-functional.md` §2.2）。失敗した呼び出し（不正応答を含む）もエラー率の集計（§5.2のJev APIエラー率・`Healthy`）に数える。既存ポジションはRisk Engine/Executionのコードベースルールで管理を継続する
- **認証情報の入力経路**: `APIKey`/`BaseURL`/`Model`は§5と同じくSettings画面（`/settings`）経由でDB保存する（issue #57）。必須は`APIKey`（`JEV_API_KEY`。<https://console.typesafe.ai>で発行したキー）のみで、`BaseURL`（`JEV_BASE_URL`）と`Model`（`JEV_MODEL`）は任意の上書き（Settings画面のJev接続先モーダル内の任意項目）
- **既定値**: 未設定・空文字の`BaseURL`は`jev.DefaultBaseURL`（`https://api.typesafe.ai`）、`Model`は`jev.DefaultModel`（`jev-latest`）になる（kabuステーションの`marketdata.DefaultBaseURL`と同じ方針）。保存済みの値は既定値より優先され、削除すると既定値へ戻る。`BaseURL`はホスト名のみ（`/v1/systemone`などのパスは付けない）。`Model`はリクエストの`model`としてScout/Traderの両方に送る。保存済みの既存値は移行処理なしでそのまま有効（issue #271・#274）

## 7. RAG連携（経験ベース文脈拡張）

`requirements/functional.md` §4.13 の実装詳細。

```mermaid
sequenceDiagram
    participant FE as Feature Engine
    participant RAG as RAG Context Builder
    participant VEC as sqlite-vec (jev_decision_vectors)
    participant JEV as Jev Adapter

    FE->>RAG: 現在の特徴量ベクトル（14次元、標準化済み）
    RAG->>VEC: embedding MATCH ? AND decision_id IN (calibration_outcomes紐付き済み ∩ 自己以外) ORDER BY distance LIMIT k
    RAG->>VEC: 不足時のみ embedding MATCH ? AND decision_id IN (自己以外) ORDER BY distance LIMIT k×4
    VEC-->>RAG: 類似jev_decision_id + distance
    RAG->>RAG: calibration_outcomesと結合し「方向・regime・future_return・was_direction_correct」を要約
    RAG->>JEV: few-shot文脈（類似局面の要約）+ 現在の状態
    JEV-->>RAG: Scout/Trader判断
```

- 埋め込みはLLM API呼び出しを伴わない標準化済み数値特徴量ベクトル（14次元、`architecture/er.md` ベクトルインデックス節参照）。追加のAPIコスト・レイテンシは発生しない（FR-RAG-3）
- `market_snapshots`保存時・`jev_decisions`保存時にそれぞれ`market_snapshot_vectors`/`jev_decision_vectors`（sqlite-vec仮想テーブル）へ同期書き込みする
- 検索は`jev_decision_vectors`に対し、まず`calibration_outcomes`が紐付いた判断だけを対象にした近傍検索（`decision_id IN (SELECT jev_decision_id FROM calibration_outcomes)`、最大k件）を行う。Scout判断はラベル付与不能で全候補×毎サイクル索引されるため、距離順の候補プールだけでは最近傍がScout判断で埋まり、紐付き済み判断が候補に入らない。紐付き済みがk件に満たない場合のみ、制限なしの距離順k×4件を追加で取得して補い（紐付き済みとの重複は除く）、`calibration_outcomes`が紐付いた判断を先頭に、次いで未付与のTrader判断、Scout判断の順（各群は距離順）に並べ替えて上位k件を採用し、不足分を`market_snapshot_vectors`の類似局面で補う（FR-RAG-2）。候補判断の本体は`jev_decisions`から`WHERE id IN (...)`の1クエリ、`calibration_outcomes`も1クエリで一括取得する（候補数に依らず固定クエリ数）
- 問い合わせ対象の状態自身は類似事例から除外する（`rag.Subject{Symbol, Timestamp}`、FR-RAG-2/4）。判断は同一銘柄で`timestamp`が現在時刻以降のもの（Trader呼び出し直前に保存された同一状態のScout判断を含む）、補充枠の`market_snapshot_vectors`は同一銘柄で現在時刻−15分（`SnapshotRecencyGuard`、特徴量ベクトルの最長ルックバック）より新しいスナップショットを除く。判断側に15分ガードは掛けない。sqlite-vecはKNN走査中のidに等価・`IN`制約しか使えず、「ほぼ全行の許可id集合」を渡すサブクエリは呼び出しごとに履歴全件を実体化する（10万行で約6倍遅い。issue #528）。そのため除外はSQLの制約にせず、KNNをk＋余裕分（`selfMargin`=32）で取得し、同一銘柄の該当行（判断は`symbol = ? AND timestamp >= ?`、スナップショットは`symbol = ? AND timestamp > ?`）をidで引いて落とし、上位k件に切り詰める。除外対象が余裕分を超えて残りがk件に満たない場合は取得件数を4倍ずつ広げて再検索する。ラベル付き判断への限定（`calibration_outcomes`紐付き）だけは小さな集合なのでSQLの`IN`制約のまま残す。`Symbol`が空のSubjectは何も除外しない
- 各事例は方向・confidence・regime（`response_json`由来）に加え、紐付き済みなら最短horizonの`horizon_minutes`・`future_return`（%単位、1.0=+1%）・`was_direction_correct`（NONE判断はnull）をJSONへ載せる。未付与の判断は方向・confidence・regimeのみ（FR-RAG-3）
- 質問文末尾の共通ガイド（`stateGuide`）は、実結果付きの事例を小標本の弱い文脈として扱い、`market`の現在の根拠を上回らせないようJevに指示する（`question_version`は`scout-v3`/`trader-v3`）
- コールドスタート期間（該当データが少ない）は空の検索結果として扱い、Jevは通常通り判断する（FR-RAG-4）。現在のスナップショット自身・直近の足・同一状態のScout判断は上記の自己除外で類似事例に返らないため、過去の蓄積が無い間は文脈が空になる

## 8. 自己改善ループ（Sol / Opus 連携）

`requirements/functional.md` §4.14 の実装詳細。Sol/Opusは高頻度の売買判断ループ（§4, §9）とは別の低頻度バッチとして動作し、Policy Engineのしきい値のみを対象に自己改善する。

```mermaid
sequenceDiagram
    participant SCHED as Scheduler（平日15:40 JST、引け後）
    participant SOL as Sol Adapter
    participant GOV as Self-Improvement Governor
    participant OPUS as Opus Adapter
    participant BT as Backtest Engine（§4.11再利用）
    participant DB as SQLite（runtime_settings, policy_proposals）
    participant SLACK as Slack Webhook

    SCHED->>SOL: 直近の負けトレード・Calibration指標を渡し分析依頼
    SOL-->>GOV: 改善提案（rationale + proposed_changes: policy.*キーのみ）
    GOV->>GOV: 提案の対象キー・変更幅を機械的に検証（FR-SELFIMPROVE-8。逸脱時はstatus=rejectedとして以降の処理をスキップ）
    GOV->>DB: policy_proposals挿入（status=pending）
    GOV->>BT: 直近20営業日相当のシャドーバックテスト実行（提案後しきい値）
    BT-->>GOV: Expectancy / Max Drawdown比較結果
    GOV->>GOV: 決定的しきい値判定（Expectancy非悪化 かつ Max Drawdown悪化が相対10%以内、FR-SELFIMPROVE-4）
    GOV->>OPUS: 提案 + シャドーバックテスト結果 + 決定的判定結果でレビュー依頼（Opus API、実際の外部AI呼び出し）
    OPUS-->>GOV: 定性レビュー結果（approve/reject, review_json）
    alt 決定的しきい値を満たす かつ Opus APIがapprove（FR-SELFIMPROVE-9）
        GOV->>DB: runtime_settings（policy.*）更新、policy_proposals.status=applied
        GOV->>SLACK: 適用を通知
        GOV->>GOV: 適用後5営業日相当のExpectancyを追跡
        opt 相対20%以上悪化（両窓にクローズ済みポジションがある場合のみ判定）
            GOV->>DB: 直前policy_versionへロールバック、policy_proposals.status=rolled_back
            GOV->>SLACK: ロールバックを通知
        end
    else 却下（決定的しきい値未達 または Opus APIがreject）
        GOV->>DB: policy_proposals.status=rejected
    end
```

- Solが変更を提案できる対象は`runtime_settings`の`policy.*`キーに限定する。`risk.*`キーとJevの`prompt_version`は`selfimprove`サービスに書き込みAPIそのものを持たせないことで技術的に強制する（§1 設計方針）
- Luna（Sense）は本ループとは独立し、高頻度側（Feature Engine/Jev呼び出しの前段）でニュース分類等を提供する補助コンポーネントとして`internal/service/assist/luna.go`に実装する
- **既定はJev（issue #273）**: Sol・Opus・Lunaは追加のキー入力なしで`JEV_API_KEY`（`JEV_BASE_URL`/`JEV_MODEL`も流用。既定`https://api.typesafe.ai`/`jev-latest`、§6）だけで動く。Jevは自由文を返さない契約（`noul`/`choice`）のため、各役は構造化質問のみを使う: **Sol**はしきい値変更の**候補生成をコード側**が行い（`policy.*`の各キーをFR-SELFIMPROVE-3の1ステップ上下、値域でクランプ、ラベル付きサンプルが0件の方向は除外）、Jevは1つの`choice`質問（`best_change`、選択肢=候補ID`<key>=<新値>`＋`none`）で最も有望な候補を選ぶ（`none`は提案なし。提案文・値はJevに生成させず、`rationale_json`もコードが`{"source":"jev",...}`として組み立てる。候補は常にFR-SELFIMPROVE-2/3の範囲内）。**Opus**は決定的しきい値（FR-SELFIMPROVE-4）を通過した提案についてのみ`adopt`の`noul`（採用してよい確率。0.5以上でapprove、未満はreject。Jevは却下側にしか効かず、決定的未達を覆せない）を問う。**Luna**は§13。呼び出しは`jev.Client.Ask`（Scout/Traderと同じ再試行・応答検証）で、補助役の失敗が`jev_api_down` Kill Switchを誘発しないよう、`Healthy`・Slackエラー率アラートの集計には算入しない。`JEV_API_KEY`が未設定ならこれらの役は無効（`assist.ErrNotConfigured`、当日スキップ）で、起動は失敗しない
- **役ごとの差し替え（任意）**: `SOL_BASE_URL`/`OPUS_BASE_URL`/`LUNA_BASE_URL`（とBearer用の`*_API_KEY`）をSettings画面のSol/Opus/Luna接続先モーダルで入力した役だけが、その外部AI API（下記契約）に切り替わる。未入力の役はJevにフォールバックする（役ごとに独立）。差し替え先の契約（`/v1/analyze`・`/v1/review`・`/v1/classify`）はモデル名を受け取らないため、モデル名の上書き項目は設けない（受け取る契約の役が現れるまで対象外）
- **差し替え時の外部AI API契約（`internal/service/assist`）**: いずれも`POST {BASE_URL}<path>`（`Authorization: Bearer {API_KEY}`、JSON、200以外はエラー扱い。再試行はJevと同じ分類で、通信エラー・5xx・429は最大4試行（429/529は1回目リトライからbackoff、それ以外は1回目リトライ即時）、401・422・その他の4xx・不正なJSON（`assist.ErrInvalidResponse`）・応答上限超過（`httpbody.ErrTooLarge`）は再試行せず即失敗）。Sol `/v1/analyze`（リクエスト: 方向別`thresholds`/`calibration`と`constraints`、レスポンス: `{"rationale":{...},"proposed_changes":[{"key","new_value"}]}`。変更なしは空配列）、Opus `/v1/review`（リクエスト: `proposal`/`backtest`/`deterministic`、レスポンス: `{"verdict":"approve|reject","reason":"..."}`）。決定的しきい値を満たさない提案ではOpus APIを呼ばずに却下する。`policy_proposals.review_json`は`verdict`/`approved`/`deterministic_passed`/`llm_reviewed`/`reason`等を含む。
- 未処理（`pending`）の提案、または適用が途中で失敗して`approved`のまま残った提案がある日は、Opusレビューの再試行／適用の再実行のみ行い新規のSol分析は行わない（同一しきい値への提案の競合防止）。`approved`の提案は日次バッチ冒頭（ロールバック判定より前）で`runtime_settings`へのSetを冪等（upsert）に再実行して`applied`へ収束させ、以後`applied`としてロールバック追跡の対象にする
- Slack通知（適用・ロールバック・AI段階スキップ）はbest-effort。DB更新の完了後に通知が失敗してもログ記録のみで、適用/ロールバックは成功として`DailyResult`（`Applied`/`RetriedApplied`/`RolledBack`）に反映する（再送はしない）
- **ロールバックの書き戻し（FR-SELFIMPROVE-6）**: 各キーは`runtime_settings`の現在値がその提案の適用値（NewValue）のままの場合のみOldValueへ戻す。後続提案・手動変更で値が変わっていれば上書きせず、`rolled_back_reason`にそのキー名を残す。OldValueが直前のロールバック済み提案の適用値なら、その提案の適用前の値まで遡る
- **`trade_signals.policy_version`**: `RuntimePolicy.AppliedPolicyVersion`（最も新しく適用された`status=applied`提案の`applied_policy_version`、無ければ空）をPolicy Engineが`policy-v1+sol-12`の形で記録する（`varchar(20)`に収まる）。ロールバック後は直前の適用版、無ければ`policy-v1`に戻る
- **ロールバック判定の境界（FR-SELFIMPROVE-6）**: 適用前/適用後の5営業日窓それぞれでクローズ済みポジションの`realized_pnl`平均（Expectancy）を求め、`post < pre`かつ`pre - post >= |pre| × 0.20`のときだけロールバックする（適用前が負でも`|pre|`基準。`pre == 0`では`post < 0`のみ。`post >= pre`では常に非ロールバック）。どちらかの窓にクローズ済みポジションが0件なら判定不能としてロールバックせず、`status=applied`のまま追跡窓の終了後も打ち切らず日次実行ごとに再評価する（`internal/service/selfimprove/rollback.go`）
- Sol/Opusはいずれも既定でJev（上記）に問い合わせ、`SOL_*`/`OPUS_*`を入力した役のみ`internal/service/assist`のHTTPクライアントを介した外部AI API呼び出しに切り替わる（Jevアダプタ（§6）と同様の認証情報の入力経路（Settings画面→`secrets`テーブル）とリトライ/exponential backoff方針に従う）。API失敗時は当該日のSol提案生成/Opusレビューをスキップし、Slack通知のうえ翌営業日に再試行する（銘柄単位の売買判断ではないためnew entry停止のような取引影響は発生しない）
- **シャドーバックテストの再現範囲**: `BT`（バックテストエンジン）は`market_snapshots`と`jev_decisions`をPolicy Engineで再生するが、Exitは固定Stop Loss/Take Profit/最大保有時間のみ（Trailing Stop・Jev方向反転・continuation_probability低下・VWAP逆クロス・引け前強制決済は未評価）、Risk Engineは適用せず（max_open_positions・同方向ポジション上限・市場逆行・日次損失上限・連敗上限・クールダウン・サイジングによる抑制は再現しない）、Jev判断は1件につき高々1回のエントリーにのみ使い（Exit後の再利用なし。保有足の無いエントリーは取引に計上しない）、約定はPaper Tradingと同じ約定モデル`fillmodel.Default`（呼値単位・スプレッド・滑りザラ場2bps/寄り引け5bps・手数料0bps・昼休みは約定しない・寄り引けは板寄せの別約定。FR-ENTRY-8）で価格付けする（`requirements/functional/components-platform.md` FR-BT-4）。決定的しきい値判定（FR-SELFIMPROVE-4）のExpectancy/Max Drawdownはこの前提での既存/提案後しきい値の相対比較であり、ライブ運用の絶対値ではない

## 9. Wails統合（デスクトップシェル）

```mermaid
graph TD
    subgraph Process["単一Goプロセス（Wailsアプリ）"]
        WV["WebView2 (ネイティブウィンドウ)"]
        AS["Wails AssetServer.Handler = Gin Engine"]
        WSL["WebSocket専用ループバックリスナー\n(Windowsのみ・router.WebSocketOnly・/ws/... のみ)"]
        GIN["Gin Router\n(SSR: Templ/HTMX, API: Huma)"]
        SCHED["自前Worker / Scheduler"]
        SVC["各Service（marketdata/featureengine/screener/jev/rag/policy/risk/execution/calibration/selfimprove）"]
        TRAY["ネイティブ通知（cmd/desktop/notify.go）"]
    end
    WV <--> AS
    WV <-.ws://wails.localhost:port (ws-base).-> WSL
    AS --> GIN
    WSL --> GIN
    GIN --> SVC
    SCHED --> SVC
    SVC -.Kill Switch発動時.-> TRAY
    SVC --> SQLITE[("SQLite（アプリ内蔵ファイル）")]
```

- Wails v2 の `options.App.AssetServer.Handler` に Gin の `http.Handler` をそのまま渡し、WebViewは常に `http://wails.localhost/` 相当の内部プロトコル経由でGinが返すHTML/HTMXフラグメント/静的アセットを描画する。外部ネットワークポートを開かない（`requirements/non-functional.md` §4 セキュリティに整合）。例外として、Wails AssetServerはWebSocketを扱えずWebView2が`ws://`をネットワークへ直接送るため、Windows版は`/ws/...`のUpgradeだけを受けるループバック専用リスナー（`127.0.0.1`と`[::1]`のランダムポート、`router.WebSocketOnly`、アドレスは`<meta name="ws-base">`でLitへ伝える）を別に開く。WebViewの接続先はAssetServerと、このWebSocket専用リスナーの2つである（`api/endpoints.md` §6、`components/runtime.md` §7）
- Risk EngineがKill Switchを発動した際は、同一プロセス内であるためネットワーク越しの通知APIを介さず、`cmd/desktop/notify.go`の`App`（`risk.Notifier`実装）が直接Wailsランタイム（`runtime.SendNotification` / `runtime.EventsEmit`）を呼び出してOSレベルのトースト通知とウィンドウ内インジケータ用イベントを発生させる。OSシステムトレイのアイコン変化は未対応（Wails v2にトレイAPIが無く、Wails v3または外部systrayが必要）
- Windowsログイン時の自動起動とクラッシュ時の自動再起動は、インストーラーがスタートアップフォルダへ作成する`pitha-trador.exe --supervise`のショートカットで実現する。`--supervise`付きで起動したプロセスは`internal/supervisor`により自身（`--supervise`なし）を子プロセスとして起動・監視し、非0終了/killでは指数バックオフ付きで再起動、終了コード0（操作者の終了・自動更新）では監視を終了する。ログはWailsアプリと同じ日次JSONログへ追記する（`requirements/non-functional.md` §3）
- SQLiteファイルはWailsアプリの起動時に存在確認・マイグレーション適用を行う。Postgresのような別プロセスの起動待ち合わせは不要
- ヘッドレス運用（例: CI・テスト環境）向けに、`cmd/desktop`とは別に`cmd/server`（Wailsを使わずGinのみを`net/http`でリッスンするエントリーポイント。`PITHA_SERVER_ADDR`で待受アドレスを指定、既定はループバック）が実在する。`internal/router`はWailsに依存しない形で実装しており、`cmd/server`ではページ自身のoriginでWebSocketも扱うため`ws-base`リスナーは持たない
- 自動アップデート（`internal/service/updater`、`cmd/desktop`のみ）は検知結果を`Checker.Status()`（最終確認時刻・新バージョン有無・安全ゲート保留とその条件種別`BlockedKind`・インストーラー準備済み・直近エラーとその種別`ErrorKind`。UIは種別を文言化し、生のエラー文言は表示しない）として保持し、`bootstrap.Services.Updater`→`router.WithUpdateController`経由でHandlerへ渡す。UIはHeaderの`UpdateBanner`で新バージョンと保留状態を通知し、Settings画面の「今すぐアップデートを確認」（`POST /system/update-check`）でスケジューラーと同じ`CheckForUpdate`を手動実行できる。`CheckForUpdate`は排他制御され、周期実行と手動実行が同時にインストーラーをダウンロード/終了要求することはない。`cmd/server`はアップデーター未搭載のためバナー/パネルは空、確認ルートは404（issue #76）。取得経路はリクエストごとの`context`タイムアウト（リリース確認30秒・ダウンロード全体10分）、ダウンロードサイズ上限（インストーラー512MiB・`checksums.txt`1MiB、GitHubの`asset.size`報告値があればそれに厳格化）、`browser_download_url`が`https://github.com/<owner>/<repo>/releases/download/`配下であることの検証で保護する（issue #126）。`checksums.txt`はインストーラーと同一リリース由来のためSHA256照合だけでは転送破損しか検知できず、リリース差し替えに対する真正性は`checksums.txt.sig`（`checksums.txt`のed25519分離署名、base64）で検証する（issue #376）。署名鍵はGitHub Actionsシークレット`RELEASE_SIGNING_KEY`、検証用公開鍵はリポジトリ変数`RELEASE_SIGNING_PUBLIC_KEY`から`-ldflags`で`internal/version.ReleasePublicKey`へ埋め込み、`updater.Config.PublicKey`で上書きできる。公開鍵を埋め込んだビルドでは、署名アセット欠落・形式不正・署名不一致を`ErrorVerification`（Permanent、再試行しない）として扱い、インストーラーをダウンロードせず一時ファイルも削除する。公開鍵が空のビルド（鍵未発行の環境）では署名検証を行わずWarnログを出して従来のSHA256照合のみとなる既知制約があり、鍵の発行手順は`docs/environment/setup.md`に従う。Authenticodeコード署名は未導入
- 自動アップデートのインストーラー（`os.TempDir()`配下の`pitha-trador-update-*`）は、更新後にアプリを終了しインストーラーを切り離して起動するため実行中に削除できない。次回の`cmd/desktop`起動時に`updater/tempcleanup.CleanupStale`が起動時刻より古い同名ディレクトリを削除する（失敗はログのみで起動を妨げない）。
- 自動アップデートの周期確認はスケジューラー起動直後に1回＋`@every 6h`。起動直後の取得失敗（ネットワーク未接続・GitHub APIレート制限等）と、新版検知後の安全ゲート保留（`Status.Blocked`）は、次の6時間周期を待たず指数バックオフ（1分から倍々、上限1時間）で再試行し、失敗も保留も無くなった時点（最新である／インストーラー検証済み）で止まる。ただし、リリース情報の内容不正・アセット検証失敗・リリース情報取得への拒否（401/403）・アセット取得への拒否（401/403/404。`ErrorRelease`/`ErrorVerification`/`ErrorAccess`。エラーの`Permanent()`で判別）は自然に直らないため即時再試行せず、次の6時間周期に委ねる（インストーラー全体の再ダウンロードの繰り返しを避ける、issue #259）。Settings画面の「自動で再試行します」系の文言は手動確認後にも成り立つ「6時間ごとの定期確認」を案内する（issue #258）。リリース取得はリポジトリが公開である前提で認証なしに行い（アセットは`browser_download_url`を直接取得）、リリース取得の404（リリース未公開、または非公開リポジトリ等でアクセス不可）はエラーではなく`Status.NoRelease`として成功扱いにし（Infoログのみ・バックオフ再試行なし。Settings画面には「公開されているリリースが見つかりませんでした（リリースが未公開か、リポジトリにアクセスできません）」と表示、issue #296）、リリース取得の401/403とアセット取得の401/403/404は`ErrorAccess`（「リリースにアクセスできない」という事実のみを案内し、再試行しない）として「内容不正」（`ErrorRelease`）と区別する。`Retry-After`付き403・429はレート制限（`ErrorRateLimit`、一時エラー）として扱い再試行する（issue #265）。再試行中は周期実行をスキップして二重実行しない（`internal/service/scheduler/updatecheck`、issue #240）。`dev`ビルド（`internal/version.Version`がsemverでないブランチ/PRビルド）は確認自体をスキップし、Settings画面にその旨を表示する
- `config/strategy.yaml`・`config/risk.yaml`・`/static/...`で配信する静的アセット（`static/src/dist`のesbuild/Tailwindビルド出力＋`static/src/vendor`のhtmx.min.js）は、いずれも`go:embed`でバイナリに埋め込み、`wails build`/`go build ./cmd/server`が生成する単一`.exe`だけで（外部ファイル・ソースツリー一切無しに）起動できる。config 2種は`internal/bootstrap.Run`が (1) 明示パス指定 (2) `PITHA_STRATEGY_PATH`/`PITHA_RISK_PATH`環境変数 (3) 実行ファイルと同じディレクトリの`config/*.yaml`（`os.Executable()`基準。配布先で手編集する運用向け） (4) 埋め込み既定値、の優先順位で解決する（issue #59）。静的アセットは`internal/router.New`が常に埋め込みから配信する
- `make dev`実行時は`Makefile`が`PITHA_STRATEGY_PATH`/`PITHA_RISK_PATH`をリポジトリ内の生ファイルへ設定するため、上記(2)が常に選ばれ、`config/risk.yaml`等を編集して再起動すれば即座に反映される（埋め込みはコンパイル時スナップショットのため、(4)経由では反映されない）

## 12. System Activity Feed連携

`requirements/functional.md` §4.15/§5.5の実装詳細。既存テーブル（`jobs`, `jev_decisions`, `kill_switch_events`）への読み取り専用集約であり、新規の永続テーブル・マイグレーションは追加しない。

- `internal/service/activityfeed`が`internal/repository`配下の`jobqueue`（`job_repo`）・`judgement`（`decision_repo`）・`system`（`killswitch_repo`）を横断的に参照し、キュー別集計（pending/running/直近failed件数。直近は過去1時間固定）と時刻順マージ済みイベント一覧を組み立てる
- `internal/web/handler/activity`に`activity.go`を追加し、`GET /api/v1/activity`（`api/endpoints.md` §5）と`/ws/activity`（同§6）を提供する
- 新規イベント（`jobs`の状態遷移、`jev_decisions`挿入、`kill_switch_events`挿入）はrepository層のコミット直後にactivityfeedのイベントバス（プロセス内channel、DB永続化なし）へ通知し、`/ws/activity`購読者へ配信する。プロセス再起動時はイベントバスの未配信分は破棄され、次回`GET /api/v1/activity`のスナップショットから再開する（監査要件はjobs/jev_decisions/kill_switch_events自体が引き続き担う）
- フィード件数上限（既定200、最大500）はAPI/WS配信側の制限であり、参照元テーブルの保持期間・行数（`architecture/er.md`各テーブルの運用注記）には影響しない

## 13. Luna ニュース分類・News Ingest連携

`requirements/functional.md` §4.16（FR-LUNA-1〜5）の実装詳細。

```mermaid
sequenceDiagram
    participant NF as News Ingest (internal/service/newsfeed)
    participant FEED as ニュースフィード（既定: やのしんTDnet WebAPI）
    participant LUNA as Luna Adapter (internal/service/assist/luna.go)
    participant CACHE as インメモリキャッシュ（直近N件、TTL付き）
    participant SCOUT as Jev Scout

    loop 定期ポーリング
        NF->>FEED: 対象銘柄（Fast Screener候補＋保有銘柄、立会時間中のみ、並列度上限4）の適時開示を銘柄単位で取得（失敗時はスキップ＋バックオフ）
        FEED-->>NF: 見出し・本文（内部契約へ正規化済み）
        NF->>LUNA: ニュース本文（既定Jev、差し替え時はLuna API）
        LUNA-->>NF: sentiment / event_type / summary
        NF->>CACHE: 銘柄別に格納（TTL経過分は破棄）
    end
    SCOUT->>CACHE: 対象銘柄のnews_context取得
    CACHE-->>SCOUT: 直近sentiment/event_type/summary（該当なしは空）
    SCOUT->>SCOUT: jev_decisions.state_jsonへnews_contextとして注入
```

- 永続化は`jev_decisions.state_json`（既存カラム）のみを利用し、新規テーブル・マイグレーションは追加しない（キャッシュはプロセスメモリ内のみでDB非永続）
- **認証情報の入力経路**: `LUNA_API_KEY`/`LUNA_BASE_URL`（Jevへの差し替え）、`NEWS_FEED_URL`/`NEWS_FEED_API_KEY`/`NEWS_FEED_ENABLED`（既定フィードの差し替え・停止）は§5・§6と同じくSettings画面（`/settings`）経由で`secrets`テーブルへ保存する。いずれも任意で、未入力なら既定（Luna=Jev、フィード=やのしん）で動く（issue #273）
- **既定のニュースフィード = やのしんTDnet WebAPI（issue #273）**: 非公式の適時開示インデックス（<https://webapi.yanoshin.jp/tdnet/>、APIキー不要）を`GET https://webapi.yanoshin.jp/webapi/tdnet/list/{銘柄コード}.json?limit=10`で**監視中の銘柄（News Ingestの対象定義＝Fast Screener候補＋保有）のみ**・銘柄単位（既存の並列度上限4）で取得する。`internal/service/newsfeed/yanoshin.go`のアダプタが応答を内部契約`{"items":[{"id","headline","body","published_at"}]}`へ正規化する: `items[].Tdnet.id`（`TDnet`表記・ネスト無しの`json2`形式も受理）→`id`、`title`→`headline`、`document_url`→`body`（`TDnet 適時開示: <URL>`。開示PDFの本文は取得せず、リンクのみ）、`pubdate`（`2026-10-05 15:30:00`、JST）→`published_at`（UTCへ正規化）。銘柄はリクエストの銘柄を優先し`company_code`（5桁）は使わない。`release.tdnet.info`のスクレイピングやPDFのダウンロードは行わない。明示的なレート制限は無い（やのしんの`llms.txt`）が節度を守り、1分周期・銘柄単位の重複排除・直近10件までの取得に留める
- **ニュースフィードの切り替え**: `NEWS_FEED_URL`を入力すると下記の汎用契約のフィード（ユーザー指定の取得先）に差し替わる。`NEWS_FEED_ENABLED`に`off`を保存するとNews Ingestを起動しない（未設定・`on`は有効。再起動後に反映）
- **汎用フィード契約（`NEWS_FEED_URL`）**: ニュースフィードは`GET {NEWS_FEED_URL}?symbol={銘柄コード}`（`Authorization: Bearer {NEWS_FEED_API_KEY}`）で`{"items":[{"id","headline","body","published_at"}]}`を返す。Luna差し替え時は`POST {LUNA_BASE_URL}/v1/classify`（リクエスト: `symbol`/`headline`/`body`/`published_at`、レスポンス: `{"sentiment":"bullish|bearish|neutral","event_type":"決算|業績修正|M&A|規制|その他","summary":"..."}`）。値域外の応答は失敗として扱う
- News Ingestは1分周期でポーリングし、記事ID（無ければ見出し）単位で重複排除して1件ずつLunaへ送信する。キャッシュは銘柄ごと直近5件・TTL 6時間（公開時刻基準）。新規に分類された記事は銘柄単位のニュースフラグを立て、Fast Screener候補であればイベント再評価（FR-SCAN-1）で1回だけ消費される
- 既定ではJevキーだけでLuna（Jev）とやのしんが有効になりNews Ingestが動く。Jevが使えず`LUNA_BASE_URL`も無い場合、または`NEWS_FEED_ENABLED=off`の間はNews Ingestを起動しない（`news_context`は注入されずニュースフラグも立たない。`Services`自体は構築され起動は失敗しない）。Sol/Opusは、Jevも`SOL_*`/`OPUS_*`も使えない間は各段階を毎日スキップする
- Lunaの既定（Jev）は`choice`質問2つ: `sentiment`（`bullish`/`bearish`/`neutral`）と`event_type`（`決算`/`業績修正`/`M&A`/`規制`/`その他`）。Jevは自由文を返さないため`summary`は見出し（コード側）とする
- **フェイルセーフ（非公式サービス前提、issue #273）**: やのしんの停止・遅延・タイムアウトがFast Screener・Jev・発注フロー・起動に波及しない。(1) 取得のHTTPタイムアウトは5秒（汎用フィードは15秒）。(2) 取得失敗はニュースフラグを立てずその銘柄をスキップする（FR-LUNA-4）。(3) `newsfeed.GuardedFeed`が失敗後に問い合わせを止めるバックオフ（2分から倍々、上限30分。1回の取得で同時に失敗した複数銘柄は1回の失敗として数える。成功で解除）を掛け、連続失敗中は問い合わせ頻度を落とす。(4) 起動はやのしんの到達性に依存しない（ポーリングはバックグラウンドgoroutine）。(5) エラーはアプリのログ（slogの`ERROR`/`WARN`。Settings画面の「エラーログ」から出力できる、FR-ERRLOG-1）に出し、System Activity Feed（§12）にも`news_feed`イベント（`newsfeed.WithErrorObserver`→`activityfeed.ObserveNewsFeedError`。直近50件のインメモリのみで、新規テーブルは追加しない）として流す。バックオフ中のスキップはログ・イベントとも出さない（連続失敗の通知は`WARN`の1行）
- Luna/News Ingest API失敗時はニュースフラグを立てず、Fast Screener/Jevの通常フローに影響を与えない（FR-LUNA-4、Jevと同様のフェイルセーフ）
