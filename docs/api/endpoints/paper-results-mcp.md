# API 仕様: Paper 実績 MCP のツール

`docs/architecture/paper-results-mcp.md` の MCP が認証通過後に提供するツール（読み取りの 9 つ（`health_today` を含む）と、設定の読み書きの 6 つと、Paper の注文案の 6 つと、Jev のプロンプトの 3 つ。合計 24）。アプリの Huma `/api/v1` とは別プロセス・別ポートであり、セッション Cookie は使わない。エラーの HTTP と `code` は `docs/architecture/paper-results-mcp-errors.md`。

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
| `capital_jpy` | Paper の資金。`runtime_settings` の `risk.paper.initial_capital_jpy`（Settings で保存した Paper の運用資金、issue #804。読み取り接続で読む）が有効に保存されていればそれ。なければ `config/risk.yaml` の `paper.initial_capital`（MCP プロセスの起動時に読む。未設定時は `config.DefaultPaperInitialCapital`＝500,000。アプリと同じ yaml 解決順）。Live の値は使わない。どちらも使えなければ `null`（issue #806） |
| `max_drawdown_capital_ratio` | `max_drawdown_jpy / capital_jpy`。`capital_jpy` が 0 以下、または保存値も yaml も使えないときは両方 `null`（円建てのドローダウンは返す） |
| `by_origin[]` | 取引の入り口（メタモデル / ブートストラップ / 注文案 / 不明）ごとの件数・勝敗・勝率・合計損益・期待値（issue #829。定義は `paper-results-mcp-proposals.md` §3.22）。全体の値は変わらない |

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
| `exit_reason` | `positions.exit_reason`。既知の値は `stop_loss` / `take_profit` / `trailing_stop` / `jev_direction_reversed` / `continuation_probability_dropped` / `vwap_cross` / `max_holding` / `force_flat_before_close` / `manual` / `force_close` / `proposal_close`（注文案の `close` による決済。`internal/domain/position.go`）。それ以外の非 NULL も、保存された文字列をそのまま返す（切り詰めない。列は `varchar(50)`） |
| `realized_pnl` | `positions.realized_pnl` |
| `fees_jpy` | エントリー注文とエグジット注文の `paper_orders.fees` の和。片方が無ければある方だけ。両方無ければ `null` |
| `origin` | どこから入った取引か: `meta_model` / `bootstrap` / `order_proposal` / `unknown`。エントリー注文のシグナルの `trade_signals.policy_version` で決める（issue #829。`paper-results-mcp-proposals.md` §3.22） |

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

`data.proposal_risk_passed_count` / `data.proposal_risk_rejected_count`（issue #828）: 上の件数のうち、MCP の注文案が書いた `trade_signals`（`policy_version` が `mcp-proposal-v1`）の、`risk_passed` が真の件数と偽の件数。注文案の Risk 拒否も `risk_passed_count` から外れた偽の側として `buckets[]` に数えられ、Policy の拒否と `policy_version` で見分けられる。注文案の Risk 以外の拒否理由（銘柄不明・価格が古い等）は `trade_signals` に残らず、`order_proposal_get` の `status_reason` で見る。

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
| `alerts[]` | いま出ている警告（期間ではなく現在）。要素は `code` と `since`（条件が始まった時刻、UTC RFC3339）だけで、自由文は返さない。無ければ `[]`。`code` は固定: `kabu_not_logged_in`（取引日の 08:45〜15:30 に kabu が未ログイン〔4001007〕。08:45 の時点で既に続いていた、または後から 5 分続いた）/ `market_data_idle`（立会中〔9:00〜11:30・12:30〜15:30〕に `market-data` ジョブが成功も失敗も 5 分間ゼロ。立会の開始から数える）。非取引日と対象時間外は出さず、条件が解けると消える。アプリ内の検知（`health_alerts`、`000039`）が保存した行を読み、検知の最終確認（`health_alert_checks`）が 2 分より古い間はアプリが止まっているとみなして行を使わず、立会中なら `jobs` から `market_data_idle` だけを導く。health_today（#808）も同じ状態（`query.HealthAlerts`）を読む |
| `retention` | `succeeded_job_days`（7）/ `failed_job_days`（30）と固定の注記。完了ジョブは `internal/service/retention` が成功 7 日・失敗 30 日で削除するため、期間がそれより前に及ぶと成功は先に消え、`jobs[]` と `job_failures` に出ない（失敗は 30 日残る） |

