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
  @property({ type: String, attribute: 'candles-url' }) candlesUrl = '';
  @property({ type: String, attribute: 'ws-url' }) wsUrl = '';

  // リアクティブ（再描画のトリガー）なのは error と wsStatus のみ。
  // チャート/系列/WsClient は非リアクティブな通常の private フィールド（DOM は createRef の containerRef 経由で掴む）
  @state() private error: string | null = null;
  @state() private wsStatus: WsStatus = 'connecting';

  private readonly containerRef = createRef<HTMLDivElement>();
  private chart: IChartApi | null = null;
  private wsClient: WsClient<SymbolMessage> | null = null;
  // ほか candleSeries / vwapSeries / volumeSeries / markers / lastDirection / lastBar も同様に非リアクティブ

  override disconnectedCallback(): void {
    super.disconnectedCallback();
    this.chart?.remove();
    this.chart = null;
    this.wsClient?.close();
    this.wsClient = null;
  }

  // connectedCallback ではなく firstUpdated: createChart には描画済みの DOM 要素が必要（issue #170）
  protected override firstUpdated(): void {
    this.initChart();   // lightweight-charts でローソク足/VWAPライン/出来高ヒストグラムペインを初期化
    this.loadInitial();  // candlesUrl から初期系列を取得（lib/api.ts経由）
    this.subscribeWs();  // wsUrl から tick / jev_update を受信し系列・マーカーを更新
  }
}
```

- `tick`は1分足に集約する: 現在の分（`floor(now/60)*60`、または最新バー時刻）のバーの`high/low/close`を更新し、分が変わったときのみ新しいバーを追加する。`price <= 0`の`tick`は無視する（サーバー側も`LastPrice <= 0`の間は`tick`を送らない。issue #183）
- `jev_update`（`direction`/`confidence`）は`direction`が変化したときのみ、チャート上のマーカー（例: LONG転換で上向き矢印）として描画する。同一`direction`の繰り返しや`confidence`のみの変化では描画せず、`entry_quality`は`jev_update`に載らないため扱わない。Symbol DetailのJev判定パネルはSSRのみで、`jev_update`では更新されない（ページ再読み込みで更新。issue #362）
- `candles-url`/`ws-url`属性が変化した場合は`updated()`ライフサイクルで再取得・再購読する。銘柄はURLに含めてサーバーが注入するため、`symbol`属性は持たない（HATEOAS。issue #409）
- 出来高ヒストグラムは`candles`の`volume`（1分足あたりの出来高。サーバーが累積セッション値の差分に変換済みで、クライアントでは再計算しない。issue #474）をそのまま描画する
- `candles-url`/`ws-url`は他コンポーネントと同様に未設定なら`logger.error`を出して該当の取得・購読を行わない
- `/ws/symbols/{symbol}`が切断されている間（`reconnecting`/`failed`）はチャート下に「接続が切れています」を表示する。`tick`は受信時刻で足を作るため切断中の足は欠落する。切断後に`open`へ復帰した時は`candles-url`を`background: true`で再取得して足を補う（操作者不在でも発火するためハートビートに数えさせない。FR-RISK-6、issue #336）
- 時間軸・クロスヘアは**JST（Asia/Tokyo）表示**とする。lightweight-charts v4は`UTCTimestamp`を既定でUTC表記するため、そのままでは東証の立会時間09:00〜15:30が00:00〜06:30に見える。`createChart`に`localization.timeFormatter`（クロスヘア、`YYYY-MM-DD HH:mm`）と`timeScale.tickMarkFormatter`（目盛、`HH:mm`/日/月/年）として`price-chart/jst-time.ts`の`formatCrosshairTime`/`formatTickMark`を渡し、`timeScale.timeVisible: true`・`secondsVisible: false`（足が1分単位のため）にする。ゾーンは`Intl.DateTimeFormat`に`timeZone: 'Asia/Tokyo'`を固定し、ホスト/Wailsのタイムゾーンに依存しない。系列・マーカー・ティック足に渡す時刻はこれまで通りepoch秒（UTC）のままで、表示時にのみJSTへ変換する（issue #478）

### 5.2 pitha-scanner-table

- 初期データを`GET /api/v1/scanner`で取得しレンダリング、以後`/ws/scanner`のPUSHで行を更新・ソート順を再計算する
- `api-url`/`ws-url`はTemplから属性で注入し、コンポーネントは既定値を持たない（HATEOAS）。未設定なら`logger.error`を出して該当の取得・購読を行わない
- 列ヘッダクリックでクライアント内ソート（サーバー往復不要）
  - エントリー品質列は辞書順ではなく品質順（poor < fair < good < strong < exceptional）でソートする。順序表は`scanner-view.ts`の`ENTRY_QUALITY_RANK`で、正本は`internal/domain/jevdecision.go`の`JevEntryQuality*`（worst→best）。未知値・nullは先頭（昇順時。降順は逆順）。値の意味づけや挙動の判断は行わず、表示順のみに使う（issue #493）
- SSRフォールバック（`organisms.ScannerTableFallback`）と同一の見た目で描画する（issue #239）: ページ上部に候補件数（`data-testid="scanner-count"`）、`<caption>`に最終更新時刻（`as_of`。REST・`/ws/scanner`はRFC 3339で返し、画面表示はSSR・Litとも`2006-01-02 15:04:05 JST`形式のJST表記（`atoms.FormatJST` / `lib/jst-datetime.ts`）で、秒未満は表示しない）、列見出しは日本語ラベル＋`title`ツールチップ、1m/5m Returnは符号付き（正=`+`緑・負=`-`赤・0/欠損=灰。0は符号なし）、Jev方向・エントリー品質はバッジ（`atoms.Badge`/`atoms.EntryQualityBadge`と同じ配色）、Confidenceは`%`表示、0件時は空状態メッセージ（`data-testid="scanner-empty"`。初回データ取得前は表示しない。`role="status"`はLit側のみ）。ハイドレーションは最初のデータ（REST応答または`/ws/scanner`のPUSH）が届くまでSSR描画を残し、初回取得に失敗してもSSR描画は消さない。列見出しは`<button>`（Tab＋Enter/Spaceで並べ替え、`aria-sort`、▲/▼表示）で、列の説明はマウス向けの`title`に加え、表の上の`<details data-testid="scanner-column-help">`（見出し「列の意味」、ラベル/説明の`<dl>`。SSRと同一）で、キーボード・タッチ・スクリーンリーダーからも読める（`title`と`aria-describedby`の二重読み上げは避ける）。数値は小数丸めをGoと揃える（ちょうど中間は0から遠い方へ）。列定義・書式・配色を変える場合はGo側`scannerColumns`と本コンポーネントの`COLUMNS`を必ず同時に更新し、共有ゴールデン`scanner-contract.json`（`scanner_table_contract_test.go`/`scanner-contract.test.ts`が検証）も更新する
- 銘柄行は通常の `<a href="{detail_url}">` として描画する。`detail_url`は`GET /api/v1/scanner`・`/ws/scanner`の各itemにサーバーが入れる銘柄詳細リンク（`organisms.SymbolHref`。SSR行と同一規則）で、Litは`/symbols/`を知らずエンコードもせずそのまま`href`に使う（HATEOAS、issue #383。共有ゴールデン`scanner-contract.json`の`item.detail_url`をSSR・API・Litの3者が検証し、特殊文字を含む銘柄でもハイドレート前後で`href`が一致する）。Litはハイパーメディアリンクの外側に出ず、通常のブラウザナビゲーションとしてページ遷移する。HTMXリクエストは発火しない = HTMX↔Lit境界ルール§「Litは HTMXリクエストをトリガーしない」に準拠

### 5.3 pitha-calibration-heatmap

- `GET /api/v1/calibration`のバケット別データからreliability curve（lightweight-chartsのラインシリーズ）とconfidence帯別カラーヒートマップを描画する
- リアルタイム性は不要なため WebSocket は使用しない。ページ再訪問時・手動更新ボタン押下時に再フェッチする
- `calibration-url`はTemplから属性で注入し、コンポーネントは既定値を持たない（HATEOAS）。未設定なら`logger.error`を出して取得を行わない（`fetch('')`で現在ページを取得しない。初回・更新ボタンとも同様。issue #494）
- 空帯・サンプルなしの扱い: APIはサンプル0件の帯・全体でも`direction_accuracy`/`avg_future_return_pct`/`avg_confidence`/`brier_score`等を`0`で返す（「データなし」と「実測0」を値では区別できない）。そのためコンポーネントは`sample_count`で判定する
  - `sample_count == 0`の帯はreliability curveの実測系列に含めず（Perfect calibration線は全帯）、ヒートマップセルは中立色（グレー）で「データなし」と表示する（的中率・平均リターン・`conf`は出さない）。各帯セルは`n=<sample_count>`を表示する
  - 方向別テーブルは`sample_count == 0`の行の的中率・平均リターンを`—`で表示する
  - 全帯・全方向の`sample_count`合計が0のときはBrier Score/Log Loss/Expected Calibration Errorを表示せず「サンプルなし」を表示する（0.000＝最良スコアと誤読させない）。API仕様（`api/endpoints/huma-api-insights.md`）は変更しない

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
// killSwitch*URL は "/api/v1/system/pause|resume|kill|status" と "/ws/system" を持つ organisms 内の const
templ KillSwitchPanel(state domain.SystemState) {
    <pitha-kill-switch-panel
        status={ string(state) }
        can-pause?={ state.CanPause() }
        can-resume?={ state.CanResume() }
        can-kill?={ state.CanKill() }
        pause-url={ killSwitchPauseURL }
        resume-url={ killSwitchResumeURL }
        kill-url={ killSwitchKillURL }
        status-url={ killSwitchStatusURL }
        ws-url={ killSwitchWSURL }>
    </pitha-kill-switch-panel>
}
```

