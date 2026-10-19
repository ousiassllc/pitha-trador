# API 仕様: Paper 実績 MCP のツール

`docs/architecture/paper-results-mcp.md` の MCP が認証通過後に提供するツール。アプリの Huma `/api/v1` とは別プロセス・別ポートであり、セッション Cookie は使わない。エラーの HTTP と `code` は `docs/architecture/paper-results-mcp-errors.md`。

トランスポートは HTTP の MCP である（stdio ではない）。**回答（実装）**: Streamable HTTP を選んだ。`POST` に JSON-RPC 2.0 を 1 件ずつ送り、応答は常に単一の `application/json`（SSE ストリームは開かない）。根拠: 旧来の HTTP+SSE は MCP 仕様 2025-03-26 で Streamable HTTP に置き換えられ、本サーバーは長寿命の接続も server push も持たないため、認証・同時実行 2・3 秒上限の検証が単純になる。対応する `protocolVersion` は `2025-06-18` / `2025-03-26` / `2024-11-05`。`GET` は 405（認証後）。Grok Bot のコネクタが旧来の HTTP+SSE しか話せないと分かった場合は別途対応する（ツールの JSON は変わらない）。ツール名と引数・戻り値の JSON は、その版がどちらでも本節のとおり固定する。`initialize` と `tools/list` を含む全リクエストがアーキテクチャ §3.3 の API キーを必要とする。URL を知っているだけではツール一覧も返さない。経路（Tailscale、任意の Cloudflare Tunnel、その他）が違ってもツールの JSON は同じである。成功・エラーの JSON-RPC への載せ方は `architecture/paper-results-mcp-errors.md` §2。

## 1. 期間

`from` と `to` は JST の暦日 `YYYY-MM-DD`。区間は半開 `[from 00:00 JST, to 00:00 JST)`。両方省略のとき既定は、JST の今日を含む直近 30 日（`to` = 明日、`from` = その 30 日前）。片方だけは `invalid_period`。長さの上限は 366 日。根拠: `GET /api/v1/export` の期間上限と同じで、1 年分の Paper 確認に足り、それ以上の全走査を初版では許さない。`to` が JST の翌日より未来なら `invalid_period`。

時刻の戻り値は UTC の RFC3339。DB の TEXT 日時をそのままの桁では返さない。

## 2. 上限

| 上限 | 値 | 根拠 |
|------|----|------|
| 同時実行 | 2 | 読み取り接続は 1 本。2 件目は 3 秒予算の中で待つ。3 件目は `concurrency_limited` で SQL を始めない。単一ユーザーの対話で、アプリの書き込みと同じディスクを占有し続けない |
| クエリ時間 | 3 秒（待ちを含む） | 超えたらキャンセルして `query_timeout`。自動再試行はしない。ローカル集計が健全なら足り、ロック待ちをアプリ側へ長引かせない |
| `busy_timeout` | 1 秒 | 取れなければ `db_locked`。アプリ本体の 60 秒には合わせない |
| 期間 | 最大 366 日。既定 30 日 | 上の §1 |
| 一覧の `limit` | 既定 50、最大 200 | Scanner のページング（FR-SCAN-4）と同じ。1 回で全履歴を渡さない |
| 集計の走査 | 期間内のクローズ済み `positions` が 5,000 件を超えたら `limit_exceeded`（部分集計は返さない） | 日次数十件の Paper なら 5,000 は年単位を超える。壊れたデータや投入済み全件の全走査を切る |
| `jev_calibration` の horizon | 最大 8 種 | 運用は 5/10/15 と Settings の timeout。旧 20 分を残しても収まる。超えたら `limit_exceeded` |
| 日別・欠損日 | 最大 366 行 | 期間上限と同じ |

## 3. ツール

共通の成功ボディは `{"tool":"<名前>","data":{...}}`。数値は JSON number。定義できない比率は `null`（0 件を 0% と誤読させない。FR-META-5 と同じ）。

### 3.1 `paper_performance_summary`

引数: `from`, `to`（省略可）。

対象: `positions` のうち `closed_at` と `realized_pnl` が両方非 NULL で、`closed_at` が期間内。勝敗は `realized_pnl > 0` を勝ち、`< 0` を負け、`= 0` を引き分けとする（FR-CAL-5。引き分けは勝ちに含めない）。