`job_failures` の `code` は `jobs.last_error` の先頭 256 バイトから、固定パターン（kabu `code 4002006` / 立花 `p_errno=-62`・`sResultCode=NNN`）の数値トークン（`-` 付きでもよい 1〜8 桁）だけを取り出した値で、無ければ `unknown`。取り出しはプロセス内で行い、`last_error` の本文は返さない（SQL に `last_error` が現れるのは `substr(last_error, 1, N)`（N は 256 以下）の形だけで、`readonly_db_test` が確認する）。

返さない: `jobs.payload_json`、`jobs.last_error` の本文、`daily_bar_runs.error`、`daily_bars` の価格・出来高、スキップ理由の自由文。

### 3.9 `health_today`

引数: なし（`from` / `to` を含む引数は `invalid_argument`）。「今日」は呼び出し時刻の JST の暦日で、取引日かどうかは `marketcalendar.TSE.IsTradingDay` で判定する。1 回の呼び出しで次のすべてを返す。戻り値は固定コード・真偽・件数・時刻だけで、自由文・板・価格・秘密は含まない。

| フィールド | 定義 |
|------------|------|
| `date` / `trading_day` | 今日（JST、`YYYY-MM-DD`）と、東証立会日か |
| `needs_attention` | `attention_codes` が空でなければ `true` |
| `attention_codes[]` | 要対応の固定コード（下表）。無ければ `[]`。並びは下表の順 |
| `paper_trades` | `opened_count`（`positions.opened_at` が今日の件数）、`closed_count`（`closed_at` が今日の件数）。価格は返さない |
| `meta_model` | `adopted`（`metalabel_models.status='active'` が 1 件以上）、`version`・`trained_at`（採用中で最新のもの。無ければ `null`） |
| `bootstrap` | `enabled`（`runtime_settings` の `paper.bootstrap_entry_enabled`。無い・解釈できない値は `false`。#798）、`effective`（`enabled` かつ採用中モデルが無い。ブートストラップが働く条件。本 MCP は Paper の DB だけを読み、アプリは Live では設定に関わらず使わない）、`signals_today`（今日のシグナルのうち `policy_version` がブートストラップ版のもの） |
| `market_data` | `kabu_login`（`ok` / `not_logged_in` / `unknown` / `not_watched`）、`market_data`（`running` / `idle` / `not_watched`）、`app_running`（アプリの検知の最終確認が 2 分以内か）、`alerts[]`（`collection_status.alerts[]` と同じ値。`code` と `since`）。`not_watched` は検知が見ない時間（非取引日、08:45〜15:30 の外、`market_data` は立会外・昼休み）。`unknown` は見る時間なのにアプリが止まっていて判断できない状態 |
| `missing_session_dates[]` | 今日より前の 7 日間（今日の引け後は今日も含む）の東証立会日のうち、`market_snapshots` が 0 件で、**それ以前にスナップショットが 1 件でもある**日付（実際の欠損）。`collection_status.missing_session_dates` は履歴の長さを見ずに 0 件の日をすべて返すので、初回起動直後は両者が異なる（#814） |
| `insufficient_history_dates[]` | 同じ期間の東証立会日のうち、`market_snapshots` が 0 件で、**それ以前のスナップショットも 1 件も無い**日付（履歴が足りず判定できない。初回起動直後など）。欠損とは区別し、警告にしない（`attention_codes` に出さない）。該当が無ければ（履歴が 7 日分そろえば）`[]`（#814） |
| `daily_guard` | `halted`、`reason`（`daily_loss_limit_jpy` / `profit_lock` / `max_daily_trades`。`halted` でなければ `null`）、`halted_at`（UTC RFC3339）。アプリの `risk.Engine.DailyHalt` と同じく、今日の取引日の停止だけを、その規則が Settings でまだ有効なときに限って返す。停止メッセージの文は返さない |
| `kill_switch` | `active`（解除されていない `kill_switch_events` がある、または `system.killed` が真）、`unresolved_count`、`since`（未解除で最新の `triggered_at`）、`reasons[]`（未解除の `reason` コード。§3.7 の CHECK 値）、`flag_set`（`system.killed`）。`risk.Engine.State` と同じ判定 |
| `retrain` | `skipped`（最新の `metalabel_retrain_skips` が 24 時間以内で、それ以後に学習されたモデルが無い）、`latest_skipped_at`、`reason_code`（§3.5 の固定コード。未知は `other`）。見送りが一度も無ければ後ろ 2 つは `null` |
| `order_proposals` | `stalled_count`（`order_proposals.status` が `received` / `processing` のまま、最後の状態の変化 `updated_at` から 10 分を超えた案の数）、`oldest_stalled_since`（その中で最も古い `updated_at`、UTC RFC3339。無ければ `null`）。案の内容は返さない（issue #829） |

