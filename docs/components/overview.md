# コンポーネント設計

HALT（HTMX + Atomic Design + Lit + Templ）に基づくフロントエンドアーキテクチャを、Wailsによるネイティブデスクトップシェルの上で構成する。設計思想の詳細は `skill://halt/references/architecture.md` を踏襲し、本ドキュメントはプロジェクト（pitha-trador）固有の適用を記述する。

## 1. フロントエンドアーキテクチャ概要

- フロントエンドはAPIサーバー（Gin）の中で動く。SPAは作らない
- サーバー（Go）がUI制御を握り、HTMXでハイパーメディア駆動のインタラクションを実現する
- リッチなインタラクション（チャート・ライブテーブル・Kill Switch操作）が必要な箇所だけ Lit Web Components（`pitha-*`）で拡張する
- テンプレートは Atomic Design（atoms/molecules/organisms/pages）で構造化する
- **HATEOAS**: サーバーが現在の状態（Running/Paused/Killed・保有ポジション有無・権限）に基づき、利用可能なアクションのみをHTML/属性として出力する。ボタンは「見えるなら押せる」。`hidden`/`disabled`で隠すのではなくレンダリングしない
- WailsのWebView2は、Gin Engine を `AssetServer.Handler` として注入されたローカルプロセス内アセットサーバーにのみアクセスする（`architecture/overview.md` §9）

### 技術スタック

| レイヤー | 技術 | 役割 |
|---------|------|------|
| デスクトップシェル | Wails v2 | ネイティブウィンドウ・通知（トレイは未対応）（§7で詳述（`runtime.md`）） |
| サーバーフレームワーク | Gin | ルーティング＋SSR |
| APIフレームワーク | Huma | `/api/v1/...` のJSON API・OpenAPI 3.1自動生成 |
| テンプレートエンジン | Templ | 型安全なGo HTMLテンプレート |
| インタラクション | HTMX | サーバー駆動のDOM更新 |
| リッチUI | Lit (Web Components) | `pitha-price-chart` 等5種（§5、`lit.md`） |
| チャート描画 | lightweight-charts (TradingView製) | ローソク足・VWAP・出来高・信頼性曲線 |
| スタイリング | Tailwind CSS | ユーティリティファーストCSS |
| ビルド | esbuild | Lit/TypeScriptバンドル |

## 2. ディレクトリ構成

```text
internal/web/
├── apierror/           # /api/v1 の huma.NewError 上書き（5xx は固定メッセージのみ返し原因を slog へ。issue #215）
├── handler/            # 直下: scanner.go, performance.go, calibration.go, policy_proposals.go, swagger.go。責務別サブパッケージ: symbol/（symbol*.go）, system/（system.go, update.go ほか）, settings/, activity/, shared/（action_error.goのToast/ErrorPage応答・ws_poll.goのWebSocketポーリング。*_ws.goはWebSocket）
├── insightapi/         # 判断履歴・シグナル・実績の読み取り専用JSON API（Huma登録）
├── middleware/         # HostGuard, Session（Cookie+CSRF）, RequestLog, Recovery, 操作者ハートビート記録（heartbeat.go）, Setup Guard（必須認証情報未設定時に`/setup`へ302、issue #80）, SystemState
├── atoms/              # Badge, StatusDot, Toast
├── molecules/          # SecretFieldRow, SignalBadgeGroup, PositionRow
├── organisms/          # Header, KillSwitchPanel, SystemStatusBadge ほか（§3）
├── pages/              # ScannerPage, SymbolDetailPage ほか、ErrorPage（§3）
└── layout/             # Shell, SetupShell

static/
└── src/
    ├── components/
    │   ├── price-chart/           pitha-price-chart.ts
    │   ├── scanner-table/         pitha-scanner-table.ts
    │   ├── calibration-heatmap/   pitha-calibration-heatmap.ts / calibration-view.ts（応答型と表示用の純粋ヘルパー）
    │   ├── activity-feed/         pitha-activity-feed.ts
    │   ├── kill-switch-panel/     pitha-kill-switch-panel.ts
    │   ├── htmx-errors/           pitha-htmx-errors.ts（Litではない。HTMX失敗時のトースト処理）
    │   └── lib/
    │       ├── api.ts
    │       ├── ws.ts / ws-status.ts    # WebSocket接続と接続状態表示
    │       └── logger.ts / styles.ts
    ├── css/
    │   └── app.css
    ├── img/
    │   └── logo.svg               # アプリロゴ（Header表示用、go:embed対象）
    └── dist/
        ├── js/
        └── css/
```