| フィールド | 定義 |
|------------|------|
| `closed_trade_count` | 対象件数 |
| `open_position_count` | 照会時点で `closed_at IS NULL` の件数。価格は返さない |
| `win_count` / `loss_count` / `flat_count` | 上の勝敗 |
| `win_rate` | `win_count / closed_trade_count`。0 件なら `null` |
| `total_pnl_jpy` | `realized_pnl` の合計（円） |
| `expectancy_jpy` | `realized_pnl` の平均。0 件なら `null` |
| `expectancy_return_pct` | 各行の `realized_pnl / (entry_price * quantity) * 100` の平均。建値が 0 以下の行はリターン 0 として数える（`internal/service/insight.Aggregate` と同じ）。0 件なら `null` |
| `profit_factor` | 勝ちの合計円 ÷ 負けの合計円の絶対値。負けが 0 件、または対象 0 件なら `null` |
| `max_drawdown_jpy` | `closed_at` 昇順、同着は `id` 昇順で `realized_pnl` を累積した曲線の、ピークから谷までの最大下落（円）。下落が無ければ 0。0 件なら 0 |
| `capital_jpy` | `config/risk.yaml` の `paper.initial_capital`。未設定時は `config.DefaultPaperInitialCapital`（30,000,000）。アプリと同じ yaml 解決順で読む。Live の値は使わない |
| `max_drawdown_capital_ratio` | `max_drawdown_jpy / capital_jpy`。`capital_jpy` が 0 以下、または yaml を読めないときは両方 `null`（円建てのドローダウンは返す） |

`internal/service/insight.Performance.MaxDrawdownPct` はリターン（%）の曲線であり、ここでいう資金比ではない。実装は円の累積を別に計算する。

### 3.2 `paper_daily_pnl`

引数: `from`, `to`。`data.days[]` は JST の決済日ごとに 1 行。決済が 0 件の日は行を作らない（欠損日は `collection_status`）。

各行: `date`（`YYYY-MM-DD`）、`trade_count`、`pnl_jpy`、`win_count`、`loss_count`。日数の上限は §2。

### 3.3 `paper_trades`

引数: `from`, `to`、`limit`（既定 50、最大 200）、`cursor`（前回応答の `next_cursor`。無ければ先頭）。

並び: `closed_at` 降順、`id` 降順。対象は §3.1 と同じクローズ済み。

各行のフィールドは次だけ。

| フィールド | 列 |
|------------|----|
| `id` | `positions.id` |
| `symbol` | `positions.symbol` |
| `side` | `positions.side`（`LONG` / `SHORT`） |
| `quantity` | `positions.quantity` |
| `entry_price` | `positions.entry_price` |
| `opened_at` | `positions.opened_at` |
| `exit_price` | `positions.exit_order_id` の `paper_orders.filled_price`。注文が無ければ `null` |
| `closed_at` | `positions.closed_at` |
| `exit_reason` | `positions.exit_reason`。既知の値は `stop_loss` / `take_profit` / `trailing_stop` / `jev_direction_reversed` / `continuation_probability_dropped` / `vwap_cross` / `max_holding` / `force_flat_before_close` / `manual` / `force_close`（`internal/domain/position.go`）。それ以外の非 NULL も、保存された文字列をそのまま返す（切り詰めない。列は `varchar(50)`） |
| `realized_pnl` | `positions.realized_pnl` |
| `fees_jpy` | エントリー注文とエグジット注文の `paper_orders.fees` の和。片方が無ければある方だけ。両方無ければ `null` |

`next_cursor` は次のページが無いとき `null`。cursor の中身はクライアントが解析する契約ではない。

返さない: `current_price`、`unrealized_pnl`、`slippage_bps`、注文の `limit_price`。

### 3.4 `jev_calibration`

引数: `from`, `to`、`horizon_minutes`（省略時は期間内に存在する horizon をすべて、最大 8）。

`data.decisions`: 期間内（`jev_decisions.timestamp`）の `scout_count` と `trader_count`。

`data.horizons[]` は horizon ごと。ラベルは `calibration_outcomes` を `jev_decision_id` で trader 判断（`decision_type='trader'`）に結合し、判断の `timestamp` が期間内のもの。

各 horizon は `horizon_minutes`（`calibration_outcomes.horizon_minutes`）と `by_label_version[]` だけを持つ。版を混ぜた 1 本の Brier・的中率・バケットは返さない。根拠: `label_version=1` と `2` で正解の定義が違う（FR-CAL-4）。

`by_label_version[]` の各要素:

