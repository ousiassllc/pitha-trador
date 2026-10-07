# API仕様: API ルート（§5）

`docs/api/endpoints.md` から分割した章（300行/ファイル制限のため）。節番号・内容は変更なし。認証・ミドルウェア（§1）・ルーティング概要（§2）・WebSocket（§6）・エラーレスポンス（§7）は `docs/api/endpoints.md` を参照。`GET /api/v1/performance`・`/calibration`・`/policy-proposals`・`/activity` の各節は `docs/api/endpoints/huma-api-insights.md` に分割した（末尾のエンドポイント一覧表は本ファイルに全エンドポイントを掲載）。

## 5. API ルート（Huma, `/api/v1`）

Huma が OpenAPI 3.1 スペックを `/api/v1/openapi.json` に自動生成する。スペックの `servers` は `[{"url": "/api/v1"}]` を宣言し、`paths` は `/scanner` のようにサーバー URL からの相対で表す（Stoplight Elements の "Try It" は `/api/v1/scanner` へリクエストする）。全 JSON 応答の `$schema` と `Link: rel="describedBy"` は `/api/v1/schemas/*.json` を指す。以下は主要エンドポイント。

### GET /api/v1/scanner

Fast Screener通過〜Jev Trader評価済みの候補銘柄一覧を返す。`return_1m`/`return_5m`は**パーセント単位**（0.42 = +0.42%）。Feature Engine・DB・Jev入力の小数比（0.0042）をAPI層（`/ws/scanner`のpushを含む）と SSRフォールバックで×100して返す／表示する。`jev_direction`/`jev_confidence`/`entry_quality`は当該銘柄の最新Jev Trader判断（`jev_decisions`のdecision_type=trader。件数窓・経過時間の上限なしで、`GET /api/v1/symbols/{symbol}`の`jev`と同一の判断。`entry_quality`は`response_json`から`internal/service/execution/enrich`で取得）、`current_position`は保有中ポジションの符号付き数量（LONG正/SHORT負。`GET /api/v1/symbols/{symbol}`の`current_position`と同定義、保有なしはnull）。Trader判断が未生成の銘柄の3項目はnull。`internal/bootstrap/candidates`が候補更新サイクルごとに最新判断（1クエリのバッチ取得）と保有中ポジション（1クエリ）を候補へ付与し、`/ws/scanner`・SSRフォールバックにも同じ値が出る（issue #492）。Scanner Dashboardの初期ロード・`pitha-scanner-table`のフォールバック取得に使用（ライブ更新は`/ws/scanner`）。

各itemの`detail_url`は銘柄詳細ページへのサーバー生成リンク（`/symbols/{symbol}`。銘柄コードはRFC 3986のunreserved文字以外をパーセントエンコード。Go側`organisms.SymbolHref`が唯一の定義で、SSR行（`ScannerTableFallback`・`ScanPanel`）と`/ws/scanner`のitemにも同じ値が入る）。`pitha-scanner-table`はこの値をそのまま`href`に使い、URLを組み立てない（HATEOAS、issue #383）。

```json
// Output（抜粋）
{
  "items": [
    {
      "symbol": "7203",
      "price": 2831.5,
      "detail_url": "/symbols/7203",
      "return_1m": 0.12,
      "return_5m": 0.42,
      "volume_ratio_5m": 3.4,
      "price_vs_vwap_bps": 38,
      "spread_bps": 7,
      "jev_direction": "LONG",
      "jev_confidence": 0.74,
      "entry_quality": "strong",
      "current_position": null
    }
  ],
  "as_of": "2026-09-26T10:15:00+09:00"
}
```

### GET /api/v1/scanner/scan

最新のスキャンサイクル（`internal/bootstrap/candidates`の候補更新サイクル）の結果を返す。ファネル件数・状態別/理由別の件数・銘柄ごとの判定（絞り込み＋ページング）。Scanner Dashboardのスキャン状況パネル（`GET /scanner/scan`）と同じデータで、メモリ上の最新1サイクル分のみを読む（`GET /api/v1/scanner`・`/ws/scanner`の経路は共有しない）。サイクル未実行なら`has_cycle=false`で他は空。
クエリ: `q`（銘柄コード/名称の部分一致、大文字小文字無視。最大64文字）, `status`（`passed`/`excluded`/`missing`）, `reason`（理由コード。最大32文字。長さ内で未知の値は400）, `page`（1始まり。範囲外は最終ページに丸める）, `page_size`（既定50、最大200）。`q`/`reason`の最大長超過・`status`の許容値外・`page`/`page_size`の範囲外はHumaの入力検証エラー（422）になる。

