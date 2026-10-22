# コンポーネント設計

HALT（HTMX + Atomic Design + Lit + Templ）に基づくフロントエンドアーキテクチャを、Wailsによるネイティブデスクトップシェルの上で構成する。設計思想の詳細は `skill://halt/references/architecture.md` を踏襲し、本ドキュメントはプロジェクト（pitha-trador）固有の適用を記述する。

## 1. フロントエンドアーキテクチャ概要

- フロントエンドはAPIサーバー（Gin）の中で動く。SPAは作らない
- サーバー（Go）がUI制御を握り、HTMXでハイパーメディア駆動のインタラクションを実現する
- リッチなインタラクション（チャート・ライブテーブル・Kill Switch操作）が必要な箇所だけ Lit Web Components（`pitha-*`）で拡張する
- テンプレートは Atomic Design（atoms/molecules/organisms/pages）で構造化する
- **HATEOAS**: サーバーが現在の状態（Running/Paused/Killed・保有ポジション有無・権限）に基づき、利用可能なアクションのみをHTML/属性として出力する。ボタンは「見えるなら押せる」。`hidden`/`disabled`で隠すのではなくレンダリングしない
- WailsのWebView2は、Gin Engine を `AssetServer.Handler` として注入されたローカルプロセス内アセットサーバーへアクセスする（`architecture/overview.md` §9）。WebSocketだけは、Wails AssetServerが扱えないためWindowsデスクトップ版が別に起動するループバック専用リスナー（`router.WebSocketOnly`。`/ws/...`のUpgradeのみ受け、`<meta name="ws-base">`でLitへアドレスを伝える）へ直接接続する（`api/endpoints.md` §6、`runtime.md` §7）

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
├── handler/            # 直下はdoc.goのみ。責務別サブパッケージ: scanner/（scanner.go, scanner_scan.go, scanner_universe.go, scanner_export.go）, watchlist/（watchlist.go。issue #730）, orderproposals/（注文案の一覧と詳細。issue #829）, performance/（performance.go, performance_view.go）, calibration/, proposals/（proposals.go）, swagger/, symbol/（symbol*.go）, system/（system.go, update.go, error_log.go, marketdata.go ほか）, settings/（settings.go, settings_fields.go, settings_hints.go）, activity/, shared/（render.goのバッファ描画`RenderHTML`・action_error.goのToast/ErrorPage応答・ws_poll.goのWebSocketポーリング・ws_accept.goのWebSocket Upgrade。*_ws.goはWebSocket）
├── insightapi/         # 判断履歴・シグナル・実績の読み取り専用JSON API（Huma登録）
├── middleware/         # SecurityHeaders（security_headers.go: CSP/`X-Content-Type-Options`/`X-Frame-Options`/`Referrer-Policy`、`/swagger`用`SwaggerCSP`、issue #378）, HostGuard, Session（Cookie+CSRF）, RequestLog, Recovery, 操作者ハートビート記録（heartbeat.go）, Setup Guard（必須認証情報未設定時に`/setup`へ302、issue #80）, SystemState, ws_base.go（`<meta name="ws-base">`用のコンテキスト値）, error_page.go（エラーページ描画の注入）
├── atoms/              # Badge（+ EntryQualityBadge）, StatusDot, Toast, Button（+ ButtonLink）, Input, Select, timefmt.go（`atoms.FormatJST`/`JST`/`TimeLayoutJST`: SSR時刻表示の唯一の書式。`2026-10-05 09:30:00 JST`）
├── molecules/          # SecretFieldRow, SignalBadgeGroup, PositionRow, Modal, SettingsCard（ConnectionStatus）, SettingsLinkCard, SetupStatus
├── organisms/          # Header, KillSwitchPanel, SystemStatusBadge ほか（§3）。scan_format.go（ScanPanelの書式ヘルパー: 次回開場時刻・サイクル所要時間。`atoms.FormatJST`系の書式を使う）。mcp/（Settings画面の MCP パネル`Panel`。issue #792）
├── pages/              # ScannerPage, SymbolDetailPage ほか、ErrorPage（§3）
└── layout/             # Shell, SetupShell

static/
└── src/
    ├── components/
    │   ├── price-chart/           pitha-price-chart.ts / bars.ts（1分足`Bar`と`foldTick`等の足の集約ヘルパー）/ chart-data.ts（系列データ・ペイン配置・代替テキストの純粋ヘルパー）/ chart-types.ts（応答・WebSocketメッセージの型）/ jst-time.ts（時間軸・クロスヘアのJST整形）
    │   ├── scanner-table/         pitha-scanner-table.ts / scanner-types.ts（`GET /api/v1/scanner`の応答型）/ scanner-view.ts（列定義・書式・配色・バッジの表示ヘルパー）/ scanner-contract.json（SSRフォールバックとLitの表示契約。Go側`scanner_table_contract_test.go`・`handler/scanner/scanner_test.go`とTS側`scanner-contract.test.ts`が共有する唯一の契約ファイル）
    │   ├── calibration-heatmap/   pitha-calibration-heatmap.ts / direction-view.ts（方向別テーブル。行数上限のため分離）/ meta-model/（メタモデルの表示: meta-model-view.ts=OOS指標・Jev評価期間の型と表示（#766）・meta-model-adoption-view.ts=採用判定の根拠（#780）。行数上限のため分離）/ calibration-view.ts（応答型と表示用の純粋ヘルパー）/ calibration-styles.ts（Shadow DOM用スタイル`heatmapStyles`と`CHART_HEIGHT`。行数上限のため分離）/ horizon-selector.ts（ホライズン切替ボタンと集計ホライズン表示。#719）/ barrier-view.ts（トリプルバリアのEV（net）・到達率の表示。#767）/ ab-arms-view.ts（paper A/B比較のアーム別・レジーム別EV・勝率の型と表示。#755）
    │   ├── exit-alignment/        pitha-exit-alignment.ts / exit-alignment-view.ts（応答型と表・警告・比較の無状態テンプレート。issue #774）
    │   ├── activity-feed/         pitha-activity-feed.ts / activity-feed-types.ts（応答型と定数）/ activity-feed-views.ts（ジョブの状況表・最近の Kill Switch の無状態テンプレート）
    │   ├── kill-switch-panel/     pitha-kill-switch-panel.ts
    │   ├── htmx-errors/           pitha-htmx-errors.ts（Litではない。HTMX失敗時のトースト処理）
    │   ├── modal/                 pitha-modal.ts（Litではない。`molecules.Modal`の`<dialog>`開閉・フォーカス復帰・URLハッシュ自動オープン）
    │   ├── copy-button/           pitha-copy-button.ts（Litではない。`[data-copy-from]`のコピーボタンと、閉じたダイアログの`[data-clear-on-close]`表示（API キーの一度きりの表示）の削除。issue #792）
    │   ├── settings-hub/          pitha-settings-hub.ts（Litではない。`/settings`のページ一覧が埋め込む`data-hash-routes`を読み、旧`/settings#modal-<id>`・`#update-panel`の直リンクを該当ページ`/settings/<page id>#<id>`へ`location.replace`で転送する。issue #805）
    │   └── lib/
    │       ├── api.ts
    │       ├── ws.ts / ws-status.ts    # WebSocket接続と接続状態表示
    │       ├── jst-datetime.ts         # SSRフォールバックの`atoms.FormatJST`と同形式のJST日時ラベル（Lit描画とSSRの表示を一致させる）
    │       └── logger.ts / styles.ts
    ├── css/
    │   └── app.css
    ├── csp/
    │   └── lightweight-charts-style-hash.test.ts   # CSPの`style-src`hashとインストール版`lightweight-charts`の一致検証（`bun test`）
    ├── img/
    │   └── logo.svg               # アプリロゴ（Header表示用、go:embed対象）
    ├── vendor/
    │   └── htmx.min.js            # checked-in、go:embed対象（`layout/shell.templ`が`/static/vendor/htmx.min.js`で参照）
    ├── embed.go                   # `//go:embed dist img vendor`
    └── dist/                      # ビルド成果物
        ├── js/
        │   └── chunks/            # esbuildの共有チャンク（`splitting: true`・`chunkNames: 'chunks/[name]-[hash]'`。`runtime.md`参照）
        ├── css/
        └── vendor/
            └── stoplight-elements/   # `/swagger`用（esbuild.config.mjsが`@stoplight/elements`から出力）