| フィールド | 定義 |
|------------|------|
| `label_version` | `calibration_outcomes.label_version` |
| `sample_count` | その版の行数（`excluded_count` を含む） |
| `excluded_count` | `confidence` または `was_direction_correct` が NULL の行。平均からは外す |
| `hit_rate` | `was_direction_correct` が非 NULL の行のうち真の割合。対象 0 件なら `null` |
| `brier` | 残った行の `jev_decisions.confidence` を予測確率、`was_direction_correct` が真なら 1・偽なら 0 とした `(confidence - y)^2` の平均（`internal/service/calibration.binaryScores` の二乗誤差項）。対象 0 件なら `null` |
| `buckets[]` | その版だけの信頼度帯。`[0.50,0.60)` `[0.60,0.70)` `[0.70,0.80)` `[0.80,0.90)` `[0.90,1.00]`（最後だけ 1.00 を含む。`domain.DefaultConfidenceBucketRanges`、FR-CAL-3）。各要素は `low` / `high` / `sample_count` / `hit_rate`。帯の外は `out_of_bucket_count` だけ |
| `barrier_counts` | その要素の `label_version` が 2 のとき、`barrier_hit` が `tp` / `sl` / `timeout` の件数。それ以外の版では各 0 |
| `avg_net_return_bps` | その要素の `label_version` が 2 かつ `net_return_bps` が非 NULL の平均。無ければ `null` |

返さない: `future_return`、`max_adverse_excursion`、`max_favorable_excursion`、`entry_ref_price`、`state_json`、`response_json`。

### 3.5 `metalabel_history`

引数: `from`, `to`、`limit`（既定 50、最大 50）。

`data.models[]`: `metalabel_models.trained_at` が期間内。`trained_at` 降順。フィールドは `version` / `algorithm` / `label_version` / `status`（`active` / `rejected`） / `reject_reason`（200 ルーンを超える分は切って `reject_reason_truncated: true`） / `trained_at` / `train_start` / `train_end` / `oos_start` / `oos_end` / `train_samples` / `oos_samples` / `oos_auc` / `oos_expected_value_bps`。AUC・EV が NULL なら `null`。

`data.prediction_counts`: `metalabel_predictions.predicted_at` が期間内の行の `take` と `skip` の件数だけ（`action` の CHECK は `take` / `skip`。`000034`）。

`data.retrain_skips[]`: 夜間再学習がデータ不足で見送られた記録（`metalabel_retrain_skips.skipped_at` が期間内、新しい順、最大 `limit` 件。`000038`）。フィールドは `skipped_at` / `reason_code` / `usable_rows` / `required_rows`。`usable_rows` 件が `required_rows` 件に足りない（「必要 130 件中 87 件」は `required_rows=130`・`usable_rows=87`）。`reason_code` は固定コードで、自由文は返さない: `too_few_rows_before_oos`（検証期間より前の使えるラベル付き判断が fit+較正の最小件数に足りない）/ `too_few_rows_after_purge`（purge・embargo 後に fit か較正が最小件数に足りない）/ `training_data_unusable`（件数はあるがラベルが片側だけ等で学習できない）。未知のコードは `other`。

返さない: `params_json`、`feature_names_json`、`oos_calibration_json`、`features_json`、確率・EV の行そのもの。

### 3.6 `risk_rejections`

引数: `from`, `to`。対象は期間内（`trade_signals.timestamp`）の行。

`data.risk_passed_count`: `risk_passed` が真の件数。`data.other_reject_count`: `risk_passed` が偽で、`reject_reason` が `risk_engine_rejected:` で始まらない件数（理由文は返さない）。

`data.other_reject_breakdown[]`: `other_reject_count` の内訳（`reason` と `count`。件数の多い順、同数は `reason` 昇順）。`reject_reason` の最初の `:` より前（前後の空白を除く）を固定コードに正規化する。コードは Policy の理由コードそのもの（`jev_none` / `probability_below_threshold` / `entry_quality_below_threshold` / `continuation_probability_below_threshold` / `toxic_flow_above_threshold` / `liquidity_stressed_above_threshold` / `spread_too_wide` / `thin_liquidity` / `special_quote` / `price_limit` / `not_lendable` / `missing_data` / `api_error` / `not_calibrated` / `meta_model_unavailable` / `vwap_z_unavailable` / `vwap_not_extreme`。`internal/service/policy/engine.go`・`meta.go`・`regime.go`）。`meta_skip: <reason> (p=… ev_bps=…)` は `meta_skip:<reason>`（`no_active_model` / `model_unusable` / `no_candidate` / `missing_feature` / `invalid_config` / `predict_failed` / `ev_not_above_minimum`。`internal/service/metalabel/decide.go`）にし、確率・EV の数値は返さない。知らない理由は `meta_skip:other`、知らないコードは `other`、`reject_reason` が NULL か空は `unknown`。内訳の合計は `other_reject_count` と一致する。板・価格・エラー文は返さない。