要対応コード（`query.Attention*`。`kabu_not_logged_in` と `market_data_idle` はアプリ内警告（#802）と同じコードで、再導出せず `health_alerts` を読む）:

| コード | 条件 |
|--------|------|
| `no_paper_trades_today` | 取引日で、前場の引け（11:30 JST）以後に、今日開いた Paper ポジションが 0 件 |
| `no_active_model` | 採用中のメタモデルが無い（日によらない） |
| `kabu_not_logged_in` | `market_data.alerts[]` に同名の警告がある（取引日の 08:45〜15:30 のみ） |
| `market_data_idle` | `market_data.alerts[]` に同名の警告がある（立会中のみ） |
| `missing_session` | `missing_session_dates[]` が空でない（取引日・非取引日とも直近 7 日の取引日を見る）。`insufficient_history_dates[]` だけが空でないときは出さない（データ不足は警告でなく判定不可） |
| `daily_guard_halted` | `daily_guard.halted` |
| `kill_switch_active` | `kill_switch.active`（日によらない） |
| `retrain_skipped` | `retrain.skipped` |
| `order_proposal_stalled` | `order_proposals.stalled_count` が 1 以上（アプリが止まっている・処理が詰まっている可能性。`waiting_session` と `submitted` は待つ設計なので含めない。日によらず出す） |

非取引日（土日・祝日）は、取引数・kabu・market-data の判定を行わない（コードを出さない）。モデル・Kill Switch・日次停止・再学習の見送りは状態そのものなので、日によらず出す。

`runtime_settings` は固定キー（日次ガードの閾値キー、`system.daily_guard`、`system.killed`、`paper.bootstrap_entry_enabled`）だけを読み取りで読む（§4）。

### 3.10 設定ツール（issue #806・#807）

設定ツールが触れるのは、次のホワイトリストの 17 キーだけである（`internal/config/mcpwritesettings`。仕様と理由は `architecture/paper-results-mcp.md` §7）。引数は `period` を持たない。

| キー | 単位 | 意味 | 受け付ける値 |
|------|------|------|--------------|
| `risk.paper.initial_capital_jpy` | 円 | Paper の運用資金（FR-RISK 各上限と数量算出の基準。issue #804） | 0 円より大きく 1 兆円以下 |
| `risk.paper.max_position_per_symbol_pct` | % | Paper の 1 銘柄あたりの建玉上限 | 0 より大きく 100 以下 |
| `risk.paper.max_total_exposure_pct` | % | Paper の総エクスポージャ上限 | 0 より大きく 100 以下 |
| `risk.paper.max_trade_loss_pct` | % | Paper の 1 回の最大損失 | `papersettings.MinTradeLossPct`（0.1）以上 `MaxTradeLossPct`（3.0）以下 |
| `risk.bucket.stock.budget_jpy` | 円 | Paper の株の予算枠（issue #824）。保有中の株の建玉（時価ベース。総エクスポージャ上限と同じ計算）に今回のエントリー額を足してこの額を超えると新規エントリーを見送る。0 は株の新規エントリーを止める | 0 以上 1 兆円以下の整数。株と債券ETFの合計が運用資金以下 |
| `risk.bucket.bond_etf.budget_jpy` | 円 | Paper の債券ETFの予算枠（issue #824）。0 はこの枠を使わない | 0 以上 1 兆円以下の整数。株と債券ETFの合計が運用資金以下 |
| `risk.operating_capital_jpy` | 円 | 日次ガードの運用資金（FR-RISK-8）。0 は未設定 | 0 以上 1 兆円以下 |
| `risk.daily_loss_limit_pct` | % | 日次損失上限（運用資金比） | 0 以上 100 以下（0 は使わない） |
| `risk.profit_lock.trigger_pct` | % | 利益確保の発動水準（運用資金比） | 0 以上 100 以下 |
| `risk.profit_lock.giveback_pct` | % | 利益確保の戻し許容 | 0 以上 100 以下 |
| `risk.max_daily_trades` | 回 | 1 日の最大エントリー回数 | 0 以上の整数 |
| `risk.daily_loss_limit_jpy` | 円 | 日次損失上限（円で直接指定） | 0 以上 |
| `risk.profit_lock.trigger_jpy` | 円 | 利益確保の発動水準（円で直接指定） | 0 以上 |
| `metalabel.llm_cutoff` | 日付 | Jev が使う LLM の学習データの終端日（UTC） | `YYYY-MM-DD` |
| `metalabel.llm_cutoff_model` | 文字列 | 上の終端日を確認した Jev のモデル名（出典） | 200 文字以内・制御文字なし |
| `metalabel.llm_cutoff_confirmed_on` | 日付 | 上の終端日を確認した日（出典） | `YYYY-MM-DD` |
| `metalabel.llm_cutoff_source` | 文字列 | 上の終端日の根拠（出典） | 200 文字以内・制御文字なし |