```

各コンポーネントディレクトリの`*-test-support.ts`（`scanner-table/`・`activity-feed/`・`kill-switch-panel/`・`price-chart/chart-test-support.ts`・`calibration-heatmap/heatmap-test-support.ts`、および共有の`lib/ws-test-support.ts`）と`*.test.ts`はテスト専用のフェイク・フィクスチャ・テストで、本番バンドルに含まれないため上のツリーでは省略している。

依存ルールは `architecture/overview.md` §3 の通り（`handler → service → repository → domain`、Templ側は `atoms/molecules/organisms/pages`）。

## 3. Templ テンプレート（Atomic Design）

**依存方針（issue #380/#521）**: Templ層（`atoms`/`molecules`/`organisms`/`pages`/`layout`）は`internal/service`をimportしない（`.golangci.yml`のdepguard `templ-no-service`で強制）。`internal/domain`の型と基盤パッケージ（`internal/config`・`internal/version`）には依存してよい。Templ層内の依存は一方向（`atoms` → `molecules` → `organisms` → `layout` → `pages`。`pages`が`layout.Shell`/`SetupShell`へ描画し、`layout`が`organisms.Header`を組み立てる）で、逆向きのimportはdepguardの`atoms-direction`/`molecules-direction`/`organisms-direction`/`layout-direction`が拒否する。`internal/web/middleware`をimportしてよいTempl層は組み立て層の`layout`のみ（`CSRFToken`・`WebSocketBaseURL`・`SystemStateFrom`を`<meta>`・`hx-headers`・`Header`へ流す）で、`atoms`/`molecules`/`organisms`/`pages`は`templ-parts-no-middleware`で禁止する。これらがCSRFトークン等を要するときは`web/handler`が解決してpropsで渡す（例: `molecules.SecretFieldRowProps`の`CSRFField`/`CSRFToken`。`settings.row`が`middleware.CSRFFormField`/`CSRFToken(ctx)`を詰める）。serviceの戻り値型（`backtest.Metrics`・`insight.Performance`・`execution.SymbolState`・`updater.Status`等）は、`organisms`/`pages`が定義する表示用のplain props（`PerformanceSummary`・`PerformanceActuals`・`PerformanceResult`・`UpdateBannerProps`など）へ`web/handler`が写像して渡す。service側の型変更はhandlerのコンパイルエラーで止まり、テンプレートへ波及しない。

### atoms

- `Badge`（Direction: LONG/SHORT/NONE の色分け表示。`atoms.Direction`型）
- `EntryQualityBadge`（Entry Quality: poor/fair/good/strong/exceptional の色分け表示。Scanner Dashboardで使用、issue #239）
- `StatusDot`（システム状態: Running=緑 / Paused=黄 / Killed=赤。organismsの`SystemStatusBadge`が`domain.SystemState`から`atoms.State`へ変換して描画する）
- `Toast`（HTMXアクション失敗のエラー通知。`role="alert"`＋閉じるボタンを持ち、`#toast-region`へswapされる。§4「エラー表示」、issue #110/#121）
- `Button` / `ButtonLink`（`ButtonProps{Variant, Size, Type, Attrs}`。Variant: primary / danger / secondary / outline / danger-outline、Size: medium / small。色・フォーカスリング・`disabled:opacity-50`を一元化し、`hx-*`・`data-testid`等は`Attrs`で渡す。ラベルは子要素。`ButtonLink`は`<a>`版。`Toast`の×ボタンを除く全テンプレートのボタンはこれを使う。issue #309）
- `Input`（`InputProps{ID, Name, Type, Class, Attrs}`。共通の枠線（`rounded-md border border-slate-300`）に`Class`のレイアウト系クラスを足した`<input>`。`Class`未指定は行内で広がる`flex-1 px-3 py-1`、縦積みラベル内では`Class`を渡して`flex-1`を外す。`SecretFieldRow`・`ScanPanel`・`BacktestForm`が使用。`ID`/`Name`が空のときは属性自体を出力しない（空の`id=""`は不正なHTML）。issue #309/#520/#629）
- `Select`（`SelectProps{ID, Name, Class, Attrs}`。`Input`と同じ枠線の`<select>`。`<option>`は子要素。`Class`未指定は`px-3 py-1`。`ScanPanel`・`ErrorLogPanel`が使用。`ID`/`Name`が空のときは属性自体を出力しない。issue #520/#629）

> **未実装コンポーネントの扱い（issue #120）**: 現状のアプリは`Spinner`/`Card`/`OrderRow`/`ConfidenceBucketBar`/`Sidebar`/`CalibrationBucketTable`のいずれも必要としない（Kill Switch確認は`pitha-kill-switch-panel`内の`window.confirm`、エラーは各画面/コンポーネント内の`role="alert"`表示、ナビゲーションは`Header`、Calibration帯別の表示は`pitha-calibration-heatmap`が担う）。これらは実装せず、**利用箇所が生じた時点で対応するレイヤに追加する**（同一の見た目・属性が複数テンプレートで重複した時点が切り出しの目安。`Button`/`Input`は重複したため issue #309 で追加済み）。§4・`api/endpoints.md`で言及する「確認モーダル」「トースト」も、現状はそれぞれ`window.confirm`・インラインの`role="alert"`/`role="status"`表示で実現している。

### molecules

- `SecretFieldRow`（Settings画面の1項目。ラベル・「設定済み」バッジ・値入力（`type=password`）と保存ボタン・削除ボタン（設定済みのときのみ）・直近の保存/削除結果の通知を持ち、保存は`POST /settings/:key`、削除は`DELETE /settings/:key`で行の`outerHTML`のみ差し替える。他項目の値には影響しない。issue #79）
- `Modal`（ネイティブ`<dialog>`のシェル。`aria-labelledby`でタイトルに紐付け、タイトル行に「閉じる」ボタンを持つ。`pitha-modal`が開閉・フォーカス復帰・URLハッシュからの自動オープンを担う。失敗トースト用の`[data-toast-region]`も内包する。バックドロップクリックで閉じるのは`pointerdown`も`<dialog>`自身で始まった場合のみで、入力欄からドラッグしてダイアログ外で離しても閉じない。issue #302/#353/#633）
- `SettingsCard`（Settings/Setupの一覧の1行。名前・状態バッジ・説明と、対応する`Modal`を開くボタン）と`ConnectionStatus`（設定済み／一部設定済み／未設定・必須バッジ・設定済み項目数。`ConnectionProps.Badges`/`Details`で接続先固有の秘密を含まないバッジ・状態行（立花証券 e支店の環境バッジ・セッション状態、issue #738）も描画する。保存・削除の応答では`hx-swap-oob`で差し替える。issue #302）と`SetupStatus`（Setup画面の「必須項目はすべて設定済みです」＋`/scanner`への「続ける」リンク／「必須項目をすべて保存すると…」メッセージ。リンクは完了時のみ描画。`id="setup-status"`で、`/setup`発の保存・削除の応答に必須キー充足状態を再計算して`hx-swap-oob`で同梱する。issue #325）
- `SettingFieldRow`（Settings画面「運用設定」の1項目。シークレットと異なり現在値を表示する入力（列挙値は`<select>`）と保存ボタン・「既定に戻す」ボタン（保存値があるときのみ）・既定値の表示・警告・直近の結果通知を持ち、保存は`POST /ops-settings/:key`・既定に戻すは`DELETE /ops-settings/:key`の行単位swap。`internal/web/molecules/setting_field_row.templ`、issue #708）と`SettingGroupStatus`（グループの「設定済み／既定を使用」バッジと保存済み項目数。保存・削除の応答に`hx-swap-oob`で同梱する）、`SettingGroupDerived`（グループの読み取り専用「計算結果」パネル。`SettingGroupProps.Derived`を持つグループ（「1日の損失上限・利益確保」）だけがモーダル上部に表示し、運用資金×%から導出した円の値を示す。保存・削除の応答にも`hx-swap-oob`で同梱し再読込なしに更新する。`internal/web/molecules/setting_group_derived.templ`、issue #782）
- `SignalBadgeGroup`（direction + confidence + entry_quality の組み合わせ表示。確信度の整数%整形は`atoms.FormatConfidence`（半分は0から遠ざける丸め。`0.125`→`13%`。`nil`は`—`）に一本化し、Scanner候補表のSSR（`ScannerTableFallback`）と共有する。`%.0f`の偶数丸めだと同じ値が画面によって`12%`/`13%`と食い違うため。Lit側は`Math.round`で同値。issue #677）
- `PositionRow`

### organisms

