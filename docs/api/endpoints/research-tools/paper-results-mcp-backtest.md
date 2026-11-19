# API 仕様: Paper 実績 MCP の研究用バックテストのツール

`docs/api/endpoints/paper-results-mcp.md` §3 の分割章（行数上限のため別ファイル）。issue #1086。共通の上限・トランスポート・エラー形式は `paper-results-mcp.md` と `docs/architecture/paper-results-mcp-errors.md` に従う。設計・上限の根拠・記録の表は `docs/architecture/paper-results-mcp-backtest.md`、`docs/architecture/er/tables-backtest.md`。

3 つのツール: `backtest_start`（書く。バックテストを 1 回起動する）、`backtest_get` / `backtest_list`（読み取り専用）。バックテストは**保存済みのデータ（`market_snapshots` / `jev_decisions`）を再生するだけ**で、`runtime_settings` を含む設定を読み替えも書き換えもしない。結果は研究用の記録で、Policy・Risk・発注・資金配分には使われない。

## 3. ツール

### backtest_start

保存済みのスナップショットと Jev の判断を、売買の条件（`variants`）ごとに再生する。**非同期**: すぐに `status = queued` の行（`run_id`）を返し、再生はバックグラウンドで進む。終わったかどうかと結果は `backtest_get` で読む。

引数:

| 名前 | 型 | 内容 |
|---|---|---|
| `from` / `to` | 文字列 | JST の日付 `YYYY-MM-DD`（`to` を含まない）。他のツールと同じ形式・同じ検査（`invalid_period`）。両方省略は直近 30 日（今日を含む）。**期間は最大 31 日**。超えると `limit_exceeded` |
| `replay_mode` | 文字列 | `paper`（既定。`min_entry_quality` に `policy.paper.*` を使う）/ `live`（`policy.*`）。これ以外は `invalid_argument` |
| `variants` | 配列 | 試す条件の組。**1〜12 組**。省略は基準 1 組（いまの設定のまま）。13 組以上は `limit_exceeded` |
| `extra_cost_bps` | 数値の配列 | 追加の往復コスト（bps）の感度表の行。**6 個まで**（超えると `limit_exceeded`）。各値は 0 以上 200 以下（外れると `invalid_value`）。省略は `[0, 2, 5, 10]` |

`variants[]` の要素は `{label, params}`:

- `label`（任意。40 文字まで。超えると `invalid_value`）: 省略は、何も変えない組が `baseline`、それ以外が `v<組の番号>`（1 始まり）。
- `params`（任意）: 書いた項目だけを基準から変える。**書かなかった項目は、いま Settings / strategy.yaml / risk.yaml にある値のまま**。

| `params` の項目 | 内容 | 許容範囲 |
|---|---|---|
| `entry_rule` | `meta`（ライブの規則: メタモデルの判定）/ `legacy_confidence`（Jev の確信度を `min_probability_*` と比べる比較用） | 左のどちらか |
| `min_ev_bps` | `meta` 規則が見る期待値（bps）の下限 | 0〜1000 |
| `min_entry_quality_long` / `_short` | 買い・売りの最低エントリー品質 | `poor` / `fair` / `good` / `strong` / `exceptional` |
| `min_probability_long` / `_short` | `legacy_confidence` だけが読む確信度の下限 | 0 より大きく 1 以下 |
| `min_continuation_probability_long` / `_short` | 継続確率の下限 | 0〜1 |
| `stop_loss_pct` / `take_profit_pct` / `trailing_stop_pct` | 損切り・利確・トレーリング（%。0 は使わない）。`stop_loss_pct` はポジションサイズの損切り幅にも使われる | 0〜100 |
| `max_holding_minutes` | 最大保有時間（分。0 は使わない） | 0〜1440 |
| `slippage_bps` / `fee_bps` | 両方向のスリッページ・比例手数料を置き換える（コストモデルのほかの項目はそのまま） | 0〜200 |
| `max_trade_loss_pct` | 1 取引の最大損失（資金に対する %） | 0.1〜3 |
| `max_position_per_symbol_pct` | 1 銘柄の最大ポジション（%） | 0 より大きく 100 以下 |

範囲外は `invalid_value`（`error.data.detail` に `variants[1].params.stop_loss_pct: 0 以上 100 以下（0 は使わない）` のように、**項目名と許容範囲だけ**。送った値は引かない）。未知の項目・型違い・未知の引数は `invalid_argument`（`variants[].params` の中の未知のキーも拒否する）。