`data.buckets[]`: `risk_engine_rejected:` の直後から、次の `:` または末尾までのトークン（前後の空白を除く）を `reason` とし、その件数。トークンが空なら `unknown`。詳細（`count=` や円金額、エラー文）は返さない。日次停止は `Check` が `daily_loss_limit_jpy` / `profit_lock` / `max_daily_trades` を返し、Policy が `risk_engine_rejected:` を付ける（`internal/service/risk/check.go`、`internal/service/policy/engine.go`）ので、バケット名はそのコードになる。

### 3.7 `kill_switch_history`

引数: `from`, `to`、`limit`（既定 50、最大 200）。`kill_switch_events.triggered_at` が期間内。新しい順。

各行: `id`、`triggered_at`、`reason`（CHECK 制約の値: `daily_loss_limit` / `consecutive_losses` / `market_data_down` / `jev_api_down` / `broker_api_error` / `unexpected_position` / `fill_discrepancy` / `db_write_failure` / `operator_heartbeat_timeout` / `operator_manual`）、`resolved_at`、`resolved_by`（`auto` / `manual`。未解除はどちらも `null`）。解除は `kill_switch_resolutions` を `kill_switch_event_id` で結合する（`kill_switch_events` に解除列は無い。マイグレーション `000014`）。

返さない: `detail_json`。

### 3.8 `collection_status`

引数: `from`, `to`。

| フィールド | 定義 |
|------------|------|
| `latest_snapshot_at` | `market_snapshots.timestamp` の最大。行が無ければ `null`。価格列は読まない |
| `missing_session_dates` | 期間内の東証立会日（`marketcalendar.Calendar.IsTradingDay` が真の JST 日）のうち、その日に `market_snapshots` が 0 件の日付。最大 366。価格・銘柄は返さない |
| `jobs[]` | キュー `market-data` / `feature-calc` / `jev-scout` / `jev-trader` / `outcome-labeling` / `analytics` ごとに、期間内（`finished_at`）の `succeeded_count` / `failed_count` と、`status=succeeded` の最新 `finished_at` |
| `daily_bar_run_failed_count` | 期間内（`run_date`）の `daily_bar_runs.status='failed'` の件数 |
| `label_skip_count` | 期間内（`calibration_label_skips.created_at`）の件数 |
| `job_failures` | 期間内（`finished_at`）に `failed` で終わったジョブの内訳。`total`、`by_day[]`（`date` は JST 日・`count`）、`by_queue[]`（`queue`・`count`。キューの順）、`by_error_code[]`（`code`・`count`）、`by_day_and_error_code[]`（`date`・`code`・`count`。「10/6 の 4002006 は N 件」はこれで引ける）。どの一覧も合計は `total`。`skipped_total` / `skipped_by_queue[]` は `last_error` が `skipped: ` で始まる `succeeded` のジョブ（`jobqueue.Skip` による終端スキップ。`jobs[].succeeded_count` に含まれる）の件数 |
| `retention` | `succeeded_job_days`（7）/ `failed_job_days`（30）と固定の注記。完了ジョブは `internal/service/retention` が成功 7 日・失敗 30 日で削除するため、期間がそれより前に及ぶと成功は先に消え、`jobs[]` と `job_failures` に出ない（失敗は 30 日残る） |

`job_failures` の `code` は `jobs.last_error` の先頭 256 バイトから、固定パターン（kabu `code 4002006` / 立花 `p_errno=-62`・`sResultCode=NNN`）の数値トークン（`-` 付きでもよい 1〜8 桁）だけを取り出した値で、無ければ `unknown`。取り出しはプロセス内で行い、`last_error` の本文は返さない（SQL に `last_error` が現れるのは `substr(last_error, 1, N)`（N は 256 以下）の形だけで、`readonly_db_test` が確認する）。

返さない: `jobs.payload_json`、`jobs.last_error` の本文、`daily_bar_runs.error`、`daily_bars` の価格・出来高、スキップ理由の自由文。