- `Header`（ナビゲーション（`MainNav`。`layout.Shell`が各ページのナビ項目`organisms.NavItem`を渡し、現在ページのリンクだけに`aria-current="page"`と強調（`font-semibold text-slate-900`）を付ける。Symbol Detailは所属の`NavScanner`、`ErrorPage`は`NavNone`で何も付けない。issue #631。項目は Scanner / Watchlist / Performance / Calibration / Activity / Settings（`NavSettings`、`/settings`）の順で、管理系の入口は Settings 1本（単独の「管理」項目は置かない。`/settings`とその下のページはすべて`NavSettings`を現在項目にする）。`Shell`を使う全ページから1操作で Settings のページ一覧へ戻れる。`/setup`だけは`Header`を持たない例外。画面一覧と Settings の構成は`docs/components/screens.md`。issue #793/#805）＋`SystemStatusBadge`（`StatusDot`）。Kill Switch状態のOOB更新対象。`layout.Shell`が`middleware.SystemStateFrom`で解決して渡す現在状態から`KillSwitchPanel`を描画する。organisms自身は`web/middleware`をimportしない）
- `SystemStatusBadge`（システム状態の`StatusDot`フラグメント。`Header`内`#header-status`と`GET /system/status`が返す。`domain.SystemState`→`atoms.State`の変換を担い、atomsを`internal/domain`から切り離す）
- `KillSwitchPanel`（`pitha-kill-switch-panel`を、現在状態に基づく`status`/`can-pause`/`can-resume`/`can-kill`と各URL属性付きで出力する。issue #106）
- `ScannerTableFallback`（JS無効時/初回SSR描画用の候補件数＋候補銘柄テーブル＋0件時の空状態。日本語列見出し＋ツールチップ、符号付きReturnの色分け、Jev方向/エントリー品質バッジ。ハイドレーション後は同一の見た目で`pitha-scanner-table`が引き継ぐ。列定義・書式・配色・空状態文言はGo側`scannerColumns`とLit側`COLUMNS`/`scanner-view.ts`で二重管理のため、共有ゴールデン`static/src/components/scanner-table/scanner-contract.json`を`scanner_table_contract_test.go`と`scanner-contract.test.ts`の双方が検証して乖離を防ぐ。小数の丸めはJSの`toFixed`に揃え、ちょうど中間の値は0から遠い方へ丸める（Goの`%f`は偶数丸めのため`formatFloat`で補正。例: 12.5→13）。符号は正のみ`+`（0は符号なし）、確信度は四捨五入（half away from zero）、銘柄リンクは非予約文字以外をパーセントエンコード。表の上に列の意味を`<details data-testid="scanner-column-help">`（`<dl>`）で常時表示可能にし、hover専用の`title`を補う。SSRの空状態は初回描画のため`role="status"`を持たない。issue #239。初回スキャン完了前（`as_of`がゼロ時刻）のcaptionは`Scanner Dashboard — データ未取得`でLit側も同じ。issue #614）
- `ScanPanel`（Scanner Dashboardのスキャン状況パネル`#scan-panel`。最新サイクルのファネル件数（`scan-funnel-*`）・時刻/所要時間・「更新」（`scan-refresh`）・「スキャン対象を見る」（`scan-open`）・現在の絞り込み（`q`/`status`/`reason`）を引き継ぐCSVダウンロードリンク（`scan-export`。`hx-disable`でHTMXを介さず`GET /api/v1/scanner/scan/export`を直接開く。ページ分割なしの全件、`api/endpoints/huma-api.md`参照）、開くと検索/状態/理由フィルターとページング付きの銘柄一覧（`scan-table`、行は`data-status`/`data-reason`）。サイクル未実行は空状態`scan-empty`。東証の立会時間外は双方の状態で停止通知`scan-offhours`（次回立会開始`scan-resume-at`、JST）を表示し、「更新」は無効化しない（`ScanPanelView.OffSession`/`NextOpen`）。フィルターフォームは1操作1リクエストで、検索語はEnter/「絞り込む」（`submit`）、状態・理由セレクト（`id="scan-status"`/`"scan-reason"`）は`change`で発火する（`hx-trigger="submit, change from:#scan-status, change from:#scan-reason"`。Tab移動のblurでは発火せず、差し替え後もidでフォーカスが戻る。issue #410）。適用中の理由は件数0でもセレクトに選択状態で残す（issue #408）。操作はすべて`hx-get="/scanner/scan"`で`#scan-panel`を`outerHTML`差し替えし、`/ws/scanner`・Lit描画は使わない。`requirements/functional.md` §5.1、issue #303）
- `ScanPanel`の銘柄マスタ未投入状態（`ScanPanelView.UniverseEmpty`。有効な`stock`が無いとき、`scan-empty`の代わりに`scanUniverseEmpty`を表示する。issue #508）: 「銘柄マスタが未投入です」の案内`scan-universe-empty`（CSVの置き場所`scan-universe-csv-guide`）と、JPXの利用上の注意へのリンク`scan-universe-jpx-terms`を添えた確認付きの取得ボタン`scan-universe-import`（`hx-post="/scanner/universe/import"`、`hx-target="#scan-panel"`、`hx-swap="outerHTML"`、押下中は無効化して`取得中です…`を表示）。押すまでJPXへは接続しない。取得後は`#scan-panel`が差し替わり、成功は`UniverseImported`件数の通知`scan-universe-imported`、失敗は`UniverseImportError`の`scan-universe-import-error`（案内とボタンは残る）を表示する。`UniverseImportError`にはハンドラが`internal/bootstrap/universe`の種別エラー（`ErrJPXConnect`/`ErrJPXFormat`/`ErrJPXSave`）から写像した固定文言だけを入れ、下位エラーは`slog`のみに記録する（issue #700）
- `ConnectionList`（Settings/Setup共通の接続先一覧。接続先ごとの`SettingsCard`と、その接続先の`SecretFieldRow`を収めた`Modal`を描く。`internal/web/organisms/connection_list.templ`、issue #302）
- `SettingGroupList`（Settings画面「運用設定」節。グループ（バックアップ先・ログディレクトリ・Policy Engineロング/ショート・Fast Screener）ごとの`SettingsCard`と、反映タイミング（再起動の要否）の注記と`SettingFieldRow`を収めた`Modal`を描く。`internal/web/organisms/setting_group_list.templ`、issue #708）
- `DecisionHistoryList`（Jev判断履歴の時系列リスト）
- `JevPanel`・`RiskPanel`・`PositionPanel`（Symbol Detailの「Jev判定」`#jev-panel`・「Risk」`#risk-panel`・「Position」`#position-panel`。`RiskPanel`は`organisms.RiskParams{AllowedPositionPct, StopLossPct, TakeProfitPct}`を受け取り、`web/handler/symbol`が`SymbolRiskParams`から写像する。issue #630）
- `BacktestFoldTable`（Performance画面のWalk Forward結果「Folds」`data-testid="backtest-folds"`。`organisms.BacktestFold{ForwardStart, ForwardEnd(排他的終端), Forward}`の一覧を描画する。issue #630）
- `BacktestForm`（Performance画面のWalk Forwardバックテスト要求フォーム。`BacktestFormValues{From, To, TrainingDays, ValidationDays, ForwardDays}`を再入力用に描画し、入力は`atoms.Input`経由。`PerformancePage`が組み立てる。issue #520）
- `PerformanceSummaryPanel`（`backtest.Metrics`を`web/handler`が写像した`organisms.PerformanceSummary`を描画する）
- `PerformanceActualsPanel`（Performance画面の「実績（Paper）」節`#performance-actuals`。クローズ済みポジションのTotal/Daily PnL・Trades・Win Rate・Profit Factor・Expectancy・Max Drawdown・Average Hold Time・Sharpe/Sortino参考値・Signal countを`GET /api/v1/performance`と同じ`insight.Performance`を`web/handler`が写像した`organisms.PerformanceActuals`から描画する。算出不能（`null`）の指標は「—」。取得失敗時は固定文言のエラーを節内に表示する。`requirements/functional.md` §5.3、issue #360）
- `UpdateBanner`（新バージョン検知時の全ページ共通通知バナー。`Header`内`#update-banner`が`GET /system/update-status`を`hx-trigger="load, every 60s, updateStatusChanged from:body"`で`X-Pitha-Background: 1`付きで取得。コンテナ`#update-banner`が固定のライブリージョン（`role="status"` `aria-live="polite"`）で、バナー本体は自前の`role`を持たない（issue #626）。安全ゲート待ち（`Blocked`）・インストーラー準備完了（`Ready`）を文言で区別し、新バージョンが無ければ描画しない、issue #76）
- `UpdatePanel`（Settings画面の「アップデート」節。現在バージョン・最終確認結果・安全ゲート保留中はその旨と理由（ポジション保有/Kill Switch/直近発注）・失敗時は原因の種別（ネットワーク/レート制限/検証失敗など。生のエラー文言は出さない）・「今すぐアップデートを確認」ボタン（`POST /system/update-check`、`#update-panel`をinnerHTMLスワップ。確認中は`hx-disabled-elt`で無効化・`hx-sync="this:drop"`で二重送信を破棄し、`#update-check-progress`に進行表示）。`cmd/server`（アップデーター未搭載）ではアップデート機能が無い旨を表示しボタンは出さない。issue #76/#241）
- `ErrorLogPanel`（Settings画面の「エラーログ」節`#error-log-panel`。対象期間（直近1/7/30/90日、既定7日）とレベル（ERRORのみ/WARN以上）の`<select>`と「ダウンロード」ボタンを持つ`<form method="get" action="/api/v1/logs/errors">`をSSRで描画する。ブラウザ標準のダウンロードに任せるため`hx-disable`を付けHTMXの差し替えと`lib/api.ts`は使わず、応答の`Content-Disposition: attachment`で保存される。秘密情報はマスク済み・最大10MiBである旨を注記する。`requirements/functional/components-platform.md` §4.19、issue #267）
- `DataExportPanel`（Settings画面の「データエクスポート」節`#data-export-panel`。開始日・終了日（UTC。`Input`の`type="date"`、必須）・形式（CSV既定/JSONLの`Select`）・種類（取引ログ/判断ログ/計測ログの`type="checkbox"`の`Input`、`name="kinds"`、既定は全選択）と「ダウンロード」ボタンを持つ`<form method="get" action="/api/v1/export">`をSSRで描画する。`ErrorLogPanel`と同様にブラウザ標準のダウンロードに任せるため`hx-disable`を付けHTMXの差し替えと`lib/api.ts`は使わない。秘密情報は`[REDACTED]`に置換される旨・外部へ送信されない旨・立花証券の利用規約に配慮し第三者へ共有しない旨・計測ログは最大10MiBで古い側が切り捨てられる旨を注記する。共有・送信ボタンは持たない。`requirements/functional/components-platform.md` §4.21 FR-EXPORT-7、issue #773）
- `MarketDataBanner`（kabuステーションAPIのトークン発行失敗時の全ページ共通エラーバナー。失敗原因（未起動・API未有効 / 未ログイン / APIパスワード不正 / API利用不可）と対処を示し、自動再試行中である旨と`/settings`リンクを表示する。未ログイン（`4001007`/`4001017`）が連続5回以上・5分以上続くときは強調形の`MarketDataPersistentBanner`（赤地の太枠・5分刻みの継続時間・失敗回数・「再ログインしてから、そのままお待ちください」・別プロセスの`/token`競合確認。`/settings`リンクなし、APIパスワードへ誘導しない。issue #712）に切り替わる。`Header`内`#marketdata-banner`が`GET /system/marketdata-status`を`load`・30秒周期で取得する。コンテナが固定のライブリージョン（`role="status"` `aria-live="polite"`）でバナー本体は`role="alert"`を持たない。issue #295/#626）
- `DailyHaltBanner`（1日の損失上限・利益確保（profit lock）・1日の最大取引回数のいずれかで当日の新規エントリーが止まっている間だけ出る全ページ共通バナー。ルール名・数値・「決済は通常どおり行われる」・「翌営業日に自動で解除」と`/settings`リンクを表示。入力`DailyHaltBannerProps{Reason, Label, Message}`、`Reason`が空なら何も描かない。`Header`の`#daily-halt-banner`が`GET /system/daily-halt`を`load`・30秒周期で取得する。issue #753、FR-RISK-8）
- `HealthAlertBanner`（立会中の市況データ停止の警告を、アラートごとに固定文言（kabuステーション未ログイン／市況データの取得が止まっている）と開始時刻（JST）で出す全ページ共通バナー。コードは`data-code`。条件が解けると消える旨と対処を併記。入力`[]HealthAlertView{Code, Since}`、空なら何も描かない。`Header`の`#health-alert-banner`が`GET /system/health-alerts`を`load`・30秒周期で取得する。issue #802、FR-SETTINGS-8）
- `SecretsBanner`（SLACK_WEBHOOK_URL等の任意キー未設定時の全ページ共通案内バナー。必須2キーはSetup Guardが`/setup`へ誘導するため対象外。`Header`内`#config-banner`が`GET /system/secrets-status`を`hx-trigger="load, secretsStatusChanged from:body"`で自己補正取得する、issue #57/#80/#695）
- `QueueStatusPanel`（`jobs`テーブルのキュー別pending/running/直近failed件数を表示。System Activity Logのほか、将来Headerへの常時表示も想定）
- `ActivityFeedFallback`（JS無効時/初回SSR描画用のアクティビティ一覧テーブル。ハイドレーション後は`pitha-activity-feed`が引き継ぐ）

