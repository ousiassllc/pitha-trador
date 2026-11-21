# コンポーネント設計: 改訂履歴

`docs/components/overview.md` から分割した改訂履歴。`overview.md`・`lit.md`・`runtime.md` の改訂を、本ファイルへ1行ずつ追記する（`.linterly.yml` の300行/ファイル制限のため分割）。`screens.md` と `paper-results-mcp.md` の改訂履歴は各ファイルにある。

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
| 1.104 | 2026-10-23 | メインナビを「ホーム／候補銘柄／AIの注文提案／成績／（区切り）／設定」の5項目・日本語ラベルに変更（`organisms.NavItem` は `NavHome`・`NavScanner`・`NavOrderProposals`・`NavPerformance`・`NavSettings`。`NavWatchlist`・`NavCalibration`・`NavActivity` は廃止し、監視リスト・銘柄詳細は `NavScanner`、予測の精度は `NavPerformance`、稼働ログは `NavSettings`）。`molecules.TabNav`（候補銘柄・成績のタブ）、`pitha-performance-tabs`（旧 `#backtest` の転送）を追加。Settings を6ページに再編（`SettingsHubPage` が `HubLinks()` を併記）。状態バッジ（`StatusDot`）と `pitha-kill-switch-panel` のボタンを日本語化し、状態の文字列は Header のバッジ1つに統一。各ページの見出し・指標名・表の列見出し・エラー画面の見出しを日本語化（英語名は `title` に残す）。旧 `/settings/<old page>#modal-*` は各 Settings ページが転送し、`pitha-performance-tabs` は実績タブだけで読み込む。詳細は `docs/components/screens.md` | issue #895 |
| 1.105 | 2026-10-23 | `GET /`をHome画面（`pages.HomePage`、`organisms/homepanels`、`handler/home`）にし、ヘッダーのロゴのリンク先を`/`、五つのバナー枠を`#attention-badge`（`organisms.AttentionBadge`、`GET /home/attention-badge`）に集約。§4に要対応バッジとHomeのカード更新（30秒）を追記し、バナーのフラグメントはヘッダーから呼ばなくなった旨を追記。`organisms.ScanFunnel`・`FormatNextOpen`・`FormatCycleDuration`・`HealthAlertTitle`/`HealthAlertAction`を公開 | issue #896（#899の上に積む） |
| 1.106 | 2026-10-23 | `handler/home`の`Source`に`OpenPositions`を追加し（`PositionReader`は廃止）、`homeview`が読み取り専用Txで`closed_at IS NULL`を読む。ヘルスの読み取りを数秒間フラグメント間で共有。`systemStateChanged`のヘッダー購読は`from:body`に統一 | issue #896（#899の上に積む。レビュー修正） |
| 1.107 | 2026-10-23 | Homeの`#home-attention`（要対応の一覧）を、旧バナー枠と同じ固定のライブリージョン（`role="status"` `aria-live="polite"`、`data-live-banner`）にした。一覧の本文は自前の`role`を持たない | issue #896（#899の上に積む。旧バナー枠のライブリージョンの検査を新画面に移す） |
| 1.108 | 2026-10-24 | atomsに`HelpMark`を追加し、全画面の説明文・制約・注意書きをヘルプマークへ移した。Issue・PR・FRの番号、設定キー名、内部コードの画面表示を削除（キー名はヘルプの中）。成績画面の4部品（`BacktestForm`・`BacktestFoldTable`・`PerformanceActualsPanel`・`PerformanceSummaryPanel`）を`organisms/performancepanels`へ分割（`organisms/`の行数上限のため。挙動の変更なし）。ルート・API・MCP・設定キー・DBの変更なし | issue #902 |
| 1.109 | 2026-10-24 | organismsから`SecretsBanner`・`UpdateBanner`・`MarketDataBanner`・`DailyHaltBanner`・`HealthAlertBanner`と、対応するフラグメントのルート（`GET /system/update-status`・`/system/secrets-status`・`/system/marketdata-status`・`/system/daily-halt`・`/system/health-alerts`）を削除。`MarketDataBannerProps`・`HealthAlertTitle`/`HealthAlertAction`・`UpdatePanel`は残す（Homeの読み取り側）。§4の旧バナー各項を、要対応の読み取り元と未設定一覧の更新に書き換え | issue #901 |
| 1.110 | 2026-10-26 | `internal/web/labels`（画面に出す内部コードの日本語名の表。Lit側は`static/src/components/lib/labels.ts`で同じ表を持ち、`TestTypeScriptLabelsMatchGo`が一致を検査する）を追加。決済理由・Kill Switchの理由・見送り理由・注文提案の理由コード・稼働ログの種別・キュー・詳細・再学習の見送り・A/Bアームを日本語で表示し、表にない値は英語のままでなく「その他（理由コードあり）」を出して元の値をログに残す。`RiskPanel`/`JevPanel`の見出し・予測の精度の見出し・成績のヘルプの英語を日本語化し、ヘルプと画面から設定キー名・MCPツール名・設定ファイル名を除去（`SecretFieldRow`は説明のある項目だけヘルプを持つ）。`ScanFunnel`の項目名を平易に変更。記録の番号は「注文提案 5 番」の形に統一（`#`を付けない）。data属性・API・MCP・設定キー・DB・URLの変更なし | issue #904 |
| 1.111 | 2026-11-06 | ライト/ダークを`<html data-theme>`（`light`か`dark`）で明示的に切り替えられるようにした（未指定はOS設定に追従）。`molecules.ThemeToggle`（`Header`に配置）、`pitha-theme-toggle`、`vendor/theme-init.js`、`lib/theme.ts`を追加し、選択はlocalStorageに保存する。チャートは`lib/chart-theme.ts`の`ColorSchemeController`がOS切替に加えテーマ切替イベントでも色を読み直す | issue #978 |
| 1.112 | 2026-11-06 | §2に`confirm/`・`ripple/`・`lib/`の各モジュール・`chart-colors.ts`・`css/table.css`、atomsに`Checkbox`・`InlineAlert`、moleculesに`ConfirmDialog`を追記。Kill の確認を`askConfirm`、フォームの値の拒否を422＋`atoms.InlineAlert`、トーストの自動消去を3秒に直した | refine-git レビュー指摘（実装との乖離） |
| 1.113 | 2026-11-06 | §3の`Switch`の説明を実装に合わせ、保存ボタン付きのフォーム内で使う設定の on/off のスイッチと訂正（旧: 即時反映）。`design-tokens.md`の部品の節も同じ | refine-git レビュー（doc-drift） |
| 1.114 | 2026-11-08 | §3の`Toast`に`atoms.ReloadNotice`（永続の再読み込み通知）を、`runtime.md` §8にその挙動（領域・解除・読み上げ）を追記。§2の`htmx-errors/`に`reload-notice.ts`、moleculesの`Modal`/`ConfirmDialog`に`[data-reload-notice-region]`、`layout`に`#reload-notice-region`。5秒で消えるトーストの後も「再読み込み」を画面に残す | PR #1044 M5（#1017 の受け入れ条件は変えず、WCAG 2.2.1 の懸念に対応） |
| 1.115 | 2026-11-22 | §2のツリーに`help-mark/`・`performance-tabs/`・`migration/`（いずれもLitではない）と`lib/help-mark.ts`・`lib/labels.ts`を追記。改訂履歴を`history.md`へ分割し（`overview.md`の行数を300行以下にするため）、分割章の表に追加 | issue #1157 |
| 1.116 | 2026-11-22 | §2の`internal/web`ツリーの`handler/`に`home/`・`trialresults/`・`jevprompt/`・`jevtrial/`・`airoles/`・`mcpsettings/`・`export/`・`migration/`、`organisms/`のサブパッケージ（`airoles/`・`jevprompt/`・`jevtrial/`・`homepanels/`・`trialresults/`・`evaluation/`・`performancepanels/`・`organismstest/`）を追記し、§3に`airoles.Panel`・`jevtrial.Panel`・`jevprompt.Panel`を追加 | issue #1168 |
| 1.117 | 2026-11-22 | §1の技術表と`lit.md` §5冒頭のLitコンポーネント数を5種から6種に訂正（`pitha-exit-alignment`を追加した時に更新が漏れていた） | issue #1159 |
| 1.118 | 2026-11-22 | §2のツリーに、行数上限のために分離したファイルを追記: `handler/settings/settings_options.go`・`handler/mcpsettings/experiments_format.go`・`organisms/mcp/panel_attrs.go`・`pages/settings_ai.templ`（`settingsHelp`を`settings_page.templ`から移動）・`price-chart/price-chart-styles.ts`・`calibration-heatmap/{bucket-grid-view,scores-view}.ts`・`css/motion.css`（`app.css`から分離）・`lib/testing/reconnect-capture.ts`（固定時間の待ちをやめる共有テストヘルパー）・`*.stale.test.ts`（価格チャート・スキャナー表の初期取得の古い応答を捨てる回帰テスト）。`chart-data.ts`の説明を実装に合わせた | issue #1164・#1165・#1166 |
| 1.119 | 2026-11-22 | `lit.md` §5.5の`pitha-activity-feed`の世代ガードを実装に合わせて訂正（世代の管理は`activity-feed-merge.ts`の`InFlightWs`で、スナップショットとKill Switch履歴の2本。Kill Switch履歴も対象。WSで届いた差分は応答の上へ重ねて二重に載せない。旧記述の世代番号の変数名は削除）。§6の`lib/api.ts`に204は`undefined`で解決する旨を追記。§2のツリーの`activity-feed/`に`activity-feed-merge.ts`・`pitha-activity-feed.stale.test.ts`を追記 | issue #1198 |