依存ルールは `architecture/overview.md` §3 の通り（`handler → service → repository → domain`、Templ側は `atoms/molecules/organisms/pages`）。

## 3. Templ テンプレート（Atomic Design）

### atoms

- `Badge`（Direction: LONG/SHORT/NONE、Regime: TREND/RANGE/BREAKOUT/CHAOTIC の色分け表示）
- `EntryQualityBadge`（Entry Quality: poor/fair/good/strong/exceptional の色分け表示。Scanner Dashboardで使用、issue #239）
- `StatusDot`（システム状態: Running=緑 / Paused=黄 / Killed=赤。organismsの`SystemStatusBadge`が`domain.SystemState`から`atoms.State`へ変換して描画する）
- `Toast`（HTMXアクション失敗のエラー通知。`role="alert"`＋閉じるボタンを持ち、`#toast-region`へswapされる。§4「エラー表示」、issue #110/#121）

> **未実装コンポーネントの扱い（issue #120）**: 現状のアプリは`Button`/`Input`/`Select`/`Spinner`/`Card`/`Modal`/`OrderRow`/`ConfidenceBucketBar`/`Sidebar`/`CalibrationBucketTable`のいずれも必要としない（ボタン・入力はTailwindユーティリティを各テンプレートに直接記述、Kill Switch確認は`pitha-kill-switch-panel`内の`window.confirm`、エラーは各画面/コンポーネント内の`role="alert"`表示、ナビゲーションは`Header`、Calibration帯別の表示は`pitha-calibration-heatmap`が担う）。これらは実装せず、**利用箇所が生じた時点で対応するレイヤに追加する**（同一の見た目・属性が複数テンプレートで重複した時点が`Button`/`Input`等の切り出しの目安）。§4・`api/endpoints.md`で言及する「確認モーダル」「トースト」も、現状はそれぞれ`window.confirm`・インラインの`role="alert"`/`role="status"`表示で実現している。

### molecules

- `SecretFieldRow`（Settings画面の1項目。ラベル・「設定済み」バッジ・値入力（`type=password`）と保存ボタン・削除ボタン（設定済みのときのみ）・直近の保存/削除結果の通知を持ち、保存は`POST /settings/:key`、削除は`DELETE /settings/:key`で行の`outerHTML`のみ差し替える。他項目の値には影響しない。issue #79）
- `SignalBadgeGroup`（direction + confidence + entry_quality の組み合わせ表示）
- `PositionRow`

### organisms

