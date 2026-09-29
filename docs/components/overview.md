# コンポーネント設計

HALT（HTMX + Atomic Design + Lit + Templ）に基づくフロントエンドアーキテクチャを、Wailsによるネイティブデスクトップシェルの上で構成する。設計思想の詳細は `skill://halt/references/architecture.md` を踏襲し、本ドキュメントはプロジェクト（pitha-trador）固有の適用を記述する。

## 1. フロントエンドアーキテクチャ概要

- フロントエンドはAPIサーバー（Gin）の中で動く。SPAは作らない
- サーバー（Go）がUI制御を握り、HTMXでハイパーメディア駆動のインタラクションを実現する
- リッチなインタラクション（チャート・ライブテーブル・Kill Switch操作）が必要な箇所だけ Lit Web Components（`pitha-*`）で拡張する
- テンプレートは Atomic Design（atoms/molecules/organisms/pages）で構造化する
- **HATEOAS**: サーバーが現在の状態（Running/Paused/Killed・保有ポジション有無・権限）に基づき、利用可能なアクションのみをHTML/属性として出力する。ボタンは「見えるなら押せる」。`hidden`/`disabled`で隠すのではなくレンダリングしない
- WailsのWebView2は、Gin Engine を `AssetServer.Handler` として注入されたローカルプロセス内アセットサーバーにのみアクセスする（`architecture/overview.md` §7）

### 技術スタック

| レイヤー | 技術 | 役割 |
|---------|------|------|
| デスクトップシェル | Wails v2 | ネイティブウィンドウ・トレイ・通知（本ドキュメントでは§7で詳述） |
| サーバーフレームワーク | Gin | ルーティング＋SSR |
| APIフレームワーク | Huma | `/api/v1/...` のJSON API・OpenAPI 3.1自動生成 |
| テンプレートエンジン | Templ | 型安全なGo HTMLテンプレート |
| インタラクション | HTMX | サーバー駆動のDOM更新 |
| リッチUI | Lit (Web Components) | `pitha-price-chart` 等4種（§5） |
| チャート描画 | lightweight-charts (TradingView製) | ローソク足・VWAP・出来高・信頼性曲線 |
| スタイリング | Tailwind CSS | ユーティリティファーストCSS |
| ビルド | esbuild | Lit/TypeScriptバンドル |

## 2. ディレクトリ構成

```text
internal/web/
├── handler/            # scanner.go, symbol.go, performance.go, calibration.go, system.go
├── middleware/         # CSRF, ロギング, リカバリ, Setup Guard（必須認証情報未設定時に`/setup`へ302、issue #80）
├── atoms/
├── molecules/
├── organisms/
├── pages/
└── layout/

static/
└── src/
    ├── components/
    │   ├── price-chart/           pitha-price-chart.ts
    │   ├── scanner-table/         pitha-scanner-table.ts
    │   ├── calibration-heatmap/   pitha-calibration-heatmap.ts
    │   ├── activity-feed/         pitha-activity-feed.ts
    │   ├── kill-switch-panel/     pitha-kill-switch-panel.ts
    │   └── lib/
    │       ├── api.ts
    │       ├── ws.ts
    │       └── logger.ts
    ├── css/
    │   └── app.css
    └── dist/
        ├── js/
        └── css/
```

依存ルールは `architecture/overview.md` §3 の通り（`handler → service → repository → domain`、Templ側は `atoms/molecules/organisms/pages`）。

## 3. Templ テンプレート（Atomic Design）

### atoms

- `Badge`（Direction: LONG/SHORT/NONE、Regime: TREND/RANGE/BREAKOUT/CHAOTIC の色分け表示）
- `StatusDot`（システム状態: Running=緑 / Paused=黄 / Killed=赤）