### pages

- `ScannerPage`（`layout.Shell`＋見出し、説明文`data-testid="scanner-description"`、色の凡例`data-testid="scanner-legend"`（緑=プラス/LONG・赤=マイナス/SHORT・エントリー品質の序列を文言で明記）、`ScanPanel`、`ScannerTableFallback`、`pitha-scanner-table`バンドル）
- `SymbolDetailPage`（`pitha-price-chart` アイランドを埋め込む。Jev/Risk/Positionパネルは`organisms`の`JevPanel`/`RiskPanel`/`PositionPanel`で、pagesは組み立てのみ。issue #630）
- `PerformancePage`（`organisms.BacktestFoldTable`などの組み立てのみ。issue #630。実績評価節として`evaluation.EvaluationPanel`を先頭に置く。issue #777）
- `evaluation.EvaluationPanel`（Performance画面の「実績評価」節`organisms/evaluation`。`Panel`は表示整形済みのplain props（切り口別の指標表・合格ライン・コスト内訳・Jev採点・メタモデルのOOS対Paper）だけを受け取り、基準未満は「判定不能」を明示する。読み取りに失敗したときは`EvaluationError`を表示する。`organisms`のディレクトリ行数上限のため別パッケージ。issue #777）
- `CalibrationPage`（`pitha-calibration-heatmap` アイランドを埋め込む）
- `WatchlistPage`（issue #730。立花の監視リストをサーバ描画で一覧表示。アイランドなし）
- `OrderProposalsPage`・`OrderProposalDetailPage`（issue #829。`GET /order-proposals`・`GET /order-proposals/:id`。MCP の注文案の読み取りと同じ値を、`handler/orderproposals` が整形済みの props（`ProposalRow`・`ProposalDetailProps`）にして渡す。サーバ描画のみでアイランド・アクションなし。自由文は `templ` のエスケープで文字として出し、情報源の URL は http / https だけ `rel="noopener noreferrer"` 付きのリンクにする。`layout.Shell(…, organisms.NavOrderProposals)`）
- `SettingsHubPage`（`GET /settings`。Settings のページ一覧。`layout.Shell("設定", organisms.NavSettings)`。`settingspages.Pages()`の各ページ（取引ルール／リスク・資金／予測モデル／証券会社・データ／外部連携・AI／システム。issue #895で6ページに再編）と、メインメニューにない画面へのリンク（`settingspages.HubLinks()`: 稼働ログ・初回セットアップ）を`molecules.SettingsLinkCard`（名前・要約・「開く」リンク）で並べる。旧`/settings#modal-<id>`・`/settings#update-panel`を該当ページへ転送する対応表`data-hash-routes`（`settingspages.HashRoutes()`）を持ち、`pitha-settings-hub`が`location.replace`で転送する。issue #805）
- `SettingsPage`（`GET /settings/<page id>`。Settings の1ページ。パンくず（Settings / ページ名）と、`settingspages`の登録簿がそのページに置いたカードだけを描く: 接続先別の一覧（Jev/kabuステーション/立花証券 e支店/Slack/Luna/ニュースフィード/Sol/Opus。各行の`SettingsCard`に設定済み／一部設定済み／未設定（Luna/ニュースフィード/Sol/Opusは既定のJev・やのしんで動く間「既定（…）を使用」）の`ConnectionStatus`を出し、「設定する」で`Modal`を開く。モーダルにその接続先のキー・URL・モデル名の`SecretFieldRow`をまとめる（`organisms.ConnectionList`））、設定項目（`SettingGroupList`。`SettingsProps.Operational`、`router.WithOperationalSettings`未指定なら非表示）、MCP カード（`SettingsProps.MCP`）、システムカード（アップデート（`#update-panel`、`UpdatePanel`）・エラーログ（`ErrorLogPanel`）・データエクスポート（`DataExportPanel`））も同じ`SettingsCard`＋`Modal`で開く。ページ末尾の「関連画面」は Settings の外の画面（Scanner・Performance など）への`SettingsLinkCard`。`SettingsProps`は`Page`・`Dialogs`（ページのダイアログ id）・`Connections`・`Operational`・`MCP`・`Links`。値は再表示せず設定済み状態のみ表示し、項目ごとに独立して`secrets`テーブルへ暗号化保存・削除する（保存・削除の非HTMXフォールバックは、`Referer`の Settings ページへ戻る）。カードの置き先は`internal/web/settingspages`の登録簿が唯一の定義。issue #57/#79/#267/#302/#805）
- `mcp.Panel`（`internal/web/organisms/mcp`。Settings「外部連携」ページ（`/settings/integrations`）のカード「MCP（Paper 実績・Jev 判断の読み取りと、基本設定のホワイトリスト変更）」（カードID`mcp`、`SettingsCard`、`SettingsProps.MCP`が真のときだけ。`router.WithMCPSettings`未配線なら非表示）が開く`Modal`（ID`modal-mcp`。外部連携ページ`/settings/integrations#modal-mcp`）の中身。`GET /settings/mcp-panel`を`hx-trigger="load"`で遅延取得するフラグメント（`#mcp-panel`）なので、待受の有無を調べるTCP接続は`/settings/integrations`本体を遅くしない。項目: 注文案の受け付け（チェック「MCP からの注文案を受け付ける（Paper のみ）」と保存。`POST /settings/mcp/order-proposals`。既定オフ。issue #827。`data-testid="mcp-order-proposals-enabled"`）、設定変更の許可（チェック「MCP からの設定変更を許可する」と保存。`router.WithMCPWriteGate`が配線されたときだけ。`POST /settings/mcp/write`）と設定変更の監査ログ（直近10件の表）、状態バッジ（待受中／起動中／停止中／エラー／キー未設定。理由の注記つき）、「次回起動時も自動で起動します」（起動しないときは理由）、「起動」「停止」「再起動」ボタン（`POST /settings/mcp/start`・`stop`・`restart`。アプリが起動した子がいるときだけ「停止」が押せる。issue #795）、待受アドレス、有効化チェック、待受アドレス入力と「保存」（`POST /settings/mcp`。許可外のアドレスは422で保存せず、`atoms.Toast`で日本語のエラーを出す）、API キーの状態と「API キーを生成」（設定済みなら「API キーを再生成」で、古いキーが（MCPの再起動後に）使えなくなる旨の`hx-confirm`つき。`POST /settings/mcp/key`）、Grok Bot に登録する値（URL＋コピーボタン、伏せ字の`Authorization: Bearer ●●●●●●●●`）、`paper-results-mcp`の配置先と見つかる／見つからない、設定ファイルの場所、「MCP は本体の子プロセスとして起動する。設定変更の反映には再起動が要る。本体を終了すると MCP も止まる」の注記。API キーは生成直後の応答にだけ、一度きりの表示ブロック（読み取り専用の欄・`Authorization: Bearer <キー>`・コピーボタン・再表示しない旨。`hx-history="false"`・`autocomplete="off"`・`data-clear-on-close`。応答は`Cache-Control: no-store`）で出し、以後は伏せ字のみ。設定ファイルが読めない・権限が緩いときは警告を出す。コピーボタンは`static/src/components/copy-button/pitha-copy-button.ts`（`[data-copy-from="<id>"]`のテキストを`navigator.clipboard.writeText`でコピーし、ダイアログを閉じると`[data-clear-on-close]`の表示ブロックを消す。インラインJSなし）。issue #792）
- `SetupPage`（初回セットアップ画面。Settingsと同じ`ConnectionList`で、必須2キーを持つJev・kabuステーションと任意のSlackを表示し、保存・削除は`POST`/`DELETE /settings/:key`を共用する。必須2キーがすべて設定済みなら完了表示と`/scanner`への「続ける」リンクを出す（未設定の間はリンクごとレンダリングしない。issue #352）。`Header`を含まない`layout.SetupShell`で描画。`requirements/functional.md` §4.18、issue #80/#302）
- `ErrorPage`（SSRページ失敗時の全ページエラー画面。`layout.Shell`（`Header`込み）でステータスコード＋固定メッセージ（`err.Error()`は表示しない）＋`/scanner`への戻りリンクを描画し、`shared.RespondPageError`（`internal/web/handler/shared`）が使用する。`api/endpoints.md` §7、issue #143）
- `ActivityLogPage`（`QueueStatusPanel` + `pitha-activity-feed` アイランドを埋め込む。`requirements/functional.md` §5.5）

### layout

- `Shell`（HTMLドキュメントの骨格＋`organisms.Header`＋`<main>`。通常ページ用。`Shell(title, current organisms.NavItem)`で現在のナビ項目を`Header`へ渡す）
- `SetupShell`（`Header`を含まない`Shell`。初回セットアップ画面`SetupPage`用。Setup Guardが`/setup`以外をリダイレクトするため`Header`のHTMXフラグメントを持たない。issue #80）