Live の値・Paper ブートストラップのスイッチ（`paper.bootstrap_entry_enabled`）・秘密・発注・送金・Kill Switch 解除に関するキーは無い。ホワイトリスト外のキーは `key_not_allowed`（HTTP 403）。`value` は文字列でも数値でもよい（Settings 画面に入れる文字列と同じ。数値キーに `2.5` でも `"2.5"` でも可）。真偽値・null・配列・オブジェクトは `invalid_argument`。

### 3.11 `settings_get`

引数なし。`data`:

| フィールド | 定義 |
|------------|------|
| `write_enabled` | 書き込み許可（既定 `true`。`runtime_settings` の `mcp.settings_write_enabled`） |
| `snapshot_id` | 最新の監査行の `id`。変更がまだ無ければ 0。`settings_diff` の `since_id` に渡せる |
| `llm_cutoff_status` | LLM cutoff（`metalabel.llm_cutoff` と出典 3 キー）の状態の固定コード。`placeholder`（未設定または仮置き `2026-01-01` のまま。夜間再学習のモデルは採用されない）／`unverified`（実日付だが確認した Jev モデル名 `metalabel.llm_cutoff_model` が未記録。Paper では警告のまま採用、Live 前チェックでは採用を止める）／`confirmed`（実日付とモデル名が記録済み）。Settings 画面の状態表示と同じ分類（`metalabelsettings.Settings.CutoffStatus`） |
| `items[]` | ホワイトリストの全キー（上の表の順）。`key` / `value`（Settings 画面と同じ文字列）/ `unit`（`yen`・`percent`・`count`・`date`・`text`）/ `source`（`stored`＝保存値、`default`＝保存値なしで既定が効いている）/ `default` / `description` / `constraint`（受け付ける値）/ `warning`（あれば。例: LLM cutoff が仮の値のまま） |

Paper の 4 キーの `default` は `risk.yaml` の Paper 値（なければ #804 の初期値）。予算枠の `default` は、株が運用資金の全額、債券ETFが 0（枠を設ける前と同じ挙動）。秘密に分類されるキーはホワイトリストに無いが、将来載せる場合は `value` と `default` が `****` になる。読み取り専用。

### 3.12 `settings_set`

引数: `key`（必須）、`value`（必須）。処理の順は ①ホワイトリスト（`key_not_allowed`）②書き込み許可（`write_disabled`）③値の検証（`invalid_value`、`invalid_argument`）④保存と監査（同じトランザクション）。`data`: `key` / `unit` / `value`（保存後の値）/ `before`（保存前の保存値。保存値が無かったときは `null`）/ `audit_id` / `at`。LLM cutoff の 4 キー（下記）を書いたときは `llm_cutoff_status`（保存後の状態コード。§3.11 と同じ）も付く。保存先は GUI と同じ `runtime_settings` で、アプリには保存後すぐに反映される。書き込みは `readOnlyHint: false`。