実行の規則:

- 組は**同じ期間・同じデータ**で 1 組ずつ順に再生する（データは 1 回の実行で 1 回だけ読む）。再生は実際のリプレイエンジン（Policy・Risk の模擬・約定モデル・手数料）を通る。
- **同時に走れるのは 1 件**。実行中にもう一度呼ぶと `concurrency_limited`（`detail` に理由。**何も書かない**）。終わればまた呼べる。
- **1 回の実行は 10 分まで**。超えるとその実行だけが止まり、`failed`・`error_code = timed_out` になる（ほかのツール・取引処理には影響しない）。
- MCP の書き込み許可（`settings_write_enable` / `settings_write_disable`）がオフのときは `write_disabled`（何も書かない）。受け付けたあとにオフにされても、その実行は終わりまで記録される。
- 再生用のデータを読む設定が無い（または DB を読めない）ときは `backtest_unavailable`（503。何も書かない）。実行が始まってから再生用のデータを開けなかったときは、その実行が `failed`・`error_code = unavailable`（読み込み中の失敗は `internal`）。

応答の `data`: `backtest_get` の「実行の行」（下の表）と同じキー（`status = queued`・`started_at` / `finished_at` / `error_code` は `null`）。

### backtest_get

`run_id`（必須。1 以上の整数）の実行 1 件の状態と結果。無いと `backtest_not_found`（404）。`run_id` が欠ける・0 以下・整数でないと `invalid_argument`。

実行の行（`backtest_start` / `backtest_list` の行も同じ）:

| キー | 内容 |
|---|---|
| `run_id` / `status` | `status` は `queued` / `running` / `succeeded` / `failed` |
| `created_at` / `started_at` / `finished_at` | UTC の RFC3339（秒）。まだ無いものは `null` |
| `period` | `from` / `to`（依頼した JST の日付。`to` を含まない）と、`first_snapshot_at` / `last_snapshot_at`（期間内に**実際にあった**スナップショットの最初と最後の時刻。データが無い期間・終了前は `null`） |
| `replay_mode` | `paper` / `live` |
| `code_version` | 記録時のビルドの版（`internal/version.Version`。ブランチ・PR のビルドは `dev`） |
| `explored_count` | 試した組の数。最良値はこの数の中から選んだ値 |
| `error_code` | 失敗のときだけ固定コード（下）。それ以外は `null` |
| `caution` | 常に固定文: 多重比較。最良値は参考で、複数の組から選んだ最良値は偶然で良く見える |

`backtest_get` はさらに:

| キー | 内容 |
|---|---|
| `extra_cost_bps` / `requested_variants` | 依頼した追加コストの行と組（`[{label, params}]`。省略した組は補った後の値）。常に返す |
| `variants` | `succeeded` のときだけ（それ以外は `null`）。依頼した順の、組ごとの結果（下の表） |

`variants[]`（金額ではなく、1 取引の価格に対する % の値。1.0 = 1%）:

| キー | 内容 |
|---|---|
| `index` / `label` / `params` | 組の番号（0 始まり）・名前・変えた項目（書いたものだけ。固定の順） |
| `trade_count` | 取引件数 |
| `win_rate` | 純リターンが 0 を超えた取引の割合（0〜1）。取引が無いと `null` |
| `expectancy_pct` | **コスト込み**の 1 取引あたりの平均純リターン（スリッページ・スプレッド・手数料を引いた後）。取引が無いと `null` |
| `max_drawdown_pct` | 純リターンの累積の最大の山から谷（決済時刻順） |
| `gross_pnl_pct` / `slippage_pnl_pct` / `fee_pnl_pct` / `net_pnl_pct` | コスト前・スリッページ後・手数料後・純の合計 |
| `expectancy_ci` | `expectancy_pct` の 95% 区間 `{lo, hi, level: 0.95, days, has_ci}`。**JST の決済日を 1 まとまり**にしたクラスター頑健な区間（同じ日の取引は独立でないため）。決済日が 2 日未満は `has_ci = false`・`lo` / `hi` は `null` |
| `days[]` | JST の決済日ごとの `date` / `trade_count` / `net_pnl_pct`（古い順） |
| `sensitivity[]` | 追加コストごと（`extra_cost_bps` の順）の `extra_cost_bps` / `expectancy_pct` / `expectancy_bps` / `positive_share`（追加コスト後も 0 を超える取引の割合）/ `predicted_ev_trade_count` / `predicted_ev_positive_share`（メタモデルの予測 EV が追加コストを超える割合。`legacy_confidence` は対象外）。`0` の行は結果そのもの |
| `is_reference_best` | 取引のあった組のうち期待値が最大の**1 組だけ**に `true`（同じなら番号の小さい方）。**参考印**であり、採用の推奨ではない（`explored_count` と `caution` と一緒に読む） |

