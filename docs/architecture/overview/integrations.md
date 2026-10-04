# アーキテクチャ設計: 外部・内部連携（§5〜§9・§12〜§13）

`docs/architecture/overview.md` から分割した章。§5 kabuステーションAPI連携 / §6 Jev API連携 / §7 RAG連携 / §8 自己改善ループ / §9 Wails統合 / §12 System Activity Feed連携 / §13 Luna ニュース分類・News Ingest連携。節番号は分割前と同一で、コードコメント等の `overview.md §<番号>` は本ファイルの同番号の節を指す。

## 5. kabuステーションAPI連携

- kabuステーションは三菱UFJ eスマート証券（旧auカブコム証券）が提供するWindows常駐アプリで、`http://localhost:18080`（既定）にローカルRESTを公開する。Go側の `internal/service/marketdata` はこれをHTTPクライアントでラップする
- **トークン発行**: アプリ起動時に `/kabusapi/token` へAPIパスワードでPOSTしトークンを取得。トークンは有効期限があるため、Wailsアプリ起動時および定期的に再発行し、メモリ上にのみ保持する（ディスクへは保存しない）。初回発行に失敗してもアプリは継続起動し（kabuステーション未起動の開発機でも他機能を使えるようにする）、トークンを保持するまでバックグラウンドで指数バックオフ再試行する（30秒から再発行間隔まで）。失敗原因は公式エラーコードで区別し（接続不可=kabuステーション未起動 / API未有効、`4001007`・`4001017`=未ログイン（「APIを利用する」オンでも出る。案内はログイン状態の確認と再ログインに限定。issue #305）、`4001008`=API利用不可、`4001013`=APIパスワード不正）、ログと全ページ共通の市況データ接続バナー（`GET /system/marketdata-status`）で対処を示す（issue #295）。接続先は仕様どおり`localhost:18080`のまま（`localhost`は`::1`・`127.0.0.1`の双方を試行するためIPv4固定にはしない）
- **銘柄マスタ**: kabuステーションAPIには上場銘柄一覧の取得エンドポイントが無いため、スキャン対象ユニバース（`instruments`）は運用者が用意する銘柄マスタCSVから`bootstrap.Services.Start`が起動時に冪等にupsertする（`internal/bootstrap/universe`、手順は`environment/setup.md`「銘柄マスタの投入」）。PUSH購読・スキャンより前に実行する。
- **銘柄登録・PUSH購読**: スキャン対象銘柄をkabuステーションAPIの銘柄登録エンドポイントに登録し、価格・板情報はPUSH WebSocket（kabuステーションが提供するローカルWebSocket）で受信する。これによりREST側の60秒ポーリングに依存せず、Feature Engineが各サイクル開始時点の最新スナップショットを参照できるようにする。実装（`bootstrap.Services.Start` → `pushfeed.Feed.Run`（`internal/service/pushfeed`））は起動時（およびPUSH切断後の再接続時）にアクティブな`stock`銘柄を最大50件（kabuステーションAPIの登録上限）登録してPUSHを購読し、直近30秒以内のPUSH板があれば`market-data`ジョブはそれを使い、無ければREST `GetBoard`へフォールバックする。上限超過分・PUSH未着の銘柄はRESTポーリングのみで取得する（登録対象は`symbol`昇順の先頭50件であり、候補銘柄・保有銘柄の優先はしない。約4,000銘柄ではほとんどがRESTで取得される。RESTの`GetBoard`は`market-data`ワーカー1本から直列に呼ばれるため並列度1で、別途のレート制御は設けない。1サイクルの所要時間の上限と実測値は`requirements/non-functional.md` §2.3）。現在値が0/未取得の板（寄り付き前・未約定）は価格欠損として扱い、`market-data`ジョブは失敗させ（スナップショットを永続化しない）、保有ポジション監視は当該銘柄をスキップする。`execution.Engine`の`OnSnapshot`/`TryFillPending`/`Close`も0以下の価格を`ErrInvalidPrice`で拒否する
- **発注**: Paper Trading中はExecutionサービス内でシミュレーションのみ行い、kabuステーションAPIへは発注しない。Phase 7（実売買移行）で初めてkabuステーションAPIの注文エンドポイントを呼び出す。それまでは`KABU_API_PASSWORD`（単一キー）は市場データ読み取り専用であり、Production用とPaper Trading用のキー・Base URLの分離（`requirements/non-functional.md` §4）はPhase 7で注文エンドポイントを実装する前に行う
- **異常時**: kabuステーションAPI無応答・エラー時、`marketdata.StatusTracker`が銘柄ごとのstale状態を記録する（`GetBoard`失敗で`MarkStale`、成功で`MarkFresh`）。ただし現状の実装はこの記録を新規取引の可否判定に使わない（`StatusTracker.IsStale`の呼び出し元は無い）。銘柄単位のstale判定による新規取引禁止、およびスナップショット`Timestamp`の鮮度チェックは未実装で、Fast Screener・Scout・Policy・Riskは最新スナップショットを鮮度確認なしで使う。新規取引を止める安全装置として有効なのは、`GetBoard`の5回連続失敗で発動する全体の`market_data_down` Kill Switch（FR-RISK-7、`requirements/functional/components-pipeline.md`）のみで、他銘柄の取得成功でカウンタがリセットされるため、特定の1銘柄だけ取得できない状態は検知されない。銘柄単位の禁止はPhase 7（実売買）移行前に実装する（鮮度の閾値は取引時間帯を考慮して別途定める）
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
- `prompt_version.go` でプロンプト/質問セットのバージョンを管理し、`jev_decisions.question_version` に記録する（`architecture/er.md` 参照）。質問の文言・構成を変えたら必ず上げる（現行は`scout-v2`/`trader-v2`。v1は旧独自スキーマで、Calibrationのコホートを分離するため別バージョンとして扱う）
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
    RAG->>VEC: embedding MATCH ? ORDER BY distance LIMIT 5
    VEC-->>RAG: 類似jev_decision_id + distance
    RAG->>RAG: calibration_outcomesと結合し「方向・regime・future_return・was_direction_correct」を要約
    RAG->>JEV: few-shot文脈（類似局面の要約）+ 現在の状態
    JEV-->>RAG: Scout/Trader判断