LLM cutoff を MCP だけで更新する手順（issue #809）: 公式のモデルカード／ドキュメントで、使っている Jev モデルの学習データの終わり（knowledge cutoff）を確認し、`settings_set` を 1 キー 1 回ずつ呼ぶ（順序は任意。推奨は下の順）。

1. `metalabel.llm_cutoff` = `YYYY-MM-DD`（公式の日付。UTC 日）
2. `metalabel.llm_cutoff_model` = 確認した Jev のモデル名（判断の記録の `model_id` と同じ表記。200 文字以内・制御文字なし）
3. `metalabel.llm_cutoff_confirmed_on` = 確認した日（`YYYY-MM-DD`）
4. `metalabel.llm_cutoff_source` = 見たページの URL か文書名（200 文字以内・制御文字なし）

各呼び出しは個別に検証・保存・監査され、途中でも一貫した状態になる: 日付だけ・日付＋確認日＋出典だけ（モデル名なし）は `unverified` のまま（警告が残り、Live 前チェックでは採用が止まる）、モデル名まで記録すると `confirmed` になり、仮置きの警告は消える。仮置き `2026-01-01` を実日付に変えるまでは `placeholder`。公式に日付が載っていないときは推測で入れず仮置きのままにする。各キーの `description` に同じ手順が載る。結果は `settings_get` の `llm_cutoff_status` で確認できる。採用判定（FR-META-6、Live 前の `unverified` 不採用を含む）はこの更新で変わらない。

予算枠（`risk.bucket.stock.budget_jpy` / `risk.bucket.bond_etf.budget_jpy`）の保存（issue #824）: 株と債券ETFの合計が運用資金（`risk.paper.initial_capital_jpy`）を超える値は `invalid_value` で拒否し、設定も監査行も残さない。運用資金を枠の合計より下げる保存も同じく拒否する。枠の間で予算を移すツールは無く、`settings_set` を 1 キーずつ呼ぶ。**減らす方の枠を先に保存し、そのあとで増やす方の枠を保存する**（先に増やす方を保存すると、途中の合計が運用資金を超えて拒否される）。

### 3.13 `settings_diff`

引数（どちらも省略可。両方は不可で `invalid_argument`）: `since_id`（監査ログの行 ID。その行の保存直後の状態と比べる。0 は最初から。存在しない ID は `invalid_argument`）、`since_at`（RFC3339。その時点の状態と比べる）。両方省略は、直近 1 回の保存の直前との差分。

`data`: `since`（`id`、あれば `at`）、`snapshot_id`（いまの最新）、`changes[]`（値か出どころが違うキーだけ。ホワイトリストの順）。各要素は `key` / `unit` / `before`・`after`（それぞれ `value` と `source`）/ `changed_at` / `changed_by`（`mcp:<IP>` か `gui`）。基準の状態は監査ログから再構成する（`architecture/paper-results-mcp.md` §7.5）。値は §3.11 と同じ表現で、秘密は載らない。読み取り専用。

### 3.14 `settings_audit`

引数: `limit`（既定 50、最大 200）、`before_id`（前回応答の最後の `id`。これより古い行を返す）。`data.entries[]` は新しい順で、`id` / `at`（UTC の RFC3339）/ `actor`（`mcp` か `gui`）/ `client`（`mcp` のときの接続元 IP。`gui` は省略）/ `action`（`set`・`reset`・`write_enable`・`write_disable`・`order_proposals_enable`・`order_proposals_disable`）/ `key` / `before` / `after`。`before` / `after` が `null` は「保存値なし（既定が効く）」、`reset` の `after` も `null`。書き込み許可の切り替えは `key` が `mcp.settings_write_enabled`、注文案の受け付けスイッチの切り替え（§3.16〜§3.18。分割章）は `key` が `mcp.order_proposals_enabled`。どちらも値は `"1"` / `"0"`。値は GUI と同じ表現で、マスク方針は `architecture/paper-results-mcp.md` §7.4（いまのキーに秘密は無く、そのまま。秘密に分類されるキーは `****`）。読み取り専用。

### 3.15 `settings_write_enable` / `settings_write_disable`