```json
// Output（抜粋）
{
  "has_cycle": true,
  "started_at": "2026-10-03T09:00:00Z", "finished_at": "2026-10-03T09:00:01.5Z", "duration_ms": 1500,
  "funnel": {"universe": 3800, "feature_computed": 3650, "fast_screener_passed": 80, "scout_evaluated": 42, "scout_passed": 11},
  "passed": 80, "excluded": 3570, "missing": 150,
  "reason_counts": [{"code": "min_price", "label": "現在値が下限未満（min_price）", "kind": "threshold", "count": 210}],
  "total": 3570, "page": 1, "page_size": 50, "pages": 72,
  "items": [{"symbol": "1301", "name": "極洋", "market": "プライム", "status": "excluded", "scout": null,
             "reasons": [{"code": "max_spread_bps", "label": "スプレッドが上限超過（max_spread_bps）"}]}]
}
```

上の例（`universe: 3800`）は`scan.full_scan_enabled: true`のとき（母集団＝有効な`stock`全件）の値。既定のランキング監視（FR-SCHED-9）では母集団が監視銘柄（最大45）になり、`funnel.universe`・`total`・`items`はその範囲に限られる（監視外の銘柄は現れない。FR-SCAN-3/4）。立会時間外は直前の立会時間内サイクルの監視リスト（＋保有・注文中）を保存済みデータで評価した結果を返す（FR-SCAN-7）。

- `status`: `passed`=Fast Screener候補、`excluded`=閾値で落ちた／上位N件の外（`top_n_cutoff`）、`missing`=値を算出できず判定不能（欠損理由が1つでもあれば`missing`を優先）
- 理由コード: 閾値は`min_price`/`max_price`/`min_turnover_5m_jpy`/`max_spread_bps`/`min_volume_ratio`/`min_abs_return_5m_pct`/`min_realized_volatility`（`kind=threshold`）、約定不能の`special_quote`（特別気配）/`limit_up`（ストップ高）/`limit_down`（ストップ安）（`kind=threshold`、`status=excluded`）、`top_n_cutoff`。欠損は`no_snapshot`（市況データ未取得）/`stale_snapshot`（立会中に最新の足が古すぎる: ランキング監視は3分超、`scan.full_scan_enabled: true`は`scan.full_scan_max_snapshot_age_seconds`（同梱620秒）超。FR-SCAN-7。`label`は閾値がモード別のため秒数を含まず「許容経過時間を超過」と表し、実際の閾値は`config/strategy.yaml`の上記設定で確認する。issue #690）/`missing_turnover`/`missing_spread`（板情報なし）/`missing_volume_ratio`/`missing_return_5m`/`missing_realized_vol`（`kind=missing`）。1銘柄が複数の理由を持ちうる（全フィルターを評価する）
- `funnel.scout_*`は候補に対するJev Scoutの判定済み件数（サイクル公開後にジョブが完了するたび増える。`scout`が`null`=候補外または判定待ち、`error`=Jev呼び出し失敗）

### GET /api/v1/scanner/scan/export

最新のスキャンサイクルの銘柄ごとの判定を、ページングなしの全件でCSVのダウンロード（`Content-Disposition: attachment`、`text/csv; charset=utf-8`、先頭にUTF-8 BOM）として返す。Scanner Dashboardの銘柄一覧の「CSVダウンロード」リンクが、現在の絞り込み条件（ページは引き継がない）付きで呼ぶ。

クエリ: `q`・`status`・`reason`は`GET /api/v1/scanner/scan`と同じ（未知の`reason`は400）。最初のサイクル前はヘッダー行のみのCSV。レスポンスヘッダー`X-Pitha-Record-Count`にデータ行数。ファイル名は`pitha-scan-<サイクル終了時刻UTC yyyymmdd-hhmmss>.csv`。