```

- 埋め込みはLLM API呼び出しを伴わない標準化済み数値特徴量ベクトル（14次元、`architecture/er.md` ベクトルインデックス節参照）。追加のAPIコスト・レイテンシは発生しない（FR-RAG-3）
- `market_snapshots`保存時・`jev_decisions`保存時にそれぞれ`market_snapshot_vectors`/`jev_decision_vectors`（sqlite-vec仮想テーブル）へ同期書き込みする
- コールドスタート期間（該当データが少ない）は空の検索結果として扱い、Jevは通常通り判断する（FR-RAG-4）

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
        opt 相対20%以上悪化
            GOV->>DB: 直前policy_versionへロールバック、policy_proposals.status=rolled_back
            GOV->>SLACK: ロールバックを通知
        end
    else 却下（決定的しきい値未達 または Opus APIがreject）
        GOV->>DB: policy_proposals.status=rejected
    end
```

- Solが変更を提案できる対象は`runtime_settings`の`policy.*`キーに限定する。`risk.*`キーとJevの`prompt_version`は`selfimprove`サービスに書き込みAPIそのものを持たせないことで技術的に強制する（§1 設計方針）
- Luna（Sense）は本ループとは独立し、高頻度側（Feature Engine/Jev呼び出しの前段）でニュース分類等を提供する補助コンポーネントとして`internal/service/assist/luna.go`に実装する
- **外部AI API契約（`internal/service/assist`）**: いずれも`POST {BASE_URL}<path>`（`Authorization: Bearer {API_KEY}`、JSON、200以外はエラー扱い）。Sol `/v1/analyze`（リクエスト: 方向別`thresholds`/`calibration`と`constraints`、レスポンス: `{"rationale":{...},"proposed_changes":[{"key","new_value"}]}`。変更なしは空配列）、Opus `/v1/review`（リクエスト: `proposal`/`backtest`/`deterministic`、レスポンス: `{"verdict":"approve|reject","reason":"..."}`）。決定的しきい値を満たさない提案ではOpus APIを呼ばずに却下する。`policy_proposals.review_json`は`verdict`/`approved`/`deterministic_passed`/`llm_reviewed`/`reason`等を含む。
- 未処理（`pending`）の提案がある日は、Opusレビューの再試行のみ行い新規のSol分析は行わない（同一しきい値への提案の競合防止）
- Sol/Opusはいずれも`internal/service/assist`のHTTPクライアントを介し、Jevアダプタ（§6）と同様の認証情報の入力経路（Settings画面→`secrets`テーブル、`SOL_API_KEY`/`SOL_BASE_URL`、`OPUS_API_KEY`/`OPUS_BASE_URL`）とリトライ/exponential backoff方針に従う実際の外部AI API呼び出しとして実装する。API失敗時は当該日のSol提案生成/Opusレビューをスキップし、Slack通知のうえ翌営業日に再試行する（銘柄単位の売買判断ではないためnew entry停止のような取引影響は発生しない）
- **シャドーバックテストの再現範囲**: `BT`（バックテストエンジン）は`market_snapshots`と`jev_decisions`をPolicy Engineで再生するが、Exitは固定Stop Loss/Take Profit/最大保有時間のみ（Trailing Stop・Jev方向反転・continuation_probability低下・VWAP逆クロス・引け前強制決済は未評価）、Risk Engineは適用せず（max_open_positions・日次損失上限・連敗上限・クールダウン・サイジングによる抑制は再現しない）、コストはスリッページ片道5bps・手数料0bpsで固定する（`requirements/functional/components-platform.md` FR-BT-4）。決定的しきい値判定（FR-SELFIMPROVE-4）のExpectancy/Max Drawdownはこの前提での既存/提案後しきい値の相対比較であり、ライブ運用の絶対値ではない