> **未実装コンポーネントの扱い（issue #120）**: 現状のアプリは`Button`/`Input`/`Select`/`Spinner`/`Toast`/`Card`/`Modal`/`OrderRow`/`ConfidenceBucketBar`/`Sidebar`/`CalibrationBucketTable`のいずれも必要としない（ボタン・入力はTailwindユーティリティを各テンプレートに直接記述、Kill Switch確認は`pitha-kill-switch-panel`内の`window.confirm`、エラーは各画面/コンポーネント内の`role="alert"`表示、ナビゲーションは`Header`、Calibration帯別の表示は`pitha-calibration-heatmap`が担う）。これらは実装せず、**利用箇所が生じた時点で対応するレイヤに追加する**（同一の見た目・属性が複数テンプレートで重複した時点が`Button`/`Input`等の切り出しの目安）。§4・`api/endpoints.md`で言及する「確認モーダル」「トースト」も、現状はそれぞれ`window.confirm`・インラインの`role="alert"`/`role="status"`表示で実現している。

### molecules

- `SecretFieldRow`（Settings画面の1項目。ラベル・「設定済み」バッジ・値入力（`type=password`）と保存ボタン・削除ボタン（設定済みのときのみ）・直近の保存/削除結果の通知を持ち、保存は`POST /settings/:key`、削除は`DELETE /settings/:key`で行の`outerHTML`のみ差し替える。他項目の値には影響しない。issue #79）
- `SignalBadgeGroup`（direction + confidence + entry_quality の組み合わせ表示）
- `PositionRow`

### organisms

- `Header`（ナビゲーション＋`StatusDot`。Kill Switch状態のOOB更新対象。`middleware.SystemStateFrom`の現在状態から`KillSwitchPanel`を描画する）
- `KillSwitchPanel`（`pitha-kill-switch-panel`を、現在状態に基づく`status`/`can-pause`/`can-resume`/`can-kill`と各URL属性付きで出力する。issue #106）
- `ScannerTableFallback`（JS無効時/初回SSR描画用の候補銘柄テーブル。ハイドレーション後は`pitha-scanner-table`が引き継ぐ）
- `DecisionHistoryList`（Jev判断履歴の時系列リスト）
- `PerformanceSummaryPanel`
- `UpdateBanner`（新バージョン検知時の全ページ共通通知バナー。`Header`内`#update-banner`が`GET /system/update-status`を`hx-trigger="load, every 60s, updateStatusChanged from:body"`で取得。安全ゲート待ち（`Blocked`）・インストーラー準備完了（`Ready`）を文言で区別し、新バージョンが無ければ描画しない、issue #76）
- `UpdatePanel`（Settings画面の「アップデート」節。現在バージョン・最終確認結果・「今すぐアップデートを確認」ボタン（`POST /system/update-check`、`#update-panel`をinnerHTMLスワップ）、issue #76）
- `SecretsBanner`（SLACK_WEBHOOK_URL等の任意キー未設定時の全ページ共通案内バナー。必須3キーはSetup Guardが`/setup`へ誘導するため対象外。`Header`内`#config-banner`が`GET /system/secrets-status`をhx-trigger="load"で自己補正取得する、issue #57/#80）
- `QueueStatusPanel`（`jobs`テーブルのキュー別pending/running/直近failed件数を表示。System Activity Logのほか、将来Headerへの常時表示も想定）
- `ActivityFeedFallback`（JS無効時/初回SSR描画用のアクティビティ一覧テーブル。ハイドレーション後は`pitha-activity-feed`が引き継ぐ）

### pages