- `Header`（ナビゲーション＋`SystemStatusBadge`（`StatusDot`）。Kill Switch状態のOOB更新対象。`middleware.SystemStateFrom`の現在状態から`KillSwitchPanel`を描画する）
- `SystemStatusBadge`（システム状態の`StatusDot`フラグメント。`Header`内`#header-status`と`GET /system/status`が返す。`domain.SystemState`→`atoms.State`の変換を担い、atomsを`internal/domain`から切り離す）
- `KillSwitchPanel`（`pitha-kill-switch-panel`を、現在状態に基づく`status`/`can-pause`/`can-resume`/`can-kill`と各URL属性付きで出力する。issue #106）
- `ScannerTableFallback`（JS無効時/初回SSR描画用の候補件数＋候補銘柄テーブル＋0件時の空状態。日本語列見出し＋ツールチップ、符号付きReturnの色分け、Jev方向/エントリー品質バッジ。ハイドレーション後は同一の見た目で`pitha-scanner-table`が引き継ぐ。列定義・書式・配色・空状態文言はGo側`scannerColumns`とLit側`COLUMNS`/`scanner-view.ts`で二重管理のため、共有ゴールデン`static/src/components/scanner-table/scanner-contract.json`を`scanner_table_contract_test.go`と`scanner-contract.test.ts`の双方が検証して乖離を防ぐ。小数の丸めはJSの`toFixed`に揃え、ちょうど中間の値は0から遠い方へ丸める（Goの`%f`は偶数丸めのため`formatFloat`で補正。例: 12.5→13）。符号は正のみ`+`（0は符号なし）、確信度は四捨五入（half away from zero）、銘柄リンクは非予約文字以外をパーセントエンコード。表の上に列の意味を`<details data-testid="scanner-column-help">`（`<dl>`）で常時表示可能にし、hover専用の`title`を補う。SSRの空状態は初回描画のため`role="status"`を持たない。issue #239）
- `DecisionHistoryList`（Jev判断履歴の時系列リスト）
- `PerformanceSummaryPanel`
- `UpdateBanner`（新バージョン検知時の全ページ共通通知バナー。`Header`内`#update-banner`が`GET /system/update-status`を`hx-trigger="load, every 60s, updateStatusChanged from:body"`で取得。安全ゲート待ち（`Blocked`）・インストーラー準備完了（`Ready`）を文言で区別し、新バージョンが無ければ描画しない、issue #76）
- `UpdatePanel`（Settings画面の「アップデート」節。現在バージョン・最終確認結果・安全ゲート保留中はその旨と理由（ポジション保有/Kill Switch/直近発注）・失敗時は原因の種別（ネットワーク/レート制限/検証失敗など。生のエラー文言は出さない）・「今すぐアップデートを確認」ボタン（`POST /system/update-check`、`#update-panel`をinnerHTMLスワップ。確認中は`hx-disabled-elt`で無効化・`hx-sync="this:drop"`で二重送信を破棄し、`#update-check-progress`に進行表示）。`cmd/server`（アップデーター未搭載）ではアップデート機能が無い旨を表示しボタンは出さない。issue #76/#241）
- `ErrorLogPanel`（Settings画面の「エラーログ」節`#error-log-panel`。対象期間（直近1/7/30/90日、既定7日）とレベル（ERRORのみ/WARN以上）の`<select>`と「ダウンロード」ボタンを持つ`<form method="get" action="/api/v1/logs/errors">`をSSRで描画する。ブラウザ標準のダウンロードに任せるため`hx-disable`を付けHTMXの差し替えと`lib/api.ts`は使わず、応答の`Content-Disposition: attachment`で保存される。秘密情報はマスク済み・最大10MiBである旨を注記する。`requirements/functional/components-platform.md` §4.19、issue #267）
- `MarketDataBanner`（kabuステーションAPIのトークン発行失敗時の全ページ共通エラーバナー。失敗原因（未起動・API未有効 / 未ログイン / APIパスワード不正 / API利用不可）と対処を示し、自動再試行中である旨と`/settings`リンクを表示する。`Header`内`#marketdata-banner`が`GET /system/marketdata-status`を`load`・30秒周期で取得する、issue #295）
- `SecretsBanner`（SLACK_WEBHOOK_URL等の任意キー未設定時の全ページ共通案内バナー。必須2キーはSetup Guardが`/setup`へ誘導するため対象外。`Header`内`#config-banner`が`GET /system/secrets-status`をhx-trigger="load"で自己補正取得する、issue #57/#80）
- `QueueStatusPanel`（`jobs`テーブルのキュー別pending/running/直近failed件数を表示。System Activity Logのほか、将来Headerへの常時表示も想定）
- `ActivityFeedFallback`（JS無効時/初回SSR描画用のアクティビティ一覧テーブル。ハイドレーション後は`pitha-activity-feed`が引き継ぐ）

### pages