- URL属性（`status-url`/`ws-url`/`pause-url`/`resume-url`/`kill-url`）が未設定なら、該当の取得・購読・操作（`status-url`→再同期、`ws-url`→WebSocket購読、`pause-url`等→対応するPOST）を行わず`logger.error`を出す（無言でreturnしない。issue #494）
- 操作可否は`domain.SystemState.CanPause/CanResume/CanKill`（Running→pause/kill可、Paused→resume/kill可、Killed→resumeのみ）が唯一の定義で、`GET/POST /api/v1/system/*`の応答も`can_pause`/`can_resume`/`can_kill`として返す。状態を読み出せないとき`Header`は操作ボタンを描画せず（`status=""`）、パネルが`status-url`から自己補正する
- 操作後は`systemStateChanged`を発火し、`Header`の`#header-status`（`hx-trigger="systemStateChanged from:closest header"`）がStatusDotを再取得する
- Killの確認ダイアログ（`window.confirm`）は、Killが新規エントリー停止に加えて保有中の全ポジションを強制決済する（`Engine.Kill` → `closer.CloseAll`、FR-RISK-3 / UC-11）ことを文言で伝える（issue #194）
- 操作（Pause/Resume/Kill）のPOSTが失敗したときはエラーを表示したうえで`status-url`から状態を再同期し、`systemStateChanged`も発火する。Killは`system.killed`を先に立ててから強制決済するため、強制決済失敗（500）でもサーバーはKilledのままで、UIが`running`のまま残らないようにする（issue #195）
- Kill Switch発動はRisk Engineからも直接トリガーされうる（`architecture/overview/flows.md` §10.3）。この場合はサーバー側が`/ws/system`経由で`kill_switch`イベントを配信し、`pitha-kill-switch-panel`が受信して`status`をローカルに反映（操作可否は`status-url`から再取得）しつつ、同様に`systemStateChanged`を発火してHeaderと同期させる。Killed以外への遷移（`AutoResume`による自動解除、別ウィンドウのResume/Pause。#363）は`state_changed`として配信され、パネルは`status-url`から（`background: true`で）再取得して`systemStateChanged`を発火し、Headerの`#header-status`とPause/Kill操作可否を追従させる。`/ws/system`が切断されている間は「接続が切れています」を表示し、再接続後に`status-url`から再同期する（§6）