- `ScannerPage`
- `SymbolDetailPage`（`pitha-price-chart` アイランドを埋め込む）
- `PerformancePage`
- `CalibrationPage`（`pitha-calibration-heatmap` アイランドを埋め込む）
- `SettingsPage`（JEV_API_KEY/JEV_BASE_URL/KABU_API_PASSWORD/SLACK_WEBHOOK_URLに加え、任意のLUNA_*/NEWS_FEED_*（およびSOL_*/OPUS_*）入力項目を`SecretFieldRow`でフィールド数だけ縦に並べる。値は再表示せず設定済み状態のみ表示し、項目ごとに独立して`secrets`テーブルへ暗号化保存・削除する。issue #57/#79）
- `SetupPage`（初回セットアップ画面。必須3キー＋任意のSLACK_WEBHOOK_URLを`SecretFieldRow`で表示し、保存・削除は`POST`/`DELETE /settings/:key`を共用する。必須3キーがすべて設定済みなら完了表示と`/scanner`へのリンクを出す。`Header`を含まない`layout.SetupShell`で描画。`requirements/functional.md` §4.18、issue #80）
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
- アクションルート（`/system/pause`, `/system/resume`, `/system/kill`, `/positions/:id/close`）は常にフラグメントを返す
- **OOB更新**: システム状態変更（pause/resume/kill）はメインレスポンスに加え、Headerの`StatusDot`をOOBスワップで更新する。用途はこの「副作用の反映」のみに限定する
- **ローディング**: Kill Switch実行ボタンは`hx-disabled-elt="this"`で二重発動を防止し、`hx-indicator`でスピナーを表示する。スケルトンスクリーンは使わない
- **エラー表示**: `response-targets`拡張を使い、422（バリデーション）と5xx（予期しないエラー）で表示先を分離する
- **アップデート通知**: `Header`内`#update-banner`は`GET /system/update-status`を`load`・60秒周期・`updateStatusChanged`イベントで取得し、`UpdateBanner`または何も描かない。Settings画面の`#update-panel`は「今すぐアップデートを確認」（`POST /system/update-check`）の応答で置き換わり、応答の`HX-Trigger: updateStatusChanged`でHeaderのバナーも即時更新される（issue #76）
- **フィールド単位保存**: Settings画面は1つの一括フォームではなく、`SecretFieldRow`ごとの独立フォームで保存（`hx-post="/settings/:key"`）・削除（`hx-delete="/settings/:key"`、`hx-confirm`で確認）し、応答の行フラグメントで当該行のみを差し替える。空入力の保存は400で、値の削除は明示的な削除操作でのみ行う（issue #79）
- **未設定バナー**: `Header`内`#config-banner`は`GET /system/secrets-status`を`hx-trigger="load"`で取得し、`SecretsBanner`（任意キー（SLACK_WEBHOOK_URL等）の未設定一覧＋`/settings`リンク）またはnothingを描く。必須3キーはバナーではなくSetup Guardの`/setup`リダイレクトで扱う。`#header-status`と同じSSR空→自己補正パターン（issue #57/#80）
- **初回セットアップ誘導**: Setup Guard Middlewareが必須3キー未設定の間`/setup`以外（`POST`/`DELETE /settings/:key`・`/static/...`を除く）を302で`/setup`へ送る。`SetupPage`は`Header`を持たない`layout.SetupShell`で描画し、ガード対象の`hx-get`フラグメントを発火させない。保存はSettingsと同じ`SecretFieldRow`の`hx-post="/settings/:key"`を使い、3キーが揃った時点で完了表示と`/scanner`への「続ける」リンクを出す（issue #80）

## 5. Lit Web Components 仕様

命名規則: `pitha-{feature-name}`。以下5種を実装する（HTMXでは実現できないCanvas描画・高頻度WebSocket更新を要するため）。