- `ScannerPage`（`layout.Shell`＋見出し、説明文`data-testid="scanner-description"`、色の凡例`data-testid="scanner-legend"`（緑=プラス/LONG・赤=マイナス/SHORT・エントリー品質の序列を文言で明記）、`ScannerTableFallback`、`pitha-scanner-table`バンドル）
- `SymbolDetailPage`（`pitha-price-chart` アイランドを埋め込む）
- `PerformancePage`
- `CalibrationPage`（`pitha-calibration-heatmap` アイランドを埋め込む）
- `SettingsPage`（JEV_API_KEY/KABU_API_PASSWORD/SLACK_WEBHOOK_URLと各APIキーの入力項目を`SecretFieldRow`で縦に並べ、URL・モデル名などの任意の上書き項目（JEV_BASE_URL/JEV_MODEL/LUNA_BASE_URL/NEWS_FEED_URL/SOL_BASE_URL/OPUS_BASE_URL）は折りたたみの「詳細設定（任意）」（`<details>`、保存済みの上書き値があるときは開いた状態）に置く。値は再表示せず設定済み状態のみ表示し、項目ごとに独立して`secrets`テーブルへ暗号化保存・削除する。末尾に`ErrorLogPanel`を置く。issue #57/#79/#267）
- `SetupPage`（初回セットアップ画面。必須2キー＋任意のSLACK_WEBHOOK_URLを`SecretFieldRow`で表示し、保存・削除は`POST`/`DELETE /settings/:key`を共用する。必須2キーがすべて設定済みなら完了表示と`/scanner`へのリンクを出す。`Header`を含まない`layout.SetupShell`で描画。`requirements/functional.md` §4.18、issue #80）
- `ErrorPage`（SSRページ失敗時の全ページエラー画面。`layout.Shell`（`Header`込み）でステータスコード＋固定メッセージ（`err.Error()`は表示しない）＋`/scanner`への戻りリンクを描画し、`shared.RespondPageError`（`internal/web/handler/shared`）が使用する。`api/endpoints.md` §7、issue #143）
- `ActivityLogPage`（`QueueStatusPanel` + `pitha-activity-feed` アイランドを埋め込む。`requirements/functional.md` §5.5）

### コンポーネントインターフェース規約

`skill://halt/references/architecture.md` の規約（単純コンポーネントは直接パラメータ、複雑なコンポーネントは`Props`構造体＋`templ.Attributes`、バリエーションはGoのconst+カスタム型）にそのまま従う。プロジェクト固有の型例:

```go
type Direction string

const (
    DirectionLong  Direction = "LONG"
    DirectionShort Direction = "SHORT"
    DirectionNone  Direction = "NONE"
)

type Regime string

const (
    RegimeTrend    Regime = "TREND"
    RegimeRange    Regime = "RANGE"
    RegimeBreakout Regime = "BREAKOUT"
    RegimeChaotic  Regime = "CHAOTIC"
)
```

## 4. HTMX パターン

`api/endpoints.md` §2〜4 のルーティング定義に対応する。要点のみ再掲する。