## 4. 出さないもの

ツールの戻り値はホワイトリスト方式とする。テーブルの `SELECT *` は禁止する。下の表は、列が存在してもツールに載せないものの一覧である（マイグレーション `000001`〜`000038` で確認した名前）。

| 出さないもの | 確認した場所 |
|--------------|----------------|
| `market_snapshots.raw_data_json` | `000002`。ブローカー生レスポンス |
| `market_snapshots` の価格・板・出来高・特徴量（`price` / `bid` / `ask` / `spread_bps` / `volume` / `vwap` / `ofi_*` / `return_*` など。`000016`・`000026`・`000032`） | 時価・板・気配そのもの。鮮度は件数と時刻だけ |
| `jev_decisions.state_json` / `response_json` | `000004`。市場状態と判断本文 |
| `jev_decisions` の `ofi_*` / `microprice_dev_bps` / `vwap_z` / `regime_volatility_bps` / `regime_autocorr` | `000032`・`000036`。板・価格由来の特徴量 |
| `secrets` の全列 | `000013`。`key` 名も返さない |
| API キー、MCP の `api_key`、証券会社の認証情報。Cloudflare を使うときの Client-Secret も | ローカル設定と `secrets`。ログにも出さない |
| `metalabel_models.params_json` / `feature_names_json` / `oos_calibration_json` | `000034` |
| `metalabel_predictions` の行の中身（`features_json` を含む） | `000034`。件数だけ |
| `kill_switch_events.detail_json` | `000009` / `000017` |
| `jobs.payload_json` / `jobs.last_error` の本文 | `000003`。`last_error` は先頭を読み、エラーコードのトークンだけを取り出す（§3.8） |
| `daily_bars` の価格・出来高、`daily_bar_runs.error` | `000030` |
| `positions.current_price` / `unrealized_pnl` | `000008`。時価マーク |
| `runtime_settings` の値 | 秘密鍵のパス等が入りうる。本 MCP は読まない |
| sqlite-vec の仮想テーブル | 市場状態の埋め込み。本 MCP は読まない |

Paper の約定記録（`entry_price` / `exit_price` / `realized_pnl` / `fees_jpy`）は取引の結果として返す。板やスナップショットの時価列とは別である。

## 5. 受け入れ条件（テストで確認できること）

- 既定の設定ファイル（`enabled: false`）では `127.0.0.1:48110` に接続できない
- `enabled: true` かつ `api_key` が非空で、そのキーが正しいときだけ待受し、ツールが応答する。`api_key` が空なら待受しない
- `listen` が `0.0.0.0` のときは起動しない。Tailscale で直接待つ設定は、その PC の tailnet 側アドレスだけを bind する
- キー無し・誤ったキーは 401 で、そのリクエストの経路で SQL が 0 件である
- MCP が開いた接続に対する `INSERT` はエラーになる。`PRAGMA wal_checkpoint` とマイグレーションを呼んだ痕跡が無い
- 既知の数件を入れたテスト DB で、§3.1 の勝率・期待値・円ドローダウン・資金比が手計算と一致する
- 戻り値の JSON とログに、`raw_data_json` の中身、`state_json` の中身、`secrets.encrypted_value`、テスト用 API キー文字列が含まれない
- 期間 367 日、`limit=201`、クローズ済み 5,001 件は、それぞれ `invalid_period` または `limit_exceeded` で部分結果が無い
- 選んだ経路（Tailscale または Cloudflare Tunnel）経由で、認証つきでリモートから呼べることを実機で確認する。自動テストの対象外とし、キーを伏せた呼び出し記録を残す（#783 の実機条件）
- Tailscale を選んだときは、tailnet の外からその待受に届かないことを同じ記録で確認する

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-10-17 | 新規作成 | issue #783。ツールの入出力・上限・出さない列を、既存マイグレーションの列名で固定 |
| 1.1 | 2026-10-17 | 受け入れ条件の実機確認を、選んだ経路（Tailscale または Cloudflare Tunnel）経由に変えた。Tailscale は tailnet 外から届かないことも確認する。必須の 401 は API キーだけ | 悠太さん指示。Cloudflare を必須にしない |
| 1.2 | 2026-10-18 | トランスポートの要確認を、Streamable HTTP（POST のみ・JSON 応答）で実装した旨の回答に改めた。ツール・上限・ホワイトリストは変更なし | issue #783 の実装 |