### 5.5 pitha-activity-feed

- 初期データを`GET /api/v1/activity`で取得しレンダリングし、以後`/ws/activity`の`job_update`（該当キューの件数のみ置換）・`activity_event`（フィード先頭に追加、最大500件で切り詰め）を反映する
- `api-url`/`ws-url`/`kill-switch-events-url`はすべてTemplから属性で注入し、コンポーネントは既定値を持たない（HATEOAS。`pitha-kill-switch-panel`と同じ）。未設定の属性があれば`logger.error`を出して該当の取得・購読を行わない
- type/queueセレクトの変更時は`GET /api/v1/activity?type=&queue=`で再取得する（サーバー側フィルタ。`queue`指定は当該キューの`job`イベントのみに一致）。WS受信イベントも同じ条件でクライアント側で絞り込む
- 直近Kill Switchイベントは`kill-switch-events-url`属性（Templ注入。値は`/api/v1/activity?type=kill_switch&limit=10`）で別途取得し、WSの`kill_switch`イベントで先頭に追加する（コンポーネント側の切り詰め件数はこの`limit`と同じ10件）。クエリ文字列はクライアントで組み立てない
- WebSocketが切断後に再接続（`open`へ復帰）した時は、切断中に失ったイベントを補うため`GET /api/v1/activity`（現在のフィルタ付き）と`?type=kill_switch&limit=10`を再取得する（#221）。これは操作者不在でも発火するため`background: true`で送り、ハートビートに数えさせない（§6、FR-RISK-6）。初回・フィルタ変更時の取得は操作者操作のため`background`を付けない
- Kill Switch履歴は取得状態を3つに区別して表示する。取得前は「Loading kill switch events…」、取得失敗時は`role="alert"`の「Failed to load kill switch events: …」を表示し、「No kill switch events.」は取得に成功して0件のときだけ表示する（安全に関わる情報を取得できていないだけの状態を「イベントなし」と誤認させない）。保持済みの履歴がある場合は、取得に失敗してもその一覧を表示したままエラーを併記し、以後の再取得（WS再接続）が成功すればエラー表示は消える（#346）
- スナップショット（`GET /api/v1/activity`）の取得は最新のリクエストの応答だけを`queues`/`events`/エラー状態に反映する。フィルタ変更や再接続直後の再取得より前に発行された古い応答が遅れて届いても、新しい結果を上書きしない（リクエストごとに世代番号`snapshotGeneration`を採番し、完了時に最新世代でなければ破棄する。#345）。Kill Switch履歴の取得（`kill-switch-events-url`）はフィルタに依存しないためこの対象外
- SSRフォールバック（`QueueStatusPanel` + `ActivityFeedFallback`）を子要素として持ち、初回スナップショット（`GET /api/v1/activity`）の取得に成功した時点で置き換える（`pitha-scanner-table`と同じ light DOM 方式）。取得完了前・取得失敗時はSSRのテーブルをそのまま残し、ハイドレーションで画面を空白にしない。この間は直近Kill Switchイベント・WS切断/エラー通知のみ追加描画する