列: `symbol`, `name`, `market`, `status`（`passed`/`excluded`/`missing`）, `reason_codes`（`;`区切り）, `reason_labels`（`;`区切り）, `scout`（Jev Scoutの結果。未実施は空）。`=`・`+`・`-`・`@`・タブ・CRで始まるセルは、表計算ソフトで数式として実行されないよう先頭に`'`を付ける。

### GET /api/v1/symbols/{symbol}

Symbol Detail向け統合情報（価格・Jev判定・Riskパラメータ）。

`vwap`は最新の`market_snapshots`のVWAP（`Feature.VWAP`）で、スナップショットが無ければ`null`。`jev`の6項目（`direction`/`confidence`/`regime`/`entry_quality`/`toxic_flow`/`liquidity_stressed`）は、Symbol Detail画面（SSR）のJev判定パネル・Scannerと同じ**最新のJev Trader判断**（`jev_decisions`の`decision_type=trader`のうち`timestamp`・`id`が最大の1行。直近N件といった件数窓や経過時間の上限は設けず、Scout行が何件続いても、判断が古くても採用する。Scanner/Symbol Detail/Symbol API/WebSocket/`execution.Engine.State`・Exit評価は`DecisionRepository.LatestTrader`/`LatestTraderByInstruments`の同一定義を共有）（`enrich.Decision`で補完）の値で、Trader判断がまだ無ければ全項目`null`。`confidence`はJevの自己申告値（FR-TRADER-2）で、Policy Engineの`trade_signals.score`ではなく、最新シグナルが`NONE`でもTrader判断があれば値が入る。

```json
// Output（抜粋）
{
  "symbol": "7203",
  "price": 2831.5,
  "vwap": 2823.0,
  "jev": {
    "direction": "LONG",
    "confidence": 0.74,
    "regime": "BREAKOUT",
    "entry_quality": "strong",
    "toxic_flow": 0.18,
    "liquidity_stressed": 0.09
  },
  "risk": {
    "allowed_position_pct": 1.4,
    "stop_loss_pct": 0.6,
    "take_profit_pct": 1.2
  },
  "current_position": null
}
```

`risk`は固定値ではなく稼働中エンジンの実設定から取得する。`allowed_position_pct`は、直近価格でRisk Engineのポジションサイジング（`PositionSize`、`requirements/functional/components-pipeline.md` §4.8 FR-ENTRY-3）を行った結果の数量が`initial_capital`に占める%（`数量×価格÷initial_capital×100`）で、1単元（100株）も発注できなければ`0`（`max_position_per_symbol_pct`の値そのものではない）。`config/risk.yaml`の`max_position_per_symbol_pct`は、サイジング関数が未配線の場合（`AllowedPositionPctFor`未設定）にのみ返す静的フォールバック。`stop_loss_pct`/`take_profit_pct`は`execution.Config`（Exit条件）の値。

### GET /api/v1/symbols/{symbol}/candles

`pitha-price-chart`（lightweight-charts）用ローソク足＋VWAP＋出来高系列。

| クエリ | 型 | 説明 |
|-------|-----|------|
| `from` | string(RFC3339) | 取得開始時刻（含む。省略時は`to`の6時間前） |
| `to` | string(RFC3339) | 取得終了時刻（含まない。省略時は現在） |
| `interval` | string | `1m` 固定（MVP。`1m`以外は422） |

パスの`{symbol}`は英数字1〜16文字（`^[0-9A-Za-z]+$`、`/symbols/{symbol}`系ルート共通）。`from`/`to`がRFC3339でない場合、`symbol`/`interval`が範囲外の場合はいずれも422。

`from > to`、または`to - from`が7日を超える場合は400（保持期間90日分の全行を1リクエストで読み込ませないための上限。画面の既定は6時間。リポジトリは呼ばれない）。取得窓は現在時刻基準のため、最終足から6時間以上経った立会時間外・休場明けは`candles`が空配列になりうる（200。`pitha-price-chart`は空のとき可視の空状態テキストを表示する。`components/lit.md` §5.1、issue #687）。

