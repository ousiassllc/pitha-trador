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
├── middleware/         # CSRF, ロギング, リカバリ
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

- `Button`, `Input`, `Select`
- `Badge`（Direction: LONG/SHORT/NONE、Regime: TREND/RANGE/BREAKOUT/CHAOTIC の色分け表示）
- `StatusDot`（システム状態: Running=緑 / Paused=黄 / Killed=赤）
- `Spinner`, `Toast`

### molecules

- `Card`, `Modal`（Kill Switch確認モーダル）
- `SignalBadgeGroup`（direction + confidence + entry_quality の組み合わせ表示）
- `PositionRow`, `OrderRow`
- `ConfidenceBucketBar`（Calibration帯別バーの単純表示。詳細な曲線描画は`pitha-calibration-heatmap`側）

### organisms

- `Header`（ナビゲーション＋`StatusDot`。Kill Switch状態のOOB更新対象）
- `Sidebar`
- `ScannerTableFallback`（JS無効時/初回SSR描画用の候補銘柄テーブル。ハイドレーション後は`pitha-scanner-table`が引き継ぐ）
- `DecisionHistoryList`（Jev判断履歴の時系列リスト）
- `PerformanceSummaryPanel`
- `CalibrationBucketTable`

### pages

- `ScannerPage`
- `SymbolDetailPage`（`pitha-price-chart` アイランドを埋め込む）
- `PerformancePage`
- `CalibrationPage`（`pitha-calibration-heatmap` アイランドを埋め込む）

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

- ページルート（`/scanner`, `/symbols/:symbol`, `/performance`, `/calibration`）はHX-Requestヘッダで フルページ/フラグメント を分岐する
- アクションルート（`/system/pause`, `/system/resume`, `/system/kill`, `/positions/:id/close`）は常にフラグメントを返す
- **OOB更新**: システム状態変更（pause/resume/kill）はメインレスポンスに加え、Headerの`StatusDot`をOOBスワップで更新する。用途はこの「副作用の反映」のみに限定する
- **ローディング**: Kill Switch実行ボタンは`hx-disabled-elt="this"`で二重発動を防止し、`hx-indicator`でスピナーを表示する。スケルトンスクリーンは使わない
- **エラー表示**: `response-targets`拡張を使い、422（バリデーション）と5xx（予期しないエラー）で表示先を分離する

## 5. Lit Web Components 仕様

命名規則: `pitha-{feature-name}`。以下4種を実装する（HTMXでは実現できないCanvas描画・高頻度WebSocket更新を要するため）。

| コンポーネント | 用途 | 埋め込みページ | データソース |
|---------------|------|---------------|------------|
| `pitha-price-chart` | ローソク足＋VWAP＋出来高チャート、Jevシグナルのマーカー表示 | Symbol Detail | `GET /api/v1/symbols/{symbol}/candles`（初期）＋ `/ws/symbols/{symbol}`（ライブ） |
| `pitha-scanner-table` | 候補銘柄のソート可能なライブテーブル | Scanner Dashboard | `GET /api/v1/scanner`（初期）＋ `/ws/scanner`（15〜30秒更新） |
| `pitha-calibration-heatmap` | confidence帯別 reliability curve / 的中率ヒートマップ | Calibration | `GET /api/v1/calibration` |
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
  // HATEOAS: サーバーが現在の状態・操作可否を属性で注入する
  @property({ type: String }) status: 'running' | 'paused' | 'killed' = 'running';
  @property({ type: Boolean, attribute: 'can-pause' }) canPause = false;
  @property({ type: Boolean, attribute: 'can-resume' }) canResume = false;
  @property({ type: Boolean, attribute: 'can-kill' }) canKill = false;

  private async onKill() {
    await api.post('/api/v1/system/kill');
    // サーバーを経由してHTMX側（Header StatusDot）へ反映するためCustomEventを発火
    this.dispatchEvent(new CustomEvent('systemStateChanged', { bubbles: true, composed: true }));
  }
}
```

```go
// Templ側: Lit の外側でCustomEventをHTMXがリッスンし、Header全体を再取得する
templ HeaderWithKillSwitch(state SystemState) {
    <div hx-get="/system/status"
         hx-trigger="systemStateChanged from:closest .header-container"
         hx-target="#header-status">
        <div id="header-status">
            @organisms.Header(state)
        </div>
        <pitha-kill-switch-panel
            status={ state.Status }
            can-pause?={ state.CanPause }
            can-resume?={ state.CanResume }
            can-kill?={ state.CanKill }>
        </pitha-kill-switch-panel>
    </div>
}
```

- Kill Switch発動はRisk Engineからも直接トリガーされうる（`architecture/overview.md` §8.3）。この場合はサーバー側が`/ws/system`経由で`kill_switch`イベントを配信し、`pitha-kill-switch-panel`が受信して`status`をローカルに反映しつつ、同様に`systemStateChanged`を発火してHeaderと同期させる

## 6. API クライアント / WebSocket（`lib/`）

`lib/api.ts`（CSRFトークンをmetaタグから自動取得、`credentials: 'same-origin'`、JSON自動パース）:

```typescript
get<T>(path: string): Promise<T>
post<T>(path: string, body?): Promise<T>
put<T>(path: string, body?): Promise<T>
patch<T>(path: string, body?): Promise<T>
del<T>(path: string): Promise<T>
```

`lib/ws.ts`（自動再接続、指数バックオフ、最大10回リトライ、JSONメッセージパース、`onOpen`/`onMessage`/`onClose`コールバック）。4種のLitコンポーネントは共通してこの2ファイルのみを経由し、`fetch()`/`new WebSocket()`を直接呼ばない。

`lib/logger.ts`: 構造化ログをブラウザ（WebView）コンソールへ出力し、致命的エラーは将来的にGoバックエンドへ送信できるようフックポイントを用意する（MVPではコンソール出力のみ）。

## 7. Wails統合

- `cmd/desktop/main.go` がGin Engineを組み立て、`options.App.AssetServer.Handler` に注入してWailsを起動する。フロントエンドは通常のWailsテンプレート（`frontend/`ディレクトリ・独自バインディング）を使わず、`static/src`のビルド成果物をGinの`Static()`で配信する
- Kill Switch発動等、サーバー内部イベントをネイティブ通知として表示する処理（`runtime.EventsEmit`, OSトースト, トレイアイコン変更）は `internal/web/handler` ではなく `internal/service/risk` からWailsランタイムを直接呼び出す薄いアダプタ（`internal/service/notify`）を介して行う
- 開発ワークフロー（3種のウォッチプロセスを並行起動、`Makefile dev`ターゲット）:

```makefile
.PHONY: dev
dev:
	@concurrently \
		"wails dev" \
		"templ generate --watch" \
		"npm run dev --prefix static"
```

  - `.templ`編集 → `templ generate --watch`が`_templ.go`を再生成 → `wails dev`がGoファイル変更を検知しプロセス再起動（WebViewは自動リロード）
  - `.ts`編集 → esbuildがバンドル → `static/dist`更新 → WebViewはHTTPキャッシュなし設定のため次回リクエストで反映（手動リロードまたは`hx-boost`遷移で反映）
  - `.css`編集 → TailwindがビルドしてSPAリロード不要で反映

- ビルド・配布: `wails build` で単一のWindows実行ファイル（`.exe`）を生成する。PostgreSQLは同梱せず、開発者環境で事前にインストール・起動済みであることを前提とする（`requirements/non-functional.md` §3）

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