引数なし。書き込み許可を入れる／切る。**書き込みが禁止のあいだも呼べる**（戻せなくなるため）。Bearer 認証は他と同じく必須。`data`: `write_enabled` / `audit_id` / `at`。切り替えは監査ログに残る。Settings 画面の同名のチェック（FR-MCP-18）と同じ値を読み書きする。`readOnlyHint: false`。これらのツールも、発注・送金・Kill Switch・Live の値には触れない。

### 3.16〜3.22 注文案ツール（issue #827・#828・#829）

書き込み: `order_proposal_submit` / `order_proposal_cancel` / `order_proposals_disable`（Paper の注文案を受け付けて記録し、取消の依頼を記録するだけ。Risk check・発注・取消はアプリ。issue #828。`architecture/order-proposal-processing.md`）。読み取り専用: `order_proposals_list` / `order_proposal_get` / `order_proposal_summary`（提案 → 約定 → 損益のひも付け。issue #829）と、`paper_trades` / `paper_performance_summary` の `origin`。引数・応答・エラーは分割章 `paper-results-mcp-proposals.md` に置く（ファイルの行数上限のため。章番号は同じ §3.16〜§3.22）。**§3.29 Jev プロンプトのツール（issue #872）**: 読み取り専用: `jev_shadow_compare`（ライブの trader の判断と、同じ入力に対するシャドー＝候補のプロンプトの版の判断を、`entry_quality` の分布・`direction` の一致率・費用・`calibration_outcomes` による結果で並べる）と `jev_shadow_cost`（シャドーの `request_cost` の日次・累計、上限と止まった記録、昇格の記録）。書き込み: `jev_prompt_promote`（ライブの版を候補の版に切り替える明示的な操作。書き込み許可のスイッチに従い、監査に残る。自動では呼ばれず、自己改善ループには経路が無い）。シャドーの有効化・候補の選択・費用の上限は Settings 画面だけで、**費用の上限の値は暫定の案で未承認**。引数・応答・エラーは分割章 `paper-results-mcp-jev-prompt.md` に置く（ファイルの行数上限のため。章番号は同じ §3.29）。

## 4. 出さないもの

ツールの戻り値はホワイトリスト方式とする。テーブルの `SELECT *` は禁止する。下の表は、列が存在してもツールに載せないものの一覧である（マイグレーション `000001`〜`000039` で確認した名前）。

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
| `runtime_settings` の値 | 秘密鍵のパス等が入りうる。読むのは次の固定キーだけ: `health_today`（§3.9）の日次ガードの閾値・`system.daily_guard`・`system.killed`・`paper.bootstrap_entry_enabled`（値そのものは返さず、解釈した真偽・コード・件数だけ）、設定ツール（§3.10）のホワイトリストの 17 キーと書き込み許可のスイッチ `mcp.settings_write_enabled`、注文案の受け付けスイッチ `mcp.order_proposals_enabled`（書き込み接続だけが、書き込みの前に 1 回読む。読み取りのツールは返さない）、`paper_performance_summary` の分母 `risk.paper.initial_capital_jpy`（1 キーだけ）。`jev_shadow_cost`（§3.29）の Jev のプロンプトの 6 キー（`jev.trader_prompt_version`・`jev.shadow.enabled`・`jev.shadow.candidate_version`・`jev.shadow.daily_cost_cap_usd`・`jev.shadow.halt`・`jev.shadow.failures`。版の名前・真偽・USD の数値・止まった記録・失敗回数で、秘密は無い）。それ以外は読まない。設定ツールが返す値は §3.11・§3.14 のとおり（ホワイトリストに秘密は無い） |
| `order_proposals.request_hash` / `client` | `000042`。照合用のハッシュと接続元 IP。`order_proposal_get` は返さない |
| sqlite-vec の仮想テーブル | 市場状態の埋め込み。本 MCP は読まない |

Paper の約定記録（`entry_price` / `exit_price` / `realized_pnl` / `fees_jpy`）は取引の結果として返す。板やスナップショットの時価列とは別である。

## 5. 受け入れ条件（テストで確認できること）