取得区間は半開区間`[from, to)`（`timestamp >= from AND timestamp < to`）で、`from`ちょうどのバーは含まれ、`to`ちょうどのバーは返らない。`{symbol}`が未登録銘柄の場合は404（`unknown symbol`。`decisions`・`signals/{symbol}`と同じ）。

各点の`volume`は**1分足あたりの出来高**（バー単位）で、保存済みの累積セッション出来高（`market_snapshots.volume`、kabuステーションAPIの`TradingVolume`）の隣接スナップショット間差分（`cur - prev`）。累積値が後退した場合（新セッション）は当該バーの累積値自体を返し、負値にはしない。応答の先頭バーは前のスナップショットを持たないため、`Feature.Volume1m`（累積差分）があればその値、なければ`0`。`open`/`high`/`low`/`close`は1バー1サンプルの価格で同値。

出力は`{"symbol": "7203", "candles": [...]}`（`candles`は時刻昇順）。

```json
// Output（抜粋）
{
  "symbol": "7203",
  "candles": [
    {"time": "2026-10-07T00:01:00Z", "open": 2831.5, "high": 2831.5, "low": 2831.5, "close": 2831.5, "volume": 1200, "vwap": 2829.4}
  ]
}
```

`time`はRFC 3339（UTC、末尾`Z`）。`vwap`は当該スナップショットの`Feature.VWAP`（算出不能時は`0`）。`volume`は`int64`、他の価格系フィールドは円。

### GET /api/v1/symbols/{symbol}/decisions

Decision history（`jev_decisions`をJev Scout/Trader別に時系列で返す）。新しい順。未登録銘柄は404。

| クエリ | 型 | 説明 |
|-------|-----|------|
| `limit` | integer | 件数上限（既定100、1〜500。範囲外は422） |

出力は`{"symbol": "7203", "items": [...]}`。各itemは`id`/`symbol`/`timestamp`（RFC3339）/`decision_type`（`scout`/`trader`）/`direction`/`confidence`/`regime`/`entry_quality`/`toxic_flow`/`liquidity_stressed`/`continuation_probability`/`question_version`/`model_id`/`latency_ms`。

`direction`〜`continuation_probability`は`decision_type`が`trader`の行のみ値を持ち、`scout`行では`null`。

### GET /api/v1/signals / GET /api/v1/signals/{symbol}

`trade_signals`の一覧・銘柄別履歴（`risk_passed`, `reject_reason`含む）。新しい順。`/signals/{symbol}`の未登録銘柄は404。クエリ `limit`（既定100、1〜500。範囲外は422）。パスの`{symbol}`は英数字1〜16文字（`^[0-9A-Za-z]+$`、違反は422）。

```json
// Output（抜粋）
{
  "items": [
    {
      "id": 3, "symbol": "7203", "timestamp": "2026-09-27T09:31:00Z",
      "direction": "LONG", "score": 0.74, "entry_price_reference": 2831.5,
      "policy_version": "policy-v1", "risk_passed": false, "reject_reason": "spread_too_wide",
      "jev_decision_id": 2
    }
  ]
}
```

### GET /api/v1/positions

現在保有中および直近クローズ済みポジション一覧。

| クエリ | 型 | 説明 |
|-------|-----|------|
| `limit` | integer | 件数上限（既定100、1〜500。範囲外は422） |

出力は`{"items": [...]}`で、**開始（`opened_at`）が新しい順**。保有中の行とクローズ済みの行を同じ形式で返す。

```json
// Output（抜粋）
{
  "items": [
    {
      "id": 12, "symbol": "7203", "side": "LONG", "quantity": 100,
      "entry_price": 2831.5, "current_price": 2840.0, "unrealized_pnl": 850.0,
      "realized_pnl": null, "opened_at": "2026-10-07T00:12:03Z",
      "closed_at": null, "exit_reason": null
    },
    {
      "id": 11, "symbol": "6758", "side": "SHORT", "quantity": 200,
      "entry_price": 3410.0, "current_price": 3398.5, "unrealized_pnl": 0.0,
      "realized_pnl": 2300.0, "opened_at": "2026-10-07T00:05:41Z",
      "closed_at": "2026-10-07T01:02:17Z", "exit_reason": "take_profit"
    }
  ]
}
```