| コンポーネント | 用途 | 埋め込みページ | データソース |
|---------------|------|---------------|------------|
| `pitha-price-chart` | ローソク足＋VWAP＋出来高チャート、Jevシグナルのマーカー表示 | Symbol Detail | `GET /api/v1/symbols/{symbol}/candles`（初期）＋ `/ws/symbols/{symbol}`（ライブ） |
| `pitha-scanner-table` | 候補銘柄のソート可能なライブテーブル | Scanner Dashboard | `GET /api/v1/scanner`（初期）＋ `/ws/scanner`（15〜30秒更新） |
| `pitha-calibration-heatmap` | confidence帯別 reliability curve / 的中率ヒートマップ | Calibration | `GET /api/v1/calibration` |
| `pitha-activity-feed` | キュー別pending/running/failed件数・直近アクティビティ一覧・直近Kill Switchイベントのライブ表示（type/queueフィルタ付き） | System Activity Log | `GET /api/v1/activity`（初期・フィルタ変更時）＋ `/ws/activity`（ライブ） |
| `pitha-kill-switch-panel` | システム状態表示・Kill Switch発動/解除操作 | 全ページ共通（Header内アイランド） | `GET/POST /api/v1/system/*` |

### 5.1 pitha-price-chart

```typescript
@customElement('pitha-price-chart')
export class PithaPriceChart extends LitElement {
  @property({ type: String, attribute: 'symbol' }) symbol = '';
  @property({ type: String, attribute: 'candles-url' }) candlesUrl = '';
  @property({ type: String, attribute: 'ws-url' }) wsUrl = '';

  @state() private chart: IChartApi | null = null;
  @state() private error: string | null = null;

  connectedCallback() {
    super.connectedCallback();
    this.initChart();   // lightweight-charts でローソク足/VWAPライン/出来高ヒストグラムペインを初期化
    this.loadInitial();  // candlesUrl から初期系列を取得（lib/api.ts経由）
    this.subscribeWs();  // wsUrl から tick / jev_update を受信し系列・マーカーを更新
  }

  disconnectedCallback() {
    super.disconnectedCallback();
    this.chart?.remove();
    this.wsClient?.close();
  }
}
```

- Jevの`direction`変化・`entry_quality`更新はチャート上のマーカー（例: LONG転換で上向き矢印）として描画する
- `symbol`属性が変化した場合（同一ページ内で銘柄を切り替えるUIを将来追加する場合）は`updated()`ライフサイクルで再購読する

### 5.2 pitha-scanner-table

- 初期データを`GET /api/v1/scanner`で取得しレンダリング、以後`/ws/scanner`のPUSHで行を更新・ソート順を再計算する
- 列ヘッダクリックでクライアント内ソート（サーバー往復不要）
- 銘柄行は通常の `<a href="/symbols/{symbol}">` として描画する（Litはハイパーメディアリンクの外側に出ず、通常のブラウザナビゲーションとしてページ遷移する。HTMXリクエストは発火しない = HTMX↔Lit境界ルール§「Litは HTMXリクエストをトリガーしない」に準拠）

### 5.3 pitha-calibration-heatmap

- `GET /api/v1/calibration`のバケット別データからreliability curve（lightweight-chartsのラインシリーズ）とconfidence帯別カラーヒートマップを描画する
- リアルタイム性は不要なため WebSocket は使用しない。ページ再訪問時・手動更新ボタン押下時に再フェッチする

### 5.4 pitha-kill-switch-panel

```typescript
@customElement('pitha-kill-switch-panel')
export class PithaKillSwitchPanel extends LitElement {
  // HATEOAS: サーバーが現在の状態・操作可否・呼び出すURLを属性で注入する。
  // コンポーネントはURLも遷移表も持たない（操作後・再同期後はAPI応答の can_* を採用する）
  @property({ type: String }) status: 'running' | 'paused' | 'killed' | '' = '';
  @property({ type: Boolean, attribute: 'can-pause' }) canPause = false;
  @property({ type: Boolean, attribute: 'can-resume' }) canResume = false;
  @property({ type: Boolean, attribute: 'can-kill' }) canKill = false;
  @property({ type: String, attribute: 'pause-url' }) pauseUrl = '';
  @property({ type: String, attribute: 'resume-url' }) resumeUrl = '';
  @property({ type: String, attribute: 'kill-url' }) killUrl = '';
  @property({ type: String, attribute: 'status-url' }) statusUrl = '';
  @property({ type: String, attribute: 'ws-url' }) wsUrl = '';
}
```