## 9. Wails統合（デスクトップシェル）

```mermaid
graph TD
    subgraph Process["単一Goプロセス（Wailsアプリ）"]
        WV["WebView2 (ネイティブウィンドウ)"]
        AS["Wails AssetServer.Handler = Gin Engine"]
        GIN["Gin Router\n(SSR: Templ/HTMX, API: Huma)"]
        SCHED["自前Worker / Scheduler"]
        SVC["各Service（marketdata/featureengine/screener/jev/rag/policy/risk/execution/calibration/selfimprove）"]
        TRAY["ネイティブ通知（cmd/desktop/notify.go）"]
    end
    WV <--> AS
    AS --> GIN
    GIN --> SVC
    SCHED --> SVC
    SVC -.Kill Switch発動時.-> TRAY
    SVC --> SQLITE[("SQLite（アプリ内蔵ファイル）")]
```

- Wails v2 の `options.App.AssetServer.Handler` に Gin の `http.Handler` をそのまま渡し、WebViewは常に `http://wails.localhost/` 相当の内部プロトコル経由でGinが返すHTML/HTMXフラグメント/静的アセットを描画する。外部ネットワークポートを開かない（`requirements/non-functional.md` §4 セキュリティに整合）
- Risk EngineがKill Switchを発動した際は、同一プロセス内であるためネットワーク越しの通知APIを介さず、`cmd/desktop/notify.go`の`App`（`risk.Notifier`実装）が直接Wailsランタイム（`runtime.SendNotification` / `runtime.EventsEmit`）を呼び出してOSレベルのトースト通知とウィンドウ内インジケータ用イベントを発生させる。OSシステムトレイのアイコン変化は未対応（Wails v2にトレイAPIが無く、Wails v3または外部systrayが必要）
- Windowsログイン時の自動起動とクラッシュ時の自動再起動は、インストーラーがスタートアップフォルダへ作成する`pitha-trador.exe --supervise`のショートカットで実現する。`--supervise`付きで起動したプロセスは`internal/supervisor`により自身（`--supervise`なし）を子プロセスとして起動・監視し、非0終了/killでは指数バックオフ付きで再起動、終了コード0（操作者の終了・自動更新）では監視を終了する。ログはWailsアプリと同じ日次JSONログへ追記する（`requirements/non-functional.md` §3）
- SQLiteファイルはWailsアプリの起動時に存在確認・マイグレーション適用を行う。Postgresのような別プロセスの起動待ち合わせは不要
- 将来ヘッドレス運用（例: CI・テスト環境）が必要な場合に備え、`cmd/desktop`とは別に`cmd/server`（Wailsを使わずGinのみを`net/http`でリッスンするエントリーポイント）を用意できるよう、`internal/router`はWailsに依存しない形で実装する
- 自動アップデート（`internal/service/updater`、`cmd/desktop`のみ）は検知結果を`Checker.Status()`（最終確認時刻・新バージョン有無・安全ゲート保留とその条件種別`BlockedKind`・インストーラー準備済み・直近エラーとその種別`ErrorKind`。UIは種別を文言化し、生のエラー文言は表示しない）として保持し、`bootstrap.Services.Updater`→`router.WithUpdateController`経由でHandlerへ渡す。UIはHeaderの`UpdateBanner`で新バージョンと保留状態を通知し、Settings画面の「今すぐアップデートを確認」（`POST /system/update-check`）でスケジューラーと同じ`CheckForUpdate`を手動実行できる。`CheckForUpdate`は排他制御され、周期実行と手動実行が同時にインストーラーをダウンロード/終了要求することはない。`cmd/server`はアップデーター未搭載のためバナー/パネルは空、確認ルートは404（issue #76）。取得経路はリクエストごとの`context`タイムアウト（リリース確認30秒・ダウンロード全体10分）、ダウンロードサイズ上限（インストーラー512MiB・`checksums.txt`1MiB、GitHubの`asset.size`報告値があればそれに厳格化）、`browser_download_url`が`https://github.com/<owner>/<repo>/releases/download/`配下であることの検証で保護する（issue #126）。`checksums.txt`はインストーラーと同一リリース由来のためSHA256照合だけでは転送破損しか検知できず、リリース差し替えに対する真正性は`checksums.txt.sig`（`checksums.txt`のed25519分離署名、base64）で検証する（issue #376）。署名鍵はGitHub Actionsシークレット`RELEASE_SIGNING_KEY`、検証用公開鍵はリポジトリ変数`RELEASE_SIGNING_PUBLIC_KEY`から`-ldflags`で`internal/version.ReleasePublicKey`へ埋め込み、`updater.Config.PublicKey`で上書きできる。公開鍵を埋め込んだビルドでは、署名アセット欠落・形式不正・署名不一致を`ErrorVerification`（Permanent、再試行しない）として扱い、インストーラーをダウンロードせず一時ファイルも削除する。公開鍵が空のビルド（鍵未発行の環境）では署名検証を行わずWarnログを出して従来のSHA256照合のみとなる既知制約があり、鍵の発行手順は`docs/environment/setup.md`に従う。Authenticodeコード署名は未導入
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
    participant FEED as 外部ニュースフィード
    participant LUNA as Luna Adapter (internal/service/assist/luna.go)
    participant CACHE as インメモリキャッシュ（直近N件、TTL付き）
    participant SCOUT as Jev Scout

    loop 定期ポーリング
        NF->>FEED: 対象銘柄（instruments.is_active）関連ニュース取得
        FEED-->>NF: 見出し・本文
        NF->>LUNA: ニュース本文（Luna API、実際の外部AI呼び出し）
        LUNA-->>NF: sentiment / event_type / summary
        NF->>CACHE: 銘柄別に格納（TTL経過分は破棄）
    end
    SCOUT->>CACHE: 対象銘柄のnews_context取得
    CACHE-->>SCOUT: 直近sentiment/event_type/summary（該当なしは空）
    SCOUT->>SCOUT: jev_decisions.state_jsonへnews_contextとして注入