同じ入力（保存データ・期間・組・コードの版）なら**結果は完全に同じ**（区間も含めバイト単位）。時計・乱数・並びの揺れを結果に入れない。

`error_code`（失敗のときだけ。エラー文・パスは返さない）:

| コード | 意味 |
|---|---|
| `timed_out` | 1 回 10 分の上限を超えて止めた |
| `interrupted` | MCP が止まった（または再起動で途中のまま残った）。再起動で残った行は、読むとすぐ `failed` / `interrupted` として返り（`finished_at` は `null`）、表の行は次の `backtest_start` で書き換わる |
| `unavailable` | 再生用のデータ（DB・設定）を開けなかった |
| `internal` | 上記以外 |

### backtest_list

起動した実行の一覧（`run_id` の新しい順）。引数: `status`（任意。上の 4 つ）、`limit`（任意。既定 20・最大 50。超えると `limit_exceeded`、1 未満は `invalid_argument`）、`cursor`（任意。前回応答の `next_cursor`。不正は `invalid_argument`）。`from` / `to` は取らない（期間は実行ごとの `period`）。応答は `runs[]`（上の「実行の行」。結果は含まない）と `next_cursor`（続きが無いと `null`）。

古い実行は自動では消えない（一覧は件数を絞って返す）。`status` の絞り込みは表の値で行うので、再起動で残った行は次の `backtest_start` までは `running` の絞り込みに入り、行の `status` は `failed` と読める。

## エラー

| `code` | HTTP | 条件 |
|---|---|---|
| `invalid_argument` | 422 | 未知の引数・型違い・`replay_mode` が `paper` / `live` でない・`run_id` / `limit` / `status` / `cursor` が不正 |
| `invalid_period` | 422 | `from` / `to` の形式・片方だけ・`to <= from` など（他のツールと同じ） |
| `invalid_value` | 422 | `params` の範囲外・`label` が 41 文字以上・`extra_cost_bps` の値が範囲外。`detail` に項目名と許容範囲 |
| `limit_exceeded` | 422 | 13 組以上・`extra_cost_bps` 7 個以上・期間 32 日以上・`limit` が 50 超。`detail` に上限 |
| `concurrency_limited` | 429 | 別のバックテストが実行中（同時 1 件）。何も書かない |
| `write_disabled` | 403 | MCP の書き込み許可がオフ（`backtest_start` のみ）。何も書かない |
| `backtest_unavailable` | 503 | 再生用のデータを読む設定が無い、または MCP が停止処理中（`backtest_start` のみ）。何も書かない |
| `backtest_not_found` | 404 | `backtest_get` の `run_id` の実行が無い |
| `query_timeout` / `db_locked` / `db_unavailable` / `internal` | | 共通（`backtest_start` の記録の書き込みもこの 3 秒に入る） |

実行の**中**の失敗（時間切れ・データを読めない・内部エラー）はツールのエラーにならず、その実行の `status = failed` と `error_code`（上の 4 つ）になる。

例（要点だけ）:

```json
{"name": "backtest_start", "arguments": {"from": "2026-10-05", "to": "2026-10-17",
  "variants": [{"label": "baseline"}, {"label": "tp-0.5", "params": {"take_profit_pct": 0.5}}]}}
```

```json
{"run_id": 7, "status": "queued", "created_at": "2026-11-15T03:00:00Z", "started_at": null, "finished_at": null,
 "period": {"from": "2026-10-05", "to": "2026-10-17", "first_snapshot_at": null, "last_snapshot_at": null},
 "replay_mode": "paper", "code_version": "dev", "explored_count": 2, "error_code": null, "caution": "多重比較: …"}
```

（数値は書式を示す架空の値。実データではない。）

## 変更履歴

| 版 | 日付 | 内容 | 出典 |
|---|---|---|---|
| 1.0 | 2026-11-15 | 新規 | issue #1086 |
