# コンポーネント設計: Lit Web Components・API クライアント（§5〜§6）

`docs/components/overview.md` の§5〜§6を分割したファイル（節番号・内容は分割前と同一。`.linterly.yml` の300行/ファイル制限のため）。

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

  // connectedCallback ではなく firstUpdated: createChart には描画済みの DOM 要素が必要（issue #170）
  firstUpdated() {
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

- `tick`は1分足に集約する: 現在の分（`floor(now/60)*60`、または最新バー時刻）のバーの`high/low/close`を更新し、分が変わったときのみ新しいバーを追加する。`price <= 0`の`tick`は無視する（サーバー側も`LastPrice <= 0`の間は`tick`を送らない。issue #183）
- Jevの`direction`変化・`entry_quality`更新はチャート上のマーカー（例: LONG転換で上向き矢印）として描画する
- `symbol`属性が変化した場合（同一ページ内で銘柄を切り替えるUIを将来追加する場合）は`updated()`ライフサイクルで再購読する

### 5.2 pitha-scanner-table

- 初期データを`GET /api/v1/scanner`で取得しレンダリング、以後`/ws/scanner`のPUSHで行を更新・ソート順を再計算する
- 列ヘッダクリックでクライアント内ソート（サーバー往復不要）
- SSRフォールバック（`organisms.ScannerTableFallback`）と同一の見た目で描画する（issue #239）: ページ上部に候補件数（`data-testid="scanner-count"`）、`<caption>`に最終更新時刻（`as_of`。REST・`/ws/scanner`・SSRとも同じRFC 3339のオフセット付き表記で、秒未満は表示しない）、列見出しは日本語ラベル＋`title`ツールチップ、1m/5m Returnは符号付き（正=`+`緑・負=`-`赤・0/欠損=灰。0は符号なし）、Jev方向・エントリー品質はバッジ（`atoms.Badge`/`atoms.EntryQualityBadge`と同じ配色）、Confidenceは`%`表示、0件時は空状態メッセージ（`data-testid="scanner-empty"`。初回データ取得前は表示しない。`role="status"`はLit側のみ）。ハイドレーションは最初のデータ（REST応答または`/ws/scanner`のPUSH）が届くまでSSR描画を残し、初回取得に失敗してもSSR描画は消さない。列見出しは`<button>`（Tab＋Enter/Spaceで並べ替え、`aria-sort`、▲/▼表示）で、列の説明は`title`に加え視覚的に隠したテキストを`aria-describedby`で関連付けキーボード・スクリーンリーダーからも参照できる。列定義・書式・配色を変える場合はGo側`scannerColumns`と本コンポーネントの`COLUMNS`を必ず同時に更新し、共有ゴールデン`scanner-contract.json`（`scanner_table_contract_test.go`/`scanner-contract.test.ts`が検証）も更新する
- 銘柄行は通常の `<a href="/symbols/{symbol}">` として描画する（Litはハイパーメディアリンクの外側に出ず、通常のブラウザナビゲーションとしてページ遷移する。HTMXリクエストは発火しない = HTMX↔Lit境界ルール§「Litは HTMXリクエストをトリガーしない」に準拠）

### 5.3 pitha-calibration-heatmap

- `GET /api/v1/calibration`のバケット別データからreliability curve（lightweight-chartsのラインシリーズ）とconfidence帯別カラーヒートマップを描画する
- リアルタイム性は不要なため WebSocket は使用しない。ページ再訪問時・手動更新ボタン押下時に再フェッチする

> **Shadow DOMのスタイル（issue #145）**: Tailwindはdocument CSSでShadow Rootを越えない。`pitha-kill-switch-panel`/`pitha-price-chart`/`pitha-calibration-heatmap`は既定のShadow DOMを使うため、各自`static styles`（共通部品は`lib/styles.ts`）を持つ。`pitha-scanner-table`/`pitha-activity-feed`はLight DOMで描画しTailwindをそのまま使う。`pitha-price-chart`は`autoSize`でコンテナ幅に追従する。

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
- Killの確認ダイアログ（`window.confirm`）は、Killが新規エントリー停止に加えて保有中の全ポジションを強制決済する（`Engine.Kill` → `closer.CloseAll`、FR-RISK-3 / UC-11）ことを文言で伝える（issue #194）
- 操作（Pause/Resume/Kill）のPOSTが失敗したときはエラーを表示したうえで`status-url`から状態を再同期し、`systemStateChanged`も発火する。Killは`system.killed`を先に立ててから強制決済するため、強制決済失敗（500）でもサーバーはKilledのままで、UIが`running`のまま残らないようにする（issue #195）
- Kill Switch発動はRisk Engineからも直接トリガーされうる（`architecture/overview/flows.md` §10.3）。この場合はサーバー側が`/ws/system`経由で`kill_switch`イベントを配信し、`pitha-kill-switch-panel`が受信して`status`をローカルに反映（操作可否は`status-url`から再取得）しつつ、同様に`systemStateChanged`を発火してHeaderと同期させる。`/ws/system`が切断されている間は「接続が切れています」を表示し、再接続後に`status-url`から再同期する（§6）

### 5.5 pitha-activity-feed

- 初期データを`GET /api/v1/activity`で取得しレンダリングし、以後`/ws/activity`の`job_update`（該当キューの件数のみ置換）・`activity_event`（フィード先頭に追加、最大500件で切り詰め）を反映する
- type/queueセレクトの変更時は`GET /api/v1/activity?type=&queue=`で再取得する（サーバー側フィルタ。`queue`指定は当該キューの`job`イベントのみに一致）。WS受信イベントも同じ条件でクライアント側で絞り込む
- 直近Kill Switchイベントは`?type=kill_switch&limit=10`で別途取得し、WSの`kill_switch`イベントで先頭に追加する
- WebSocketが切断後に再接続（`open`へ復帰）した時は、切断中に失ったイベントを補うため`GET /api/v1/activity`（現在のフィルタ付き）と`?type=kill_switch&limit=10`を再取得する（#221）。これは操作者不在でも発火するため`background: true`で送り、ハートビートに数えさせない（§6、FR-RISK-6）。初回・フィルタ変更時の取得は操作者操作のため`background`を付けない
- SSRフォールバック（`QueueStatusPanel` + `ActivityFeedFallback`）を子要素として持ち、ハイドレーション時に置き換える（`pitha-scanner-table`と同じ light DOM 方式）

## 6. API クライアント / WebSocket（`lib/`）

`lib/api.ts`（CSRFトークンをmetaタグから自動取得、`credentials: 'same-origin'`、JSON自動パース）:

```typescript
get<T>(path: string, options?: { background?: boolean }): Promise<T>
post<T>(path: string, body?): Promise<T>
put<T>(path: string, body?): Promise<T>
patch<T>(path: string, body?): Promise<T>
del<T>(path: string): Promise<T>
```

`get` の `background: true` は自動発火のリクエスト（`pitha-kill-switch-panel` の再同期、`pitha-activity-feed` のWebSocket再接続後のスナップショット再取得）に `X-Pitha-Background: 1` を付け、操作者ハートビートとして数えさせない（`architecture/overview/flows.md` §10.4、FR-RISK-6）。

`lib/ws.ts`（自動再接続、指数バックオフ（〜30秒）、JSONメッセージパース（不正なJSONは`logger.warn`して破棄）、`onOpen`/`onMessage`/`onClose`/`onStatusChange`コールバック）。`onStatusChange`は`connecting`/`open`/`reconnecting`/`failed`を通知する。`failed`は連続10回の再接続失敗後で、以降も30秒間隔で無期限に再試行する。`WsClient`を持つ`pitha-kill-switch-panel`/`pitha-scanner-table`/`pitha-activity-feed`は`reconnecting`/`failed`の間、`lib/ws-status.ts`の「接続が切れています」通知（`role="status"`）を表示する（issue #133）。5種のLitコンポーネントは共通してこの2ファイルのみを経由し、`fetch()`/`new WebSocket()`を直接呼ばない。

`lib/logger.ts`: 構造化ログをブラウザ（WebView）コンソールへ出力し、致命的エラーは将来的にGoバックエンドへ送信できるようフックポイントを用意する（MVPではコンソール出力のみ）。