```

- 永続化は`jev_decisions.state_json`（既存カラム）のみを利用し、新規テーブル・マイグレーションは追加しない（キャッシュはプロセスメモリ内のみでDB非永続）
- **認証情報の入力経路**: `LUNA_API_KEY`/`LUNA_BASE_URL`、`NEWS_FEED_URL`/`NEWS_FEED_API_KEY`は§5・§6と同じくSettings画面（`/settings`）経由で`secrets`テーブルへ保存する
- **外部API契約**: ニュースフィードは`GET {NEWS_FEED_URL}?symbol={銘柄コード}`（`Authorization: Bearer {NEWS_FEED_API_KEY}`）で`{"items":[{"id","headline","body","published_at"}]}`を返す。Lunaは`POST {LUNA_BASE_URL}/v1/classify`（リクエスト: `symbol`/`headline`/`body`/`published_at`、レスポンス: `{"sentiment":"bullish|bearish|neutral","event_type":"決算|業績修正|M&A|規制|その他","summary":"..."}`）。値域外の応答は失敗として扱う
- News Ingestは1分周期でポーリングし、記事ID（無ければ見出し）単位で重複排除して1件ずつLunaへ送信する。キャッシュは銘柄ごと直近5件・TTL 6時間（公開時刻基準）。新規に分類された記事は銘柄単位のニュースフラグを立て、Fast Screener候補であればイベント再評価（FR-SCAN-1）で1回だけ消費される
- LUNA/NEWS_FEEDのどちらかが未設定の間はNews Ingestを起動しない（`news_context`は注入されずニュースフラグも立たない）。SOL/OPUS未設定時は各段階を毎日スキップする
- Luna/News Ingest API失敗時はニュースフラグを立てず、Fast Screener/Jevの通常フローに影響を与えない（FR-LUNA-4、Jevと同様のフェイルセーフ）