```go
// Templ側（organisms.Header → organisms.KillSwitchPanel）。
// state は middleware.SystemStateFrom(ctx)（domain.SystemState、読み出せない場合は ""）
templ KillSwitchPanel(state domain.SystemState) {
    <pitha-kill-switch-panel
        status={ string(state) }
        can-pause?={ state.CanPause() }
        can-resume?={ state.CanResume() }
        can-kill?={ state.CanKill() }
        pause-url="/api/v1/system/pause" resume-url="/api/v1/system/resume"
        kill-url="/api/v1/system/kill" status-url="/api/v1/system/status"
        ws-url="/ws/system">
    </pitha-kill-switch-panel>
}
```

- 操作可否は`domain.SystemState.CanPause/CanResume/CanKill`（Running→pause/kill可、Paused→resume/kill可、Killed→resumeのみ）が唯一の定義で、`GET/POST /api/v1/system/*`の応答も`can_pause`/`can_resume`/`can_kill`として返す。状態を読み出せないとき`Header`は操作ボタンを描画せず（`status=""`）、パネルが`status-url`から自己補正する
- 操作後は`systemStateChanged`を発火し、`Header`の`#header-status`（`hx-trigger="systemStateChanged from:closest header"`）がStatusDotを再取得する
- Kill Switch発動はRisk Engineからも直接トリガーされうる（`architecture/overview.md` §8.3）。この場合はサーバー側が`/ws/system`経由で`kill_switch`イベントを配信し、`pitha-kill-switch-panel`が受信して`status`をローカルに反映（操作可否は`status-url`から再取得）しつつ、同様に`systemStateChanged`を発火してHeaderと同期させる。`/ws/system`が切断されている間は「接続が切れています」を表示し、再接続後に`status-url`から再同期する（§6）

### 5.5 pitha-activity-feed

- 初期データを`GET /api/v1/activity`で取得しレンダリングし、以後`/ws/activity`の`job_update`（該当キューの件数のみ置換）・`activity_event`（フィード先頭に追加、最大500件で切り詰め）を反映する
- type/queueセレクトの変更時は`GET /api/v1/activity?type=&queue=`で再取得する（サーバー側フィルタ。`queue`指定は当該キューの`job`イベントのみに一致）。WS受信イベントも同じ条件でクライアント側で絞り込む
- 直近Kill Switchイベントは`?type=kill_switch&limit=10`で別途取得し、WSの`kill_switch`イベントで先頭に追加する
- SSRフォールバック（`QueueStatusPanel` + `ActivityFeedFallback`）を子要素として持ち、ハイドレーション時に置き換える（`pitha-scanner-table`と同じ light DOM 方式）

## 6. API クライアント / WebSocket（`lib/`）

`lib/api.ts`（CSRFトークンをmetaタグから自動取得、`credentials: 'same-origin'`、JSON自動パース）:

```typescript
get<T>(path: string): Promise<T>
post<T>(path: string, body?): Promise<T>
put<T>(path: string, body?): Promise<T>
patch<T>(path: string, body?): Promise<T>
del<T>(path: string): Promise<T>
```

`lib/ws.ts`（自動再接続、指数バックオフ（〜30秒）、JSONメッセージパース（不正なJSONは`logger.warn`して破棄）、`onOpen`/`onMessage`/`onClose`/`onStatusChange`コールバック）。`onStatusChange`は`connecting`/`open`/`reconnecting`/`failed`を通知する。`failed`は連続10回の再接続失敗後で、以降も30秒間隔で無期限に再試行する。`WsClient`を持つ`pitha-kill-switch-panel`/`pitha-scanner-table`/`pitha-activity-feed`は`reconnecting`/`failed`の間、`lib/ws-status.ts`の「接続が切れています」通知（`role="status"`）を表示する（issue #133）。4種のLitコンポーネントは共通してこの2ファイルのみを経由し、`fetch()`/`new WebSocket()`を直接呼ばない。