- `side`は`LONG`/`SHORT`（`paper_orders.side`の`BUY`/`SELL`とは別）。`quantity`は株数、`entry_price`/`current_price`/`unrealized_pnl`/`realized_pnl`は円
- `realized_pnl`/`closed_at`/`exit_reason`は**未クローズ（保有中）で`null`**。`realized_pnl`はエントリー・Exit両注文の`fees`を差し引いた値
- `exit_reason`の値は`stop_loss`/`take_profit`/`trailing_stop`/`jev_direction_reversed`/`continuation_probability_dropped`/`vwap_cross`/`max_holding`/`force_flat_before_close`/`manual`/`force_close`（意味は`architecture/er/tables-trading.md`の`positions.exit_reason`）
- `opened_at`/`closed_at`はRFC 3339（UTC、末尾`Z`）
- `unrealized_pnl`は**保有中のみ有効**で、クローズ済み行は`0`（クローズ時に`0`へリセットする。実現損益と二重計上しないため。`current_price`はクローズ時の約定価格）

### GET /api/v1/orders

`paper_orders`一覧（ステータスフィルタ `?status=` 対応）。

| クエリ | 型 | 説明 |
|-------|-----|------|
| `status` | string | `PENDING`/`FILLED`/`CANCELLED`/`REJECTED`でフィルタ（省略時は全件。それ以外は422） |
| `limit` | integer | 件数上限（既定100、1〜500。範囲外は422） |

出力は`{"items": [...]}`で、**`submitted_at`が新しい順**。

```json
// Output（抜粋）
{
  "items": [
    {
      "id": 25, "symbol": "7203", "side": "BUY", "order_type": "MARKET", "quantity": 100,
      "limit_price": null, "status": "FILLED", "submitted_at": "2026-10-07T00:12:02Z",
      "filled_at": "2026-10-07T00:12:03Z", "filled_price": 2831.5,
      "fees": 0.0, "slippage_bps": 1.8
    },
    {
      "id": 26, "symbol": "6758", "side": "SELL", "order_type": "LIMIT", "quantity": 200,
      "limit_price": 3410.0, "status": "PENDING", "submitted_at": "2026-10-07T00:20:41Z",
      "filled_at": null, "filled_price": null, "fees": 0.0, "slippage_bps": null
    }
  ]
}
```

- `side`は`BUY`/`SELL`、`order_type`は`MARKET`/`LIMIT`、`status`は`PENDING`/`FILLED`/`CANCELLED`/`REJECTED`
- `limit_price`は成行（`MARKET`）で`null`。`filled_at`/`filled_price`/`slippage_bps`は**未約定で`null`**
- `fees`は約定手数料（円。約定時に`FeeBps`×約定代金を記録、既定0。未約定は`0`）。`slippage_bps`は約定時の直近価格（シグナル価格）に対する不利方向のbps（負は有利。FR-ENTRY-8、`architecture/er/tables-trading.md`の`paper_orders`）
- `submitted_at`/`filled_at`はRFC 3339（UTC、末尾`Z`）

### GET /api/v1/system/status / POST /api/v1/system/pause / resume / kill

Kill Switchの状態取得（読み取り専用の`GET`）と操作。`pitha-kill-switch-panel`が再接続・自動発動通知後の再同期に`GET`を、操作に`POST`を呼ぶ。確認ダイアログ（`window.confirm`）を出すのはKillのみで、Pause/Resumeは確認なしで`POST`する（外部スクリプトからも利用可）。HTMX用の同名アクションルートは持たない。どれも`{"state":"running","can_pause":true,"can_resume":false,"can_kill":true}`の形式で（`POST`は更新後の）状態を返す。`state`は`running`/`paused`/`killed`、`can_*`は現在の`state`から各`POST`が有効な遷移か。

### GET /api/v1/logs/errors

エラーログのダウンロード（`requirements/functional/components-platform.md` §4.19 / FR-ERRLOG-1〜7）。ログディレクトリ（`logs/`）の日次ログ（`<YYYY-MM-DD>.log`、30日超は`.log.gz`）から対象レベルのslogレコードを抽出し、NDJSONの添付ファイルとして返す読み取り専用API。Settings画面`#error-log-panel`のフォームが`GET`で直接呼ぶ（JSを介さないブラウザ標準のダウンロード）。新規テーブル・ログ出力は持たない。