- 既定の設定ファイル（`enabled: false`）では `127.0.0.1:48110` に接続できない
- `enabled: true` かつ `api_key` が非空で、そのキーが正しいときだけ待受し、ツールが応答する。`api_key` が空なら待受しない
- `listen` が `0.0.0.0` のときは起動しない。Tailscale で直接待つ設定は、その PC の tailnet 側アドレスだけを bind する
- キー無し・誤ったキーは 401 で、そのリクエストの経路で SQL が 0 件である
- MCP の読み取り接続に対する `INSERT` はエラーになる。`PRAGMA wal_checkpoint` とマイグレーションを呼んだ痕跡が無い
- 設定ツール: ホワイトリストの 17 キーはすべて MCP から更新でき、GUI（`opsettings`）が同じ値を読む。ホワイトリスト外（Live・ブートストラップ・秘密・書き込みスイッチ自身・未知）は `key_not_allowed` で、`runtime_settings` と監査に何も残らない。書き込みが禁止のとき `settings_set` は `write_disabled` で何も保存せず、`settings_write_enable` は呼べる。範囲外の値は GUI と同じ検証で `invalid_value`。予算枠の合計（株 + 債券ETF）が運用資金を超える保存（枠の保存・運用資金を下げる保存）は `invalid_value` で、設定も監査行も残らず、減らす方を先に保存すれば通る。書き込み許可は既定でオン。認証なしの設定ツール呼び出しは SQL 0 件・接続 0 本。実行された更新文は `runtime_settings` の upsert と `mcp_settings_audit` の `INSERT` だけ
- 設定の一覧（`settings_get`）→ 変更（`settings_set`）→ 差分（`settings_diff`）が `snapshot_id` でつながり、監査ログ（`settings_audit`）に誰が・いつ・どのキーを・前後の値が出る。出力に秘密・API キー・生データが出ない
- 注文案ツールの受け入れ条件は `paper-results-mcp-proposals.md` §5（スイッチ・同一トランザクション・冪等・引数の規則・1 日の上限・認証なしで SQL 0 件・書き込みの狭さ・`mode` の CHECK・オンにする手段が無いこと）
- 既知の数件を入れたテスト DB で、§3.1 の勝率・期待値・円ドローダウン・資金比が手計算と一致する
- 戻り値の JSON とログに、`raw_data_json` の中身、`state_json` の中身、`secrets.encrypted_value`、テスト用 API キー文字列が含まれない
- 期間 367 日、`limit=201`、クローズ済み 5,001 件は、それぞれ `invalid_period` または `limit_exceeded` で部分結果が無い
- `health_today` は、要対応コード 9 種それぞれが、その条件だけで入り・条件が解けると消え、取引日の健康な状態では `needs_attention` が `false` である。土日・祝日は取引数・kabu・market-data の判定でコードを出さない。戻り値に自由文（日次停止のメッセージ等）が含まれない
- 選んだ経路（Tailscale または Cloudflare Tunnel）経由で、認証つきでリモートから呼べることを実機で確認する。自動テストの対象外とし、キーを伏せた呼び出し記録を残す（#783 の実機条件）
- Tailscale を選んだときは、tailnet の外からその待受に届かないことを同じ記録で確認する

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-10-17 | 新規作成 | issue #783。ツールの入出力・上限・出さない列を、既存マイグレーションの列名で固定 |
| 1.1 | 2026-10-17 | 受け入れ条件の実機確認を、選んだ経路（Tailscale または Cloudflare Tunnel）経由に変えた。Tailscale は tailnet 外から届かないことも確認する。必須の 401 は API キーだけ | 悠太さん指示。Cloudflare を必須にしない |
| 1.2 | 2026-10-18 | トランスポートの要確認を、Streamable HTTP（POST のみ・JSON 応答）で実装した旨の回答に改めた。ツール・上限・ホワイトリストは変更なし | issue #783 の実装 |
| 1.3 | 2026-10-20 | §3.8 `collection_status` に `alerts[]`（`code`・`since`。`kabu_not_logged_in` / `market_data_idle`）を追加。マイグレーション `000039` の `health_alerts` / `health_alert_checks` を読む | issue #802 |
| 1.4 | 2026-10-20 | §3.1 `capital_jpy` の既定を 500,000 に更新し、Settings で保存した Paper の運用資金（`runtime_settings`）には追従しない旨（`runtime_settings` を読まない方針を維持）を明記 | issue #804 |
| 1.5 | 2026-10-20 | §3.9 `health_today` を追加（今日の Paper 取引数・メタモデル・ブートストラップ・kabu/market-data・欠損・日次ガード・Kill Switch・再学習の見送りと、要対応フラグ `needs_attention` / 固定コード `attention_codes[]`）。§4 の `runtime_settings` を、固定キーのみ読み取りで読む記述に改めた | issue #808 |
| 1.6 | 2026-10-20 | §3.1 `capital_jpy` を、Settings の保存値（`runtime_settings` の `risk.paper.initial_capital_jpy`）に追従し、なければ risk.yaml とする仕様に更新。§3.10〜§3.15 に設定ツール（`settings_get` / `settings_set` / `settings_diff` / `settings_audit` / `settings_write_enable` / `settings_write_disable`）とホワイトリスト 15 キー、§4 の `runtime_settings` の行、§5 の受け入れ条件を追加 | issue #806・#807 |
| 1.7 | 2026-10-20 | §3.11 `settings_get` に `llm_cutoff_status`（`placeholder` / `unverified` / `confirmed`）を追加。§3.12 `settings_set` の LLM cutoff 4 キーの応答に `llm_cutoff_status` を付け、MCP だけで日付と出典を更新する手順を追記 | issue #809 |
| 1.8 | 2026-10-20 | §3.9 `health_today` の `missing_session_dates[]` を、それ以前のスナップショットがある日付だけ（実際の欠損）に改め、履歴が足りず判定できない立会日を `insufficient_history_dates[]` に分けた。`attention_codes` の `missing_session` は `insufficient_history_dates[]` だけが空でないときは出さない | issue #814 |
| 1.9 | 2026-10-20 | §3.10 のホワイトリストに Paper の予算枠 2 キー（`risk.bucket.stock.budget_jpy`・`risk.bucket.bond_etf.budget_jpy`）を足して 17 キーにした（`settings_get` / `settings_diff` にも出る）。§3.12 に、枠の合計が運用資金を超える保存の拒否（監査行なし）と、枠の間で予算を移すときは減らす方を先に保存する手順を追記。§4・§5 の 15 キーを 17 キーに改めた。ツールの数・入出力の形は変更なし | issue #824（親 #823） |
| 1.10 | 2026-10-21 | §3.16〜§3.18（分割章 `paper-results-mcp-proposals.md`）に注文案ツール（`order_proposal_submit` / `order_proposal_get` / `order_proposals_disable`。Paper の注文案を受け付けて記録するだけ）を追加し、ツール数を 18 にした。§3.14 の `action` に `order_proposals_enable` / `order_proposals_disable` を追加。§4 に受け付けスイッチの読み取りと `order_proposals.request_hash` / `client` を、§5 に注文案の受け入れ条件を追加。ホワイトリストは 17 キーのまま | issue #827（親 #826） |
| 1.11 | 2026-10-21 | §3.16〜§3.19（分割章）に `order_proposal_cancel`（取消の依頼を記録するだけ）を追加しツール数を 19 にした。§3.6 の `risk_rejections` に `proposal_risk_passed_count` / `proposal_risk_rejected_count` を追加。§3.3 の `exit_reason` の既知の値に `proposal_close` を追加 | issue #828（親 #826） |
| 1.12 | 2026-10-21 | §3.17〜§3.22（分割章）に `order_proposals_list` / `order_proposal_summary` を追加し、`order_proposal_get` に結果（signal・order・position・最大損失・損切りと利確に届いたか）を足してツール数を 21 にした（読み取り 9・設定 6・注文案 6）。§3.1 `paper_performance_summary` に `by_origin`、§3.3 `paper_trades` に `origin` を追加。§3.9 `health_today` に `order_proposals` と要対応コード `order_proposal_stalled` を追加（9 種）。読み取り接続のままで、新規テーブルなし | issue #829（親 #826） |
| 1.13 | 2026-10-22 | §3.29（分割章 `paper-results-mcp-jev-prompt.md`）に Jev のプロンプトのツール `jev_shadow_compare` / `jev_shadow_cost` / `jev_prompt_promote` を追加しツール数を 24 にした。§4 に `jev_shadow_decisions` / `jev_prompt_promotions` の出さない列と、読む `runtime_settings` の Jev の 5 キーを追記。読み取り接続のまま。書き込みは昇格の 1 種類 | issue #872 |