> 本§3のatoms/molecules/organisms/pages/layoutの一覧が、公開コンポーネントの唯一の一覧である。`internal/web/{atoms,molecules,organisms,pages,layout}/doc.go`は一覧を持たず本節を参照するだけにする（二重管理しない。issue #384）。

### コンポーネントインターフェース規約

`skill://halt/references/architecture.md` の規約（単純コンポーネントは直接パラメータ、複雑なコンポーネントは`Props`構造体＋`templ.Attributes`、バリエーションはGoのconst+カスタム型）にそのまま従う。プロジェクト固有の型例:

```go
// internal/web/atoms/badge.templ
type Direction string

const (
    DirectionLong  Direction = "LONG"
    DirectionShort Direction = "SHORT"
    DirectionNone  Direction = "NONE"
)
```

Regime（TREND/RANGE/BREAKOUT/CHAOTIC）は型を持たず、`domain.JevRegime*`（`internal/domain/jevdecision.go`）の文字列定数をそのままテキスト表示する（色分けはしない）。

**表のアクセシビリティ（issue #544）**: `internal/web`のtemplに書く`<table>`は、すべての`<th>`に`scope`（列見出しは`scope="col"`）を付け、表ごとに`aria-label`（または`<caption>`）で目的を与える。`internal/web/table_a11y_test.go`が全`.templ`を静的に検査する。

## 4. HTMX パターン

`api/endpoints.md` §2〜4 のルーティング定義に対応する。要点のみ再掲する。

- ページルート（`/scanner`, `/symbols/:symbol`, `/performance`, `/calibration`, `/watchlist`, `/order-proposals`, `/order-proposals/:id`, `/activity`, `/settings`, `/settings/<page id>`, `/setup`。`/admin`は`/settings`への301）は常にフルページを返し、`HX-Request`では分岐しない（失敗時のみ`HX-Request`にはトーストを返す）。HTMXフラグメントの取得は`GET /scanner/scan`（スキャン状況パネル）と、`Header`等が取得するフラグメントのGETルート（`GET /system/status`・`/system/update-status`・`/system/update-panel`・`/system/secrets-status`・`/system/marketdata-status`）が担う
- アクションルート（`POST /positions/:id/close`, `POST /system/update-check`, `POST /scanner/universe/import`, `POST`/`DELETE /settings/:key`）は常にフラグメントを返す。Kill Switch操作（pause/resume/kill）はHTMXアクションルートを持たず、Litの`pitha-kill-switch-panel`が`/api/v1/system/*`を呼ぶ
- **状態バッジの更新**: システム状態変更（pause/resume/kill）後は、`pitha-kill-switch-panel`が`systemStateChanged`イベントを発火し、Headerの`StatusDot`が`GET /system/status`で再取得される。OOBスワップは「副作用の反映」のみに限定する
- **ローディング**: HTMXアクションは`hx-disabled-elt="this"`（必要に応じ`hx-indicator`）で二重送信を防ぐ（例: 「今すぐアップデートを確認」`#update-check-progress`、ポジション手動決済）。`hx-indicator`の表示はhtmx既定のインライン`<style>`に頼れない（`includeIndicatorStyles:false`。次項）ため、インジケーター要素はTailwindの`.htmx-request`バリアント等で自前でスタイルする。Kill Switch操作はLitの`pitha-kill-switch-panel`が`busy`状態でボタンを無効化する。スケルトンスクリーンは使わない
- **エラー表示**: htmx 2は4xx/5xxを既定でswapしないため、`layout`が`<meta name="htmx-config">`の`responseHandling`（htmx 2標準機能。`response-targets`拡張の後継でありvendorしない）で`[45]..`を`#toast-region`へ`beforeend`でswapする。アクションハンドラは失敗時にステータスと`atoms.Toast`フラグメント（`shared.RespondActionError`）を返す。`static/src/components/htmx-errors/pitha-htmx-errors.ts`が①Toastを持たない失敗応答（空ボディ・プロキシのプレーンテキスト等）のswap抑止と`htmx:responseError`での汎用トースト、②`htmx:sendError`/`htmx:timeout`（応答なし）のトースト、③閉じるボタンと8秒での自動消去（ポインタが載っている間・フォーカスが内側にある間は停止し、外れてから8秒数え直す。WCAG 2.2.1、issue #561）を担う。トースト表示先は全ページ共通の`#toast-region`（`layout.Shell`/`SetupShell`）で、フォーム再レンダリング（422）は現状どのルートも使わない（フィールド単位保存の400もトースト）（issue #110/#121）。`#toast-region`は`popover="manual"`で、トーストが入る（スクリプト追加・htmxの`beforeend`swapどちらも）たびに`pitha-htmx-errors.ts`が`hidePopover()`→`showPopover()`でtop layer最前面へ再表示する。`<dialog>.showModal()`のモーダル（Settings/Setupの保存・削除、アップデート確認）はtop layerに描画されz-indexでは勝てず、これが無いとモーダル内の失敗トーストが背面に隠れるため（issue #321）。ただし`#toast-region`は`<dialog>`の外にあるため、モーダル表示中は`showModal()`の背景inertで閉じるボタンの操作・テキスト選択ができない。そこで`molecules.Modal`が各ダイアログ内に`[data-toast-region]`を持ち、開いているモーダルがある間は`pitha-htmx-errors.ts`がスクリプト追加のトーストをそこへ追加し、htmxのエラーフラグメントも`htmx:beforeSwap`で`detail.target`をそのリージョンへ差し替えて表示する（閉じるボタンと8秒の自動消去が効く。issue #353）
- **htmxの動的実行無効化**: `layout`の`<meta name="htmx-config">`（`shell.templ`の`htmxConfig`）は`allowEval:false`（`hx-on*`・`hx-trigger`のフィルタ式・`js:`値の評価）と`allowScriptTags:false`（swapされたHTML内`<script>`の実行）、`includeIndicatorStyles:false`（htmxが読み込み時に`<style>`をインラインで注入しないようにする。CSPの`style-src`が`'unsafe-inline'`を許さないため。`hx-indicator`の表示は自前でスタイルする規約で、唯一のインジケーター`UpdatePanel`の`#update-check-progress`はTailwindの`.htmx-request`バリアントで装飾する。issue #413）、`responseHandling`（上の「エラー表示」）を設定する。テンプレートはこれらを使わない（`hx-on*`・`[...]`フィルタ・`js:`は禁止。`<script>`を返すHTMXフラグメントも作らない）。`selfRequestsOnly`はhtmx 2既定のtrueのまま（issue #379）
- **アップデート通知**: `Header`内`#update-banner`は`GET /system/update-status`を`load`・60秒周期・`updateStatusChanged`イベントで取得し、`UpdateBanner`または何も描かない。Settings画面の`#update-panel`は「今すぐアップデートを確認」（`POST /system/update-check`）の応答で置き換わり、応答の`HX-Trigger: updateStatusChanged`でHeaderのバナーも即時更新される（issue #76）
- **バージョン表示**: `Header`内`#header-version`が`internal/version.Version`（リリースビルドはタグ名、ブランチ/PRビルドは`dev`）を全ページで表示し、`/settings/maintenance#update-panel`へリンクする。手動の「今すぐアップデートを確認」ボタンはHeaderに置かず、Settings画面に一本化する（確認でインストーラーが検証済みになるとアプリが自動再起動するため、全ページ常設の押下導線にしない）。リンク先の`#update-panel`は`SettingsPage`（ログ・エクスポートページ）の「アップデート」`Modal`内に置き、`pitha-modal`がURLハッシュからそのモーダルを開く（`:target`のリングでパネルを強調。issue #241/#302）
- **ロゴ表示**: `Header`内`#header-logo`が`/static/img/logo.svg`（`static/src/img/logo.svg`。`go:embed`でバイナリに同梱、`make dev`では`PITHA_STATIC_DIR`経由でディスクから配信）とアプリ名を`nav`の直前に表示し、`/scanner`へリンクする。`nav`（`aria-label="メインナビゲーション"`）と同じflexグループ内に置き、狭い幅ではグループ内で折り返す（`flex-wrap`/`min-w-0`）。バージョン・StatusDot・Kill Switchパネルの`justify-between`配置は変わらない。ロゴの「P」マークは`cmd/desktop/build/appicon.png`（Wailsデスクトップアイコン）と同じ意匠（白地の角丸＋ネイビーのセリフ体P）で揃える。`<img>`は隣接するアプリ名テキストが代替になるため`alt=""`（issue #238）
- **モーダル（issue #302）**: `molecules.Modal`はネイティブ`<dialog>`（`aria-labelledby`でタイトルに紐付け）で、`static/src/components/modal/pitha-modal.ts`（Lit不要の小さなスクリプト）が`[data-modal-open]`ボタンから`showModal()`で開く。`showModal()`によりフォーカスはモーダル内に閉じ込められ（Tabは内部を循環、背景は操作不可）、`Esc`で閉じる。「閉じる」ボタン・背景クリックでも閉じ、閉じるとフォーカスは開いたボタンへ戻る。各ダイアログは失敗トースト用の`[data-toast-region]`を内包する（§4「エラー表示」、issue #353）。内容は通常のHTMXフォームなのでスワップはそのまま動く。保存・削除の応答は行に加えて接続先の`ConnectionStatus`を`hx-swap-oob`で返し、背後の一覧の状態を更新する
- **HTMX swap後のフォーカス（issue #676）**: htmx 2は、swap前の`document.activeElement`が`id`を持つ場合だけ同じ`id`の新要素へ`focus()`で復元する。そのためswapで置換される操作ボタン（`ScanPanel`の`scan-open`/`scan-refresh`/`scan-filter-submit`/`scan-prev`/`scan-next`/`scan-universe-import`、`UpdatePanel`の`update-check-button`、`SecretFieldRow`の`save-<key>`/`delete-<key>`、`PositionRow`の`close-position-<ID>`）には安定した`id`を付け、応答フラグメントでも同じ`id`を描画する（`atoms.Button`は`Attrs["id"]`で渡す）。置換後にボタン自体が消える／別の要素に変わる操作（`close-position-<ID>`→決済済みの行、`scan-open`→一覧、`scan-prev`/`scan-next`→端のページ、`delete-<key>`→削除後の行、`scan-universe-import`→投入結果）は、ボタンに`data-focus-after-swap="<移動先のid>"`を付ける。`pitha-htmx-errors.ts`の`htmx:afterSwap`ハンドラ（`onAfterSwap`）が、htmxの復元後もフォーカスが`<body>`に落ちたままのときだけ移動先（`tabindex="-1"`を持つ見出し・見出し的な要素: `scan-list-heading`/`scan-total`/`scan-universe-imported`、削除後は`secret-input-<key>`、決済後は置換された行`position-row-<ID>`）へ`focus()`する（復元できた場合は何もしない）。WCAG 2.4.3。
- **フィールド単位保存**: Settings画面は1つの一括フォームではなく、`SecretFieldRow`ごとの独立フォームで保存（`hx-post="/settings/:key"`）・削除（`hx-delete="/settings/:key"`、`hx-confirm`で確認）し、応答の行フラグメントで当該行のみを差し替える。空入力の保存は400で、値の削除は明示的な削除操作でのみ行う（issue #79）
- **ヘッダーのバナー枠（issue #625/#626）**: `#config-banner`・`#update-banner`・`#marketdata-banner`は同じ`order-last w-full empty:hidden`を持ち、Headerの折り返し行の最後に全幅で出る（空のときは領域を取らない）。ポーリングする`#update-banner`/`#marketdata-banner`は`data-live-banner`を持ち、`pitha-htmx-errors`が`htmx:beforeSwap`で直前と同一のレスポンスのswapを中止する。ライブリージョンは差し替えられないため、バナーの出現・内容変化時だけ支援技術に通知され、30/60秒ごとの再読み上げは起きない
- **市況データ接続バナー**: `Header`内`#marketdata-banner`は`GET /system/marketdata-status`を`load`・30秒周期で取得し（周期ポーリングは操作者ハートビートに数えない。要素の`hx-headers`で`X-Pitha-Background: 1`を送る。周期ポーリングを追加するときは同ヘッダの付与が必須、issue #310）、トークン発行が失敗している間だけ`MarketDataBanner`を描く。起動時にトークンが取れなくてもアプリは継続起動し（開発機でkabuステーション未起動でもScannerやAPIを使えるようにする既存方針）、バックグラウンドで再試行して復旧後は自動でバナーが消える（issue #295）。未ログインが続く間は継続時間・失敗回数つきの強調表示になる（`MarketDataBannerProps`の`Persistent`/`Failures`/`Elapsed`。issue #712）
- **新規エントリー停止バナー**: `Header`内`#daily-halt-banner`は`#marketdata-banner`と同じ枠規約（`order-last w-full empty:hidden`、固定の`role="status"`ライブリージョン、`X-Pitha-Background: 1`付き30秒周期ポーリング）で`GET /system/daily-halt`を取得し、停止中だけ`DailyHaltBanner`を描く（issue #753）
- **市況データ停止の警告バナー**: `Header`内`#health-alert-banner`は`#daily-halt-banner`と同じ枠規約（`order-last w-full empty:hidden`、固定の`role="status"`ライブリージョン、`X-Pitha-Background: 1`付き30秒周期ポーリング）で`GET /system/health-alerts`を取得し、警告がある間だけ`HealthAlertBanner`を描く（issue #802）
- **未設定バナー**: `Header`内`#config-banner`は`GET /system/secrets-status`を`hx-trigger="load, secretsStatusChanged from:body"`で取得し、`SecretsBanner`（任意キー（SLACK_WEBHOOK_URL等）の未設定一覧＋`/settings`リンク）またはnothingを描く。`secretsStatusChanged`は`POST`/`DELETE /settings/:key`のHTMX成功応答が`HX-Trigger`で発火し（`#update-banner`の`updateStatusChanged`と同方式）、保存・削除した直後にバナーの未設定一覧が追従する（操作起点の再取得のため`X-Pitha-Background`は付けない。ページ再読み込み不要。失敗応答・非HTMXのリダイレクトでは発火しない）。必須2キーはバナーではなくSetup Guardの`/setup`リダイレクトで扱う。`#header-status`と同じSSR空→自己補正パターン（issue #57/#80/#695）
- **初回セットアップ誘導**: Setup Guard Middlewareが必須2キー未設定の間`/setup`以外（`POST`/`DELETE /settings/:key`・`/static/...`を除く）を`/setup`へ送る（ページ遷移は302、HTMXは`204`＋`HX-Redirect`、`/api/v1`は503 JSON、WebSocketは403。issue #140）。`SetupPage`は`Header`を持たない`layout.SetupShell`で描画し、ガード対象の`hx-get`フラグメントを発火させない。保存はSettingsと同じ`SecretFieldRow`の`hx-post="/settings/:key"`を使い、2キーが揃った時点で完了表示と`/scanner`への「続ける」リンクを出す（issue #80）