| クエリ | 型 | 説明 |
|-------|-----|------|
| `days` | integer | 対象期間（UTC日付、当日を含む直近N日。既定7、1〜90。範囲外は422） |
| `level` | string | `error`（既定。`ERROR`のみ）/ `warn`（`WARN`と`ERROR`）。それ以外は422 |

応答（200）は抽出の完了後に送信する（ヘッダに件数を載せるため。最大10MiBをメモリに保持）:

| ヘッダ | 内容 |
|-------|------|
| `Content-Type` | `application/x-ndjson; charset=utf-8` |
| `Content-Disposition` | `attachment; filename="pitha-error-logs-20261001-123045.ndjson"`（UTCの取得時刻） |
| `X-Pitha-Record-Count` | 出力した記録数（0件でも200・空ボディ） |
| `X-Pitha-Truncated` | 10MiB超で古い側を切り捨てたときのみ`true` |

```
// Body（1行1レコード。元のslog JSONのまま、秘密情報のみマスク済み）
{"time":"2026-10-01T11:36:02.123Z","level":"ERROR","msg":"update check failed","error":"Get \"https://example.invalid/releases\": dial tcp: lookup failed"}
```

500（いずれも固定メッセージ。原因はslogへ。`api/endpoints.md` §7）は、ログディレクトリを読めないとき、およびエクスポータ未注入のとき（`router.WithErrorLogExporter`の組み立て漏れ。`router.New()`の既定は常に失敗するエクスポータで、空の200ダウンロードが「エラー0件」に見えるのを避ける）。Setup Guardの対象で、必須認証情報が未設定の間は503 JSON。

認証: このGETは安全メソッドでありながら、有効なセッションCookieを必須とする（FR-ERRLOG-6。ログをCookieなしの呼び出し元へ渡さないため。`api/endpoints.md` §1）。Cookieが無い・無効な場合は403（`missing or invalid session cookie`）で、他の`/api/v1`のGET（Cookie不要）とは異なる。

### エンドポイント一覧表

| メソッド | パス | 概要 |
|---------|------|------|
| GET | `/api/v1/scanner` | 候補銘柄一覧 |
| GET | `/api/v1/scanner/scan` | 最新スキャンサイクルのファネル件数・銘柄別の判定（通過/除外/欠損と理由） |
| GET | `/api/v1/scanner/scan/export` | 最新スキャンサイクルの銘柄ごとの判定をCSVでダウンロード（絞り込みはscanと同じ、ページングなしの全件、UTF-8 BOM付き） |
| GET | `/api/v1/symbols/{symbol}` | 銘柄詳細 |
| GET | `/api/v1/symbols/{symbol}/candles` | チャート用系列データ |
| GET | `/api/v1/symbols/{symbol}/decisions` | Jev判断履歴 |
| GET | `/api/v1/signals` | トレードシグナル一覧 |
| GET | `/api/v1/signals/{symbol}` | 銘柄別シグナル履歴 |
| GET | `/api/v1/positions` | ポジション一覧 |
| GET | `/api/v1/orders` | 注文一覧 |
| GET | `/api/v1/performance` | 実績集計 |
| GET | `/api/v1/calibration` | Calibrationバケット集計 |
| GET | `/api/v1/policy-proposals` | Sol/Opus自己改善ループの提案・レビュー履歴（監査用） |
| GET | `/api/v1/system/status` | システム状態と許可される操作の取得（読み取り専用） |
| POST | `/api/v1/system/pause` | 一時停止 |
| POST | `/api/v1/system/resume` | 再開 |
| POST | `/api/v1/system/kill` | Kill Switch発動 |
| GET | `/api/v1/openapi.json` | OpenAPI 3.1スペック（Huma自動生成） |
| GET | `/api/v1/activity` | System Activity Log向けキュー状況・直近アクティビティ |
| GET | `/api/v1/logs/errors` | エラーログのダウンロード（NDJSON添付、秘密情報マスク済み） |