- ページルート（`/scanner`, `/symbols/:symbol`, `/performance`, `/calibration`, `/settings`, `/setup`）はHX-Requestヘッダで フルページ/フラグメント を分岐する
- アクションルート（`/positions/:id/close`, `/system/update-check`, `/settings/:key`）は常にフラグメントを返す。Kill Switch操作（pause/resume/kill）はHTMXアクションルートを持たず、Litの`pitha-kill-switch-panel`が`/api/v1/system/*`を呼ぶ
- **状態バッジの更新**: システム状態変更（pause/resume/kill）後は、`pitha-kill-switch-panel`が`systemStateChanged`イベントを発火し、Headerの`StatusDot`が`GET /system/status`で再取得される。OOBスワップは「副作用の反映」のみに限定する
- **ローディング**: Kill Switch実行ボタンは`hx-disabled-elt="this"`で二重発動を防止し、`hx-indicator`でスピナーを表示する。スケルトンスクリーンは使わない
- **エラー表示**: htmx 2は4xx/5xxを既定でswapしないため、`layout`が`<meta name="htmx-config">`の`responseHandling`（htmx 2標準機能。`response-targets`拡張の後継でありvendorしない）で`[45]..`を`#toast-region`へ`beforeend`でswapする。アクションハンドラは失敗時にステータスと`atoms.Toast`フラグメント（`shared.RespondActionError`）を返す。`static/src/components/htmx-errors/pitha-htmx-errors.ts`が①Toastを持たない失敗応答（空ボディ・プロキシのプレーンテキスト等）のswap抑止と`htmx:responseError`での汎用トースト、②`htmx:sendError`/`htmx:timeout`（応答なし）のトースト、③閉じるボタンと8秒での自動消去を担う。トースト表示先は全ページ共通の`#toast-region`（`layout.Shell`/`SetupShell`）で、フォーム再レンダリング（422）は現状どのルートも使わない（フィールド単位保存の400もトースト）（issue #110/#121）
- **アップデート通知**: `Header`内`#update-banner`は`GET /system/update-status`を`load`・60秒周期・`updateStatusChanged`イベントで取得し、`UpdateBanner`または何も描かない。Settings画面の`#update-panel`は「今すぐアップデートを確認」（`POST /system/update-check`）の応答で置き換わり、応答の`HX-Trigger: updateStatusChanged`でHeaderのバナーも即時更新される（issue #76）
- **バージョン表示**: `Header`内`#header-version`が`internal/version.Version`（リリースビルドはタグ名、ブランチ/PRビルドは`dev`）を全ページで表示し、`/settings#update-panel`へリンクする。手動の「今すぐアップデートを確認」ボタンはHeaderに置かず、Settings画面に一本化する（確認でインストーラーが検証済みになるとアプリが自動再起動するため、全ページ常設の押下導線にしない）。リンク先の`#update-panel`は`SettingsPage`の見出し直下（秘密情報入力欄より上）に置き、遷移後は`:target`のリングで強調して初回表示領域内に着地させる（issue #241）
- **ロゴ表示**: `Header`内`#header-logo`が`/static/img/logo.svg`（`static/src/img/logo.svg`。`go:embed`でバイナリに同梱、`make dev`では`PITHA_STATIC_DIR`経由でディスクから配信）とアプリ名を`nav`の直前に表示し、`/scanner`へリンクする。`nav`（`aria-label="メインナビゲーション"`）と同じflexグループ内に置き、狭い幅ではグループ内で折り返す（`flex-wrap`/`min-w-0`）。バージョン・StatusDot・Kill Switchパネルの`justify-between`配置は変わらない。ロゴの「P」マークは`cmd/desktop/build/appicon.png`（Wailsデスクトップアイコン）と同じ意匠（白地の角丸＋ネイビーのセリフ体P）で揃える。`<img>`は隣接するアプリ名テキストが代替になるため`alt=""`（issue #238）
- **フィールド単位保存**: Settings画面は1つの一括フォームではなく、`SecretFieldRow`ごとの独立フォームで保存（`hx-post="/settings/:key"`）・削除（`hx-delete="/settings/:key"`、`hx-confirm`で確認）し、応答の行フラグメントで当該行のみを差し替える。空入力の保存は400で、値の削除は明示的な削除操作でのみ行う（issue #79）
- **市況データ接続バナー**: `Header`内`#marketdata-banner`は`GET /system/marketdata-status`を`load`・30秒周期で取得し、トークン発行が失敗している間だけ`MarketDataBanner`を描く。起動時にトークンが取れなくてもアプリは継続起動し（開発機でkabuステーション未起動でもScannerやAPIを使えるようにする既存方針）、バックグラウンドで再試行して復旧後は自動でバナーが消える（issue #295）
- **未設定バナー**: `Header`内`#config-banner`は`GET /system/secrets-status`を`hx-trigger="load"`で取得し、`SecretsBanner`（任意キー（SLACK_WEBHOOK_URL等）の未設定一覧＋`/settings`リンク）またはnothingを描く。必須2キーはバナーではなくSetup Guardの`/setup`リダイレクトで扱う。`#header-status`と同じSSR空→自己補正パターン（issue #57/#80）
- **初回セットアップ誘導**: Setup Guard Middlewareが必須2キー未設定の間`/setup`以外（`POST`/`DELETE /settings/:key`・`/static/...`を除く）を`/setup`へ送る（ページ遷移は302、HTMXは`204`＋`HX-Redirect`、`/api/v1`は503 JSON、WebSocketは403。issue #140）。`SetupPage`は`Header`を持たない`layout.SetupShell`で描画し、ガード対象の`hx-get`フラグメントを発火させない。保存はSettingsと同じ`SecretFieldRow`の`hx-post="/settings/:key"`を使い、2キーが揃った時点で完了表示と`/scanner`への「続ける」リンクを出す（issue #80）

