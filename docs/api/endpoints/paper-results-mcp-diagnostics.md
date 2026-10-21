# API 仕様: Paper 実績 MCP の収集診断ツール

`docs/api/endpoints/paper-results-mcp.md` §3.8.1 の分割章（行数上限のため別ファイル）。issue #850。共通の上限・トランスポート・成功ボディ `{"tool":"<名前>","data":{...}}` は元の文書、エラーの HTTP と `code` は `docs/architecture/paper-results-mcp-errors.md`。

## 3.8.1 `session_diagnostics`

引数: `from`, `to`（他のツールと同じ。省略は直近 30 日）。読む範囲は期間の末尾 14 日（`window.max_days`。長い期間は `truncated=true`。14 日は実装者の判断で、合成 DB の測定による）。`data` は固定コード・件数・日付・時刻だけで、`last_error`・`payload_json` の本文・価格・銘柄は返さない。

| フィールド | 定義 |
|------------|------|
| `window` | `from`・`to`（JST 日、`to` を含まない）、`requested_from`（指定された期間の開始）、`max_days`（14）、`truncated` |
| `days[]` | 範囲内の東証立会日（JST。古い順）。要素は下の表 |
| `current` | いまの kabu ログイン・market-data・アプリ稼働・警告（`health_today.market_data` と同じ値）。過去日の状態ではない。時間切れで読めなかったときだけ `null` |
| `unavailable[]` | DB が日ごとには持たないもの。`kabu_login_by_day`（過去日の kabu ログイン状態）と `app_start_by_day`（アプリの起動記録）。`health_alerts` は「いま出ている警告」だけ、`health_alert_checks` は最後の確認だけで、履歴が無い |
| `retention` | `snapshot_days`（90）/ `succeeded_job_days`（7）/ `failed_job_days`（30）。これより前の日の数は `null` |
| `note` | 上の限界の固定の注記 |
| `timed_out` | 内部の時間予算（`QueryTimeout` の手前。architecture §5）を超えたとき `true`。`query_timeout`（504）で全部捨てず、読めた分を返す。全部読めたら `false` |
| `incomplete[]` | 読めなかった項目: `current` / `market_data_jobs` / `other_queue_jobs` / `watch_lists` / `snapshot_rows:<JST 日>` / `succeeded_without_snapshot:<JST 日>`。全部読めたら空配列。日は新しい順に読むので、読めないのは古い日。ここに挙がった項目から作る数は `null`（`null` は「保持期間で消えた」ではなく「読めなかった」）で、そこから作る `observations[]` は出さない |

`days[]` の要素:

| フィールド | 定義 |
|------------|------|
| `date` | JST の日付 |
| `snapshot_rows` | その日の `market_snapshots` の行数。保持期間（90 日）で消えうる日は `null` |
| `market_data_jobs.succeeded` / `failed` | その日（`finished_at` の JST 日）に終わった `market-data` ジョブの件数。成功 7 日・失敗 30 日の保持より前の日（日の 00:00 が削除の境より前）は `null`（0 ではない） |
| `market_data_jobs.succeeded_without_snapshot` | 成功したジョブのうち、`payload_json` の固定キー `instrument_id` の銘柄の `market_snapshots` 行が `started_at`〜`finished_at` の間に無いものの数（レート制限で板取得を見送った `nil` 成功など）。`succeeded` が `null` の日は `null` |
| `other_queue_jobs_finished` | `market-data` と廃止済み `feature-calc` 以外のキューで、その日に終わった（成功・失敗）ジョブ数。アプリが動いていたことの間接的な目安。`null` は成功ジョブの保持より前 |
| `watch_list` | （時間切れで読めなかった日だけ `null`）`present`（`watch_lists` に行があるか）、`source`（`daily_screen` / `fixed` / `carried_over` / `fixed_fallback`。無ければ `null`）、`entries`（`watch_list_entries` の件数） |
| `observations[]` | 固定コード。`no_snapshots`（行 0）/ `no_market_data_jobs`（成功も失敗も 0）/ `all_market_data_jobs_failed`（成功 0・失敗あり）/ `succeeded_without_snapshot`（上の数が 1 以上）/ `watch_list_empty`（ウォッチリストが無いか 0 件）/ `other_queues_idle`（他キューが 0）。件数が `null` のコードは出さない。原因の断定ではなく手がかり |

例（2026-10-21 17:45 JST に `{"from":"2026-10-14","to":"2026-10-17"}`。一部）:

```json
{"window":{"from":"2026-10-14","to":"2026-10-17","requested_from":"2026-10-14","max_days":14,"truncated":false},
 "days":[{"date":"2026-10-15","snapshot_rows":0,
          "market_data_jobs":{"succeeded":0,"failed":3,"succeeded_without_snapshot":0},
          "other_queue_jobs_finished":1,"watch_list":{"present":true,"source":"daily_screen","entries":5},
          "observations":["no_snapshots","all_market_data_jobs_failed"]}],
 "current":{"kabu_login":"not_watched","market_data":"not_watched","app_running":true,"alerts":[]},
 "unavailable":["kabu_login_by_day","app_start_by_day"],"timed_out":false,"incomplete":[]}
```

時間切れの例（3 番目に読んだ日 10-19 の行数の文が終わらなかった。新しい 10-21・10-20 は読めている。一部）:

```json
{"timed_out":true,
 "incomplete":["snapshot_rows:2026-10-19","succeeded_without_snapshot:2026-10-19","snapshot_rows:2026-10-16","..."],
 "days":[{"date":"2026-10-19","snapshot_rows":null,"market_data_jobs":{"succeeded":2,"failed":0,"succeeded_without_snapshot":null},"observations":["watch_list_empty"]},
         {"date":"2026-10-20","snapshot_rows":0,"observations":["no_snapshots","no_market_data_jobs"]}]}
```

## 受け入れ条件

- `session_diagnostics` は、欠けた立会日（`market_snapshots` 0 件）ごとに、ジョブの成功/失敗/成功なのに行なし、ウォッチリストの件数、観測コードを読める。保持期間で消えた日の数は 0 でなく `null`。`collection_status` は 30 日・366 日でも失敗の内訳を末尾 3 日（`job_failures.window`）に絞り、エラーコード別にまとめる。どちらも内部の時間予算を超えたら部分結果と `timed_out`・`incomplete[]` を返す（`diagtests`: `TestMissingDays`・`TestPartialResultWhenTheBudgetRunsOut`・`TestCollectionStatusMarksJobFailuresTimedOut`・`TestJobFailuresGroupByCodeAndAreWindowed`、`jobfailures`: `TestReadReturnsWhatItHasWhenTheBudgetRunsOut`）

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-10-21 | 新規。`session_diagnostics` の定義 | issue #850 |
| 1.1 | 2026-10-22 | 窓を 14 日にし、`window.requested_from`、`timed_out`、`incomplete[]`、`current`・`watch_list` の `null` を追加 | issue #850 |