## 5〜9. 分割章

以降の章は `.linterly.yml` の300行/ファイル制限のため別ファイルに分割している（節番号・内容は分割前と同一）。

| 節 | ファイル |
|----|----------|
| §5 Lit Web Components 仕様（§5.1〜§5.5）/ §6 API クライアント / WebSocket（`lib/`） | `docs/components/lit.md` |
| §7 Wails統合 / §8 エラーハンドリング（要約）/ §9 テスト戦略 | `docs/components/runtime.md` |
| Paper 実績 MCP（画面外の別プロセス） | `docs/components/paper-results-mcp.md` | issue #783。HALT のページには出さない |
| 画面一覧・Settings の構成（ナビ構造・戻り導線） | `docs/components/screens.md` | issue #793/#805。管理系の全画面の URL・できること・Settings のページ |

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
| 1.30 | 2026-10-03 | molecules に`Modal`・`SettingsCard`（`ConnectionStatus`）、organisms に`ConnectionList`を追加し、`SettingsPage`/`SetupPage`を接続先別の一覧＋モーダル構成へ変更（「詳細設定（任意）」を廃止）。§4にモーダルのパターン、`static/src/components/modal/pitha-modal.ts`を追記 | issue #302 |
| 1.31 | 2026-10-03 | organismsに`ScanPanel`を追加し、`ScannerPage`にスキャン状況パネル（ファネル件数・銘柄一覧・除外/欠損理由・手動更新）を置く | issue #303 |
| 1.32 | 2026-10-03 | atomsに`Button`/`ButtonLink`/`Input`を追加し、各テンプレートの直書きボタン・入力を置き換え | issue #309 |
| 1.33 | 2026-10-03 | §2のディレクトリ構成図に`static/src/components/modal/`（`pitha-modal.ts`）を追記 | issue #319 |
| 1.34 | 2026-10-03 | molecules に`SetupStatus`を追加し、`/setup`発の`POST`/`DELETE /settings/:key`応答で完了メッセージを`hx-swap-oob`更新 | issue #325 |
| 1.35 | 2026-10-03 | `ConnectionList`の分類を実装（`internal/web/organisms`）に合わせ、pages節からorganisms節へ移動 | issue #340 |
| 1.36 | 2026-10-03 | §2のディレクトリ構成図の`molecules/`行に`Modal`・`SettingsCard`（`ConnectionStatus`）・`SetupStatus`を追記し、§3と整合させる | issue #349 |
| 1.37 | 2026-10-03 | `SetupPage`の「続ける」リンクを`SetupStatus`の完了分岐へ移し、必須2キー未設定の間は描画しない（OOB更新と一体化） | issue #352 |
| 1.38 | 2026-10-03 | `Modal`が`[data-toast-region]`を内包し、モーダル表示中の失敗トーストをダイアログ内へ表示して閉じるボタンを操作可能にする（§3「Modal」・§4「エラー表示」） | issue #353 |
| 1.39 | 2026-10-03 | §5.1 `pitha-price-chart`のマーカー記述から`entry_quality`更新を削除し、`jev_update`の`direction`変化のみ描画・Jev判定パネルはSSRのみと明記（`components/lit.md`） | issue #362 |
| 1.40 | 2026-10-03 | `ScanPanel`に立会時間外の停止通知（`scan-offhours`・次回立会開始`scan-resume-at`）を追記 | issue #367 |
| 1.41 | 2026-10-03 | `pitha-kill-switch-panel`が`/ws/system`の`state_changed`でも`status-url`から再同期し`systemStateChanged`を発火すると追記（`components/lit.md` §5.4） | issue #363 |
| 1.42 | 2026-10-04 | `pitha-scanner-table`の銘柄リンクをサーバー生成の`detail_url`に変更（Lit側でURLを組み立てない。`components/lit.md` §5.2） | issue #383 |
| 1.43 | 2026-10-04 | §7（`runtime.md`）の静的配信の記述を`StaticFS`（`go:embed`、`PITHA_STATIC_DIR`でディスク上書き）へ訂正し、実在しない開発時のキャッシュ無効化設定と自動遷移による反映の記述を削除して手動リロードでの再取得に置き換え | issue #385 |
| 1.44 | 2026-10-04 | §4に「htmxの動的実行無効化」を追記（`htmx-config`に`allowEval:false`/`allowScriptTags:false`） | issue #379 |
| 1.45 | 2026-10-04 | §3に「依存方針」を追記し、Templ層（atoms〜layout）は`internal/service`をimportせずplain propsを受け取る方針に統一（`PerformanceSummaryPanel`/`PerformanceActualsPanel`/`PerformancePage`/`SymbolDetailPage`の入力型を表示用propsへ変更。`web/handler`が写像する） | issue #380 |
| 1.46 | 2026-10-04 | §4のページルート記述を実装に合わせ、`GET /scanner`の`HX-Request`フラグメント分岐を廃止（フルページのみ）と明記 | issue #381 |
| 1.47 | 2026-10-04 | §3に`layout`節（`Shell`/`SetupShell`）を追加し、§3を公開コンポーネントの唯一の一覧とする。`internal/web/{atoms,molecules,organisms,pages,layout}/doc.go`の実装済み一覧・例示を削除し本節への参照に置換 | issue #384 |
| 1.48 | 2026-10-04 | §3の`handler/`構成を実装に合わせ、直下の`scanner.go`ほかを`scanner/`・`performance/`・`calibration/`・`proposals/`・`swagger/`サブパッケージへ更新 | issue #370 |
| 1.49 | 2026-10-05 | `ScanPanel`の絞り込みフォームのトリガーを`submit`＋セレクトの`change`に限定しセレクトへ`id`付与（#410）、適用中の理由を件数0でも選択肢に残す（#408）。`pitha-price-chart`から未使用の`symbol`属性を削除し§5.1を訂正（#409）。旧`handler`パスを参照するコメントを更新（#401/#404） | issue #408, #409, #410, #401, #404 |
| 1.50 | 2026-10-05 | §2の`middleware/`列挙に`SecurityHeaders`（`security_headers.go`、`SwaggerCSP`）を追記 | issue #413 |
| 1.51 | 2026-10-05 | §2の`static/src`ツリーに`csp/`（`lightweight-charts-style-hash.test.ts`）・`vendor/`（`htmx.min.js`）・`embed.go`・`dist/vendor/`（`stoplight-elements`）を追記 | issue #432 |
| 1.52 | 2026-10-05 | §2の`static/src/components`ツリーに`scanner-table/`の`scanner-types.ts`・`scanner-view.ts`・`scanner-contract.json`（Go/TS共有の表示契約）、`activity-feed/`の`activity-feed-types.ts`・`activity-feed-views.ts`、`dist/js/chunks/`（esbuildの共有チャンク）を追記し、`*-test-support.ts`はテスト専用のため省略と明記 | issue #436 |
| 1.53 | 2026-10-05 | §6（`lit.md`）`lib/ws.ts`の再接続バックオフ復帰条件（`open`時点ではリセットせず、最初のメッセージ受信または`open`から10秒の接続維持で初期値へ戻す）を追記 | issue #466 |
| 1.54 | 2026-10-05 | §5.1（`lit.md`）`pitha-price-chart`の時間軸・クロスヘアをJST（Asia/Tokyo）表示と明記し、§2ツリーに`jst-time.ts`を追記 | issue #478 |
| 1.55 | 2026-10-05 | §5.3（`lit.md`）`pitha-calibration-heatmap`の空帯（`sample_count==0`）をグレー「データなし」・curve対象外、各帯`n=`表示、サンプルなし時のBrier/LogLoss/ECE非表示を追記 | issue #480 |
| 1.56 | 2026-10-05 | §5.1（`lit.md`）`pitha-price-chart`の出来高ヒストグラムが`candles`の1分足あたり`volume`をそのまま描画する旨を追記 | issue #474 |
| 1.57 | 2026-10-05 | §5.2/§5.3/§5.4（`lit.md`）エントリー品質列を品質順（poor<…<exceptional）でソートする旨（#493）と、`calibration-url`・`status-url`等のURL属性未設定時に`logger.error`を出し取得・購読・操作を行わない旨（#494）を追記 | issue #493, #494 |
| 1.58 | 2026-10-05 | §7（`runtime.md`）`make dev`スニペットを`Makefile`の`dev`ターゲットと完全一致させ（見出し行のコメントと`PITHA_UNIVERSE_PATH=$(CURDIR)/config/universe.sample.csv`を追記。#389で追加後の#320回帰）、環境変数の括弧書きに銘柄マスタCSVを追記 | issue #501, #320, #389 |
| 1.59 | 2026-10-05 | §5.1（`lit.md`）`pitha-price-chart`のスニペットを実装に合わせ、`chart`の`@state()`を外して非リアクティブ（リアクティブは`error`/`wsStatus`のみ）と明記、`wsStatus`・`disconnectedCallback`の`chart`/`wsClient`の`null`化・`override`修飾子を反映。§5.4のTempl例を`killSwitch*URL`定数に、§3の`Badge`/`Direction`/`Regime`記述を`atoms.Direction`の実体（`Regime`型・色分けは存在しない）に訂正 | issue #502 |
| 1.60 | 2026-10-05 | `ScanPanel`に銘柄マスタ未投入の案内とJPXからの確認付き取得（`scan-universe-empty`/`scan-universe-import`/`scan-universe-imported`/`scan-universe-import-error`）を追記 | issue #508 |
| 1.61 | 2026-10-05 | atomsに`Select`を追加し`Input`に`Class`を追加、organismsに`BacktestForm`を追加して`PerformancePage`/`ScanPanel`/`ErrorLogPanel`の直書きinput/selectをatoms経由に統一 | issue #520 |
| 1.62 | 2026-10-05 | SSRの時刻表示（判断履歴・Activity Feed・Scanner caption・最終サイクル・ポジション。1.68で`UpdatePanel`の最終確認時刻`CheckedAt`（`handler/system/update.go`）も対象として追記）を`atoms.FormatJST`（`2006-01-02 15:04:05 JST`）に統一し、Litの同表示も`formatJstDateTime`で同形式にした。API（JSON）はRFC 3339のまま | issue #542 |
| 1.63 | 2026-10-05 | 周期ポーリングの操作者ハートビート除外を`X-Pitha-Background`ヘッダのみに統一し、`middleware.backgroundPollPaths`を廃止 | issue #522 |
| 1.64 | 2026-10-05 | §3に表のアクセシビリティ規約（`<th scope>`・`<table>`の`aria-label`/`<caption>`）を追記し、全表へ適用して静的テストを追加 | issue #544 |
| 1.65 | 2026-10-05 | §1のWebView接続先を`api/endpoints.md` §6に整合（Windowsのloopback専用WebSocketリスナー`ws-base`を追記、`runtime.md` §7・`lit.md` §6にも反映）。§2のツリー（`scanner_universe.go`・`shared/render.go`・`Badge`/`EntryQualityBadge`・`Button`/`ButtonLink`）と§4のルート列挙（`/activity`・`POST /scanner/universe/import`・フラグメントGET群）を実態に合わせた。§3にTempl層のAtomic Design一方向依存と`web/middleware`のimportを`layout`のみに限る方針を追記し、depguard（`atoms-direction`等・`templ-parts-no-middleware`）で強制、`SecretFieldRowProps`にCSRFField/CSRFTokenを追加して`molecules`の`middleware`依存を除去（#521, #568, #587, #588） |
| 1.66 | 2026-10-06 | `Header`のナビに`aria-current="page"`（`NavItem`、#631）、3つのバナー枠の配置統一（#625）とポーリングバナーのライブリージョン化（#626）、`atoms.Input`/`Select`の空`id`/`name`非出力（#629）、Symbol Detail/Performanceのパネルを`JevPanel`/`RiskPanel`/`PositionPanel`/`BacktestFoldTable`としてorganismsへ移動（#630）、Scanner captionの初回スキャン前表示（#614）を反映 | issue #625, #626, #629, #630, #631, #614 |
| 1.67 | 2026-10-06 | §5.1/§5.3（`lit.md`）`pitha-price-chart`/`pitha-calibration-heatmap`がDOMから外されて再挿入された場合に`connectedCallback`でチャートを作り直す旨（#523）、§5.1の`tick`をスナップショット時刻（クライアント時計は使わない）の分で1分足に集約する旨（`price-chart/bars.ts`の`foldTick`。#557）、`candles-url`/`ws-url`変更時に前銘柄の方向マーカーを初期化する旨（#562）を追記。§2のツリーに`price-chart/bars.ts`・`chart-data.ts`・`chart-types.ts`、`lib/jst-datetime.ts`、`atoms/timefmt.go`、`organisms/scan_format.go`を追加し、`*-test-support.ts`の列挙を`price-chart/`・`calibration-heatmap/`・`lib/ws-test-support.ts`まで広げた | issue #523, #557, #562, #639 |
| 1.68 | 2026-10-06 | §5.5（`lit.md`）`pitha-activity-feed`の`resync`受信時のスナップショット再取得（#536）、§5.4の`pitha-kill-switch-panel`の操作世代カウンタ・`onReconnect`の分離・live region（#556, #558, #561）、§6`lib/api.ts`の`ApiError`導入と未使用の`put`/`patch`/`del`の削除（#559, #563）を履歴に記録。SSR時刻表示の統一対象（1.62）に`UpdatePanel`の最終確認時刻を追加 | issue #536, #556, #558, #559, #561, #563, #640 |
| 1.69 | 2026-10-06 | §4「htmxの動的実行無効化」に`includeIndicatorStyles:false`（CSPの`style-src`回避と、インジケーターをTailwindの`.htmx-request`バリアントで自前スタイルする規約）を追記し、`shell.templ`の`htmxConfig`の全キーと一致させた | issue #413, #641 |
| 1.70 | 2026-10-07 | `ScanPanel`のCSVダウンロードリンク`scan-export`（`GET /api/v1/scanner/scan/export`）と、§2ツリーの`handler/scanner/scanner_export.go`・`handler/settings/settings_hints.go`を追記 | issue #661 |
| 1.71 | 2026-10-07 | §2のツリーの`calibration-heatmap/`に実在する`calibration-styles.ts`（Shadow DOM用スタイル`heatmapStyles`と`CHART_HEIGHT`。行数上限のためコンポーネントから分離。#636で追加）を追記 | issue #664 |
| 1.72 | 2026-10-07 | §5.2（`lit.md`）`pitha-scanner-table`の行順の既定をSSRと同じサーバー順（ScreenScore降順）に統一し、列ソートはヘッダークリック後のみ（未選択時は`aria-sort="none"`）と明記 | issue #674 |
| 1.73 | 2026-10-07 | §4に「HTMX swap後のフォーカス」（置換されるボタンへの安定した`id`と、消えるボタン用の`data-focus-after-swap`＋`pitha-htmx-errors.ts`の`onAfterSwap`、#676）を追加。`SignalBadgeGroup`の確信度整形を`atoms.FormatConfidence`に一本化しScannerと丸めを統一（#677） | issue #676, #677 |
| 1.74 | 2026-10-07 | §4「HTMX swap後のフォーカス」に`PositionRow`のCloseボタン（`close-position-<ID>`。決済で行ごと置換され決済済みの行にはボタンが無いため、`data-focus-after-swap="position-row-<ID>"`で置換後の行へフォーカスを移す）を追加 | issue #681 |
| 1.75 | 2026-10-07 | §5.1（`lit.md`）`pitha-price-chart`が初回の`candles`0件（立会時間外・休場明け）のとき`role="status"`の可視の空状態テキストを表示する旨を追記 | issue #687 |
| 1.76 | 2026-10-07 | §3`SecretsBanner`・§4「未設定バナー」を、`#config-banner`が`load`に加えてSettings/Setup の`POST`/`DELETE /settings/:key`成功応答の`HX-Trigger: secretsStatusChanged`（`secretsStatusChanged from:body`）で再取得する記述へ更新 | issue #695 |
| 1.77 | 2026-10-07 | `ScanPanel`の`UniverseImportError`を、下位エラーの生文字列ではなく失敗種別ごとの固定文言とし、下位エラーは`slog`のみに記録する旨を追記 | issue #700 |
| 1.78 | 2026-10-07 | §5.1（`lit.md`）`pitha-price-chart`のコード断片と注記を、リアクティブ状態は`error`・`wsStatus`・`summary`・`empty`の4つである実装に合わせて訂正 | issue #703 |
| 1.79 | 2026-10-08 | `MarketDataBanner`の入力を`MarketDataBannerProps`にし、未ログイン`4001007`/`4001017`の継続時の強調形`MarketDataPersistentBanner`を追加 | issue #712 |
| 1.80 | 2026-10-08 | moleculesに`SettingFieldRow`・`SettingGroupStatus`、organismsに`SettingGroupList`を追加し、`SettingsPage`に「運用設定」節（`SettingsProps.Operational`）を追記 | issue #708 |
| 1.81 | 2026-10-08 | §2の`calibration-heatmap/`に`horizon-selector.ts`を追記、§5.3（`lit.md`）に`pitha-calibration-heatmap`のホライズン切替（5/10/15/全体、`?horizon=`）と集計ホライズン表示を追記 | issue #719 |
| 1.82 | 2026-10-08 | `ConnectionProps`に`Badges`/`Details`を追加し、`ConnectionStatus`が接続先固有の環境バッジ・状態行（立花証券 e支店カード）を描画する旨を追記 | issue #738 |
| 1.83 | 2026-10-08 | §3/§4にWatchlist画面（`GET /watchlist`、`pages.WatchlistPage`、`organisms.NavWatchlist`、`handler/watchlist`）を追加 | issue #730 |
| 1.84 | 2026-10-14 | organismsに`DailyHaltBanner`を追加し、`Header`に`#daily-halt-banner`枠（`GET /system/daily-halt`）を追記 | issue #753 |
| 1.85 | 2026-10-14 | §2の`calibration-heatmap/`に`barrier-view.ts`を追記、§5.3（`lit.md`）に`pitha-calibration-heatmap`のトリプルバリアEV（net）・到達率の表示を追記 | issue #767 |
| 1.86 | 2026-10-14 | §2の`calibration-heatmap/`に`meta-model-view.ts`を追記、§5.3（`lit.md`）に`pitha-calibration-heatmap`のメタラベリングモデルOOS指標（AUC・較正曲線・EV）とJev評価期間の表示を追記 | issue #766 |
| 1.87 | 2026-10-14 | §2の`calibration-heatmap/`に`ab-arms-view.ts`を追記、§5.3（`lit.md`）に`pitha-calibration-heatmap`のA/Bアーム別EV・勝率の表示を追記 | issue #755 |
| 1.88 | 2026-10-15 | organismsに`DataExportPanel`を追加し、`SettingsPage`の「システム」節に「データエクスポート」カード（`modal-data-export`）を置く | issue #773・親 #768 |
| 1.89 | 2026-10-17 | moleculesに`SettingGroupDerived`（運用設定グループの「計算結果」パネル）を追加 | issue #782 |
| 1.90 | 2026-10-17 | `SettingGroupDerived`の`SettingDerivedItem`に`Warn`（琥珀色の「注意:」付き警告行）と`Heading`（見出し行）を追加し、日次ガードの「計算結果」に「整合チェック」「Live 前チェック」を表示（コンポーネント名の変更なし） | issue #782 |
| 1.91 | 2026-10-17 | §2に`exit-alignment/`、§5（`lit.md`）に`pitha-exit-alignment`（Calibration画面のラベル基準と実出口の整合ブロック。`GET /api/v1/calibration/exit-alignment`）を追記 |
| 1.92 | 2026-10-17 | §2の`calibration-heatmap/`に`meta-model/`（`meta-model-adoption-view.ts`＝採用判定の根拠。#780）と`direction-view.ts`を追記し、§5（`lit.md`）にLLM学習データ終了日の警告・出典（#776）と採用判定の根拠（#780）を追記。§3に`PerformancePage`の「実績評価」節`evaluation.EvaluationPanel`（#777）を追記 |
| 1.93 | 2026-10-17 | 分割章の表に Paper 実績 MCP（`paper-results-mcp.md`）を追加。画面コンポーネントは増やさない | issue #783 |
| 1.94 | 2026-10-19 | §2に`organisms/mcp`と`copy-button/`を、§3に`mcp.Panel`（Settings画面の MCP カード/モーダル。状態・保存・API キーの一度きりの表示と伏せ字）を追記 | issue #792 |
| 1.95 | 2026-10-19 | メインナビに「管理」（`NavAdmin`、`/admin`）を追加し、管理ハブ（`AdminPage`・`AdminHub`）を organisms/pages に追加。ナビ構造・画面一覧・戻り導線の規則を分割章`docs/components/screens.md`に新設 | issue #793 |
| 1.96 | 2026-10-19 | §3の`mcp.Panel`に、状態の 起動中・エラー、自動起動の表記、起動・停止・再起動ボタンを追記 | issue #795 |
| 1.97 | 2026-10-20 | organismsに`HealthAlertBanner`を追加し、`Header`に`#health-alert-banner`枠（`GET /system/health-alerts`）を追記 | issue #802 |
| 1.98 | 2026-10-20 | 管理ハブ（`AdminPage`・`AdminHub`・`NavAdmin`）を廃止して Settings に統合: `SettingsHubPage`（`GET /settings`のページ一覧）・`molecules.SettingsLinkCard`・`SettingsPage`の1ページ化（`GET /settings/<page id>`）を追加し、メインナビは Settings 1本、`pitha-settings-hub`で旧ハッシュ直リンクを転送 | issue #805 |
| 1.99 | 2026-10-20 | §3の`mcp.Panel`に、設定変更の許可チェックと監査ログ（`POST /settings/mcp/write`、`router.WithMCPWriteGate`）を追記 | issue #806 |
| 1.100 | 2026-10-20 | `SettingGroupProps`に`DerivedTitle`（読み取り専用パネルの見出し。空は「計算結果」）を追加し、Settings「メタラベリング」グループが同パネルに「LLM cutoffの状態と確認手順」（状態＝仮置き/未確認/確認済み、記録済みの日付・モデル名・確認日・出典、公式日付の確認ヘルプ）を表示 | issue #809 |
| 1.101 | 2026-10-20 | Settings の7つ目のページ「資産ごとの予算枠」（`asset-buckets`）を、`SettingsHubPage`の一覧に追記。同ページのグループ`asset-buckets`は`SettingGroupProps.Derived`に運用資金・枠の合計・残額（円）を表示する（`SettingGroupDerived`） | issue #824 |
| 1.102 | 2026-10-21 | §3の`mcp.Panel`に、注文案の受け付けスイッチ（`POST /settings/mcp/order-proposals`。`WriteGate`の`OrderProposalsEnabled`）を追記 | issue #827 |
| 1.103 | 2026-10-21 | §3/§4に注文案画面（`GET /order-proposals`・`/order-proposals/:id`、`pages.OrderProposalsPage`・`OrderProposalDetailPage`、`organisms.NavOrderProposals`、`handler/orderproposals`）を追加。読み取り専用 | issue #829 |
| 1.104 | 2026-10-23 | メインナビを「ホーム／候補銘柄／AIの注文提案／成績／（区切り）／設定」の5項目・日本語ラベルに変更（`organisms.NavItem` は `NavHome`・`NavScanner`・`NavOrderProposals`・`NavPerformance`・`NavSettings`。`NavWatchlist`・`NavCalibration`・`NavActivity` は廃止し、監視リスト・銘柄詳細は `NavScanner`、予測の精度は `NavPerformance`、稼働ログは `NavSettings`）。`molecules.TabNav`（候補銘柄・成績のタブ）、`pitha-performance-tabs`（旧 `#backtest` の転送）を追加。Settings を6ページに再編（`SettingsHubPage` が `HubLinks()` を併記）。状態バッジ（`StatusDot`）と `pitha-kill-switch-panel` のボタンを日本語化し、状態の文字列は Header のバッジ1つに統一。各ページの見出し・指標名を日本語化（英語名は `title` に残す）。詳細は `docs/components/screens.md` | issue #895（版は origin/main の最新 1.103 の次。他の未マージ PR と重なる場合はマージ時に採番し直す） |