## 5〜9. 分割章

以降の章は `.linterly.yml` の300行/ファイル制限のため別ファイルに分割している（節番号・内容は分割前と同一）。

| 節 | ファイル |
|----|----------|
| §5 Lit Web Components 仕様（§5.1〜§5.5）/ §6 API クライアント / WebSocket（`lib/`） | `docs/components/lit.md` |
| §7 Wails統合 / §8 エラーハンドリング（要約）/ §9 テスト戦略 | `docs/components/runtime.md` |

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-09-26 | 新規作成 | 初版 |
| 1.1 | 2026-09-26 | §7 Wails統合の配布説明をSQLite（アプリ内蔵）前提に更新 | PostgreSQLからSQLiteへの全面移行 |
| 1.2 | 2026-09-26 | §7にWindowsクロスビルド（CGO不要）に関する注記を追加 | レビュー指摘対応 |
| 1.3 | 2026-09-26 | §7のMakefile例を`npm`から`bun`に修正（TypeScript/JavaScriptプロジェクトはbun固定の方針と統一） | 表記統一 |
| 1.4 | 2026-09-28 | organisms/pagesに`SecretsBanner`/`SettingsPage`を追加、§4に`/settings`ルートと未設定バナーのHTMXパターンを追記 | issue #57実装 |
| 1.5 | 2026-09-28 | §5.4の`HeaderWithKillSwitch`例の`hx-trigger`セレクタを`.header-container`から`header`要素セレクタに修正 | issue #74実装でHeaderのTailwindユーティリティクラス化に伴い`header-container`クラスを廃止したことへの追随 |
| 1.6 | 2026-09-29 | organismsに`UpdateBanner`/`UpdatePanel`を追加、§4にアップデート通知のHTMXパターンを追記 | issue #76実装 |
| 1.7 | 2026-09-29 | organisms/pagesに`QueueStatusPanel`/`ActivityFeedFallback`/`ActivityLogPage`、Litに`pitha-activity-feed`（§5.5）を追加 | issue #77実装（System Activity Log） |
| 1.8 | 2026-09-29 | moleculesに`SecretFieldRow`を追加、`SettingsPage`をフィールド単位の保存・削除へ変更、§4に「フィールド単位保存」パターンを追記 | issue #79実装 |
| 1.9 | 2026-09-29 | `middleware/`にSetup Guard、pagesに`SetupPage`（`layout.SetupShell`）を追加。`SecretsBanner`の対象を任意キーのみへ縮小し、§4に初回セットアップ誘導パターンを追記 | issue #80実装 |
| 1.10 | 2026-09-29 | §3から未使用のatoms/molecules/organisms（`Button`/`Input`/`Select`/`Spinner`/`Toast`/`Card`/`Modal`/`OrderRow`/`ConfidenceBucketBar`/`Sidebar`/`CalibrationBucketTable`）を除き、「利用箇所が生じた時点で追加する」方針を明記。`KillSwitchPanel`を追加し§5.4を状態・URL属性のSSR注入に更新、§6に`WsClient`の`onStatusChange`と切断表示を追記 | issue #106/#120/#133実装 |
| 1.11 | 2026-09-29 | atomsの`Toast`を実装し、§4「エラー表示」をhtmx 2標準の`responseHandling`＋`htmx-errors`モジュールによる方式へ更新（`response-targets`拡張は採用しない）、§2ディレクトリ構成に`htmx-errors/`を追加 | issue #110/#121実装 |
| 1.12 | 2026-09-29 | `middleware/`にリクエストログ（`RequestLog`）とpanicリカバリ（`Recovery`）を実装し、未使用のHTMXアクション`POST /system/pause\|resume\|kill`を削除（Kill Switch操作は`/api/v1/system/*`に一本化） | issue #108/#109/#122 |
| 1.13 | 2026-09-29 | Setup Guardの応答種別、Shadow DOMコンポーネントのスタイル方針、本番sourcemap無効化を追記 | issue #140/#145/#146実装 |
| 1.14 | 2026-09-29 | §1・§5.4の`architecture/overview.md`節参照を実在する§9・`overview/flows.md` §10.3へ修正、§2の`lib/`に`ws-status.ts`/`styles.ts`を追加 | issue #153/#155 |
| 1.15 | 2026-09-29 | §5〜§9を`lit.md`（§5〜§6）・`runtime.md`（§7〜§9）へ分割（節番号・内容は変更なし）。§2のhandler/middleware/atoms〜layout一覧、§3のorganismsに`SystemStatusBadge`・pagesに`ErrorPage`を実装に合わせて追記 | issue #182（300行/ファイル制限の解消・実装追従） |
| 1.16 | 2026-09-29 | §2の`internal/web/`ツリーに`apierror/`を追加 | issue #215/#219 |
| 1.17 | 2026-09-30 | `Header`に`#header-version`（バージョン表示、`/settings#update-panel`へのリンク）を追加し、§4にバージョン表示パターンを追記 | 手動指示（ヘッダーへのバージョン表示） |
| 1.18 | 2026-09-30 | `Header`に`#header-logo`（アプリロゴ`static/src/img/logo.svg`とアプリ名）を追加し、§2の`static/src`ツリーに`img/`を追記、§4にロゴ表示パターンを追記 | issue #238 |
| 1.19 | 2026-09-30 | Scanner Dashboardの見た目を整備: atomsに`EntryQualityBadge`を追加、`ScannerTableFallback`に候補件数・空状態・日本語列見出し（ツールチップ）・符号色分けを追加し、`pitha-scanner-table`のLit描画を同一スタイルに揃えた | issue #239 |
| 1.20 | 2026-09-30 | `UpdatePanel`に保留理由・失敗種別・確認中の進行表示・アップデーター未搭載の表示を追加、`#update-panel`を`SettingsPage`の見出し直下へ移動 | issue #241 |
| 1.21 | 2026-09-30 | §2の`handler/`をサブパッケージ構成（`shared`/`symbol`/`system`/`settings`/`activity`）へ更新 | issue #245 |
| 1.22 | 2026-09-30 | §2の`calibration-heatmap/`に、行数上限（300行/ファイル）のため型と純粋ヘルパーを分離した`calibration-view.ts`を追記 | issue #248 |
| 1.23 | 2026-09-30 | 分割後の参照名を訂正（`handler.respondPageError`/`respondActionError` → `shared.RespondPageError`/`RespondActionError`）。ヘッダーのロゴ+`nav`グループに`flex-wrap`/`min-w-0`を、`<nav>`に`aria-label`を追加（§4ロゴ表示） | issue #238/#245 |
| 1.24 | 2026-09-30 | Scanner: `ScannerTableFallback`とLit描画の表記統一（0は符号なし・確信度丸め・URLエスケープ・見出しスタイル）、共有ゴールデンによる契約テスト、`ScannerPage`の説明文/凡例を記載 | issue #239 レビュー指摘 |
| 1.25 | 2026-09-30 | Scanner: 小数丸めをGo/JSで統一（中間値は0から遠い方へ）、列の説明を`<details>`で常時参照可能にし`aria-describedby`/sr-only を廃止 | issue #239 レビュー指摘 |
| 1.26 | 2026-10-01 | organismsに`ErrorLogPanel`を追加し、`SettingsPage`の末尾にエラーログのダウンロード節を置く | issue #267 |
| 1.27 | 2026-10-02 | `SettingsPage`の「詳細設定（任意）」から`UPDATE_GITHUB_TOKEN`を削除（更新確認用トークン機能の廃止） | 更新確認用トークン機能の廃止 |
| 1.28 | 2026-10-02 | organismsに`MarketDataBanner`を追加し、`Header`に`#marketdata-banner`を置く | issue #295 |
| 1.29 | 2026-10-02 | `SecretsBanner`・`SetupPage`・§4の未設定バナー／初回セットアップ誘導の必須キー表記を3キーから2キー（`JEV_API_KEY`/`KABU_API_PASSWORD`）へ更新し、`SettingsPage`の「詳細設定（任意）」に`JEV_BASE_URL`/`JEV_MODEL`を追加 | issue #271/#291 |