## 6. API クライアント / WebSocket（`lib/`）

`lib/api.ts`（CSRFトークンをmetaタグから自動取得、`credentials: 'same-origin'`、JSON自動パース）:

```typescript
get<T>(path: string, options?: { background?: boolean }): Promise<T>
post<T>(path: string, body?): Promise<T>
put<T>(path: string, body?): Promise<T>
patch<T>(path: string, body?): Promise<T>
del<T>(path: string): Promise<T>
```

`get` の `background: true` は自動発火のリクエスト（`pitha-kill-switch-panel` の再同期、`pitha-activity-feed` / `pitha-price-chart` のWebSocket再接続後のスナップショット・足の再取得）に `X-Pitha-Background: 1` を付け、操作者ハートビートとして数えさせない（`architecture/overview/flows.md` §10.4、FR-RISK-6）。

`lib/ws.ts`（自動再接続、指数バックオフ（〜30秒）、JSONメッセージパース（不正なJSONは`logger.warn`して破棄）、`onOpen`/`onMessage`/`onClose`/`onStatusChange`コールバック）。`onStatusChange`は`connecting`/`open`/`reconnecting`/`failed`を通知する。`failed`は連続10回の再接続失敗後で、以降も30秒間隔で無期限に再試行する。バックオフは`open`時点ではリセットせず、最初のメッセージ受信または`open`から10秒の接続維持を確認した時点で初期値（500ms）へ戻す。受理直後に切断される接続を繰り返しても、バックオフが伸びて`failed`に到達する（issue #466）。`WsClient`を持つ`pitha-price-chart`/`pitha-kill-switch-panel`/`pitha-scanner-table`/`pitha-activity-feed`は`reconnecting`/`failed`の間、`lib/ws-status.ts`の「接続が切れています」通知（`role="status"`）を表示する（issue #133、`pitha-price-chart`は#336）。5種のLitコンポーネントは共通してこの2ファイルのみを経由し、`fetch()`/`new WebSocket()`を直接呼ばない。

`lib/logger.ts`: 構造化ログをブラウザ（WebView）コンソールへ出力し、致命的エラーは将来的にGoバックエンドへ送信できるようフックポイントを用意する（MVPではコンソール出力のみ）。