`lib/logger.ts`: 構造化ログをブラウザ（WebView）コンソールへ出力し、致命的エラーは将来的にGoバックエンドへ送信できるようフックポイントを用意する（MVPではコンソール出力のみ）。

## 7. Wails統合

- `cmd/desktop/main.go` がGin Engineを組み立て、`options.App.AssetServer.Handler` に注入してWailsを起動する。フロントエンドは通常のWailsテンプレート（`frontend/`ディレクトリ・独自バインディング）を使わず、`static/src`のビルド成果物をGinの`Static()`で配信する
- Kill Switch発動等、サーバー内部イベントをネイティブ通知として表示する処理（`runtime.EventsEmit`, OSトースト, トレイアイコン変更）は `internal/web/handler` ではなく `internal/service/risk` からWailsランタイムを直接呼び出す薄いアダプタ（`internal/service/notify`）を介して行う
- 開発ワークフロー（3種のウォッチプロセスを並行起動、`Makefile dev`ターゲット）:

```makefile
.PHONY: dev
dev:
	@bunx concurrently \
		"wails dev" \
		"templ generate --watch" \
		"bun --cwd static run dev"
```

  - `.templ`編集 → `templ generate --watch`が`_templ.go`を再生成 → `wails dev`がGoファイル変更を検知しプロセス再起動（WebViewは自動リロード）
  - `.ts`編集 → esbuildがバンドル → `static/dist`更新 → WebViewはHTTPキャッシュなし設定のため次回リクエストで反映（手動リロードまたは`hx-boost`遷移で反映）
  - esbuildのエントリは`static/src/components/*/pitha-*.ts`をglobで自動列挙し（`lib/*.ts`は各コンポーネントからimportされるためエントリにしない）、`splitting: true`（ESM）でLit等の共有コードを`dist/js/chunks/`へ切り出す。全ページ共通の`pitha-kill-switch-panel`と各ページのコンポーネントでLitが二重にロードされることはない
  - `.css`編集 → TailwindがビルドしてSPAリロード不要で反映

- ビルド・配布: `wails build` で単一のWindows実行ファイル（`.exe`）を生成する。DBはSQLite（アプリ内蔵、`modernc.org/sqlite`）のため、Postgres等の外部DBサービスを事前にインストール・起動しておく必要はない。初回起動時に`db/migrations`を自動適用しDBファイルを生成する。Wails v2のWindowsターゲットとDBドライバ（`modernc.org/sqlite`, `modernc.org/sqlite/vec`）はいずれもpure Go実装のためCGO不要であり、`wails build -platform windows/amd64`はLinux CIランナー上でもそのままクロスビルドできる（`environment/setup.md` §CI/CD参照）

## 8. エラーハンドリング（要約）

- HTMX: サーバーがエラーUIもHTMLで返す（422はフォーム再レンダリング、ビジネスエラーはOOBトースト、5xxはグローバル`htmx:responseError`リスナーで汎用トースト）
- Lit: JSON APIを使うため自前でtry/catchしコンポーネント内にエラー状態をレンダリングする
- Huma API: バリデーションエラーはRFC 7807 Problem Details形式で自動生成される

## 9. テスト戦略

| レイヤー | テスト手法 | 検証内容 |
|---------|----------|---------|
| Go ハンドラ | `httptest` + HTMLアサーション | 正しいHTMLフラグメント/フルページ・ステータスコード |
| Templ テンプレート | `Render()` → HTML文字列アサーション | atoms/molecules/organisms/pagesの出力 |
| Lit コンポーネント | `@open-wc/testing` + `@web/test-runner` | チャート初期化・WS再接続・Kill Switch操作等の単体挙動 |
| E2E | Playwright（Wailsアプリのwebview、またはビルド前は`wails dev`のブラウザアクセスモード） | Scanner→Symbol Detail遷移、Kill Switch操作フロー全体 |

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
