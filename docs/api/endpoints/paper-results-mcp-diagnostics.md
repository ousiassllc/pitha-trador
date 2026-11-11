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
| `broker_provider` | 保存されているブローカーの選択（`runtime_settings` の `broker.provider`。issue #914）: `kabu`（行が無い・読めない値のときも。アプリの起動時の読み方と同じ）/ `tachibana` / `unknown`（読めなかった）。`watch_lists` に書くのは立花のアダプタだけで、kabu の監視リストはメモリ上にあり保存されないため、kabu では空の表が毎日になる。そこで kabu の日は `watch_list_empty` でなく `watch_list_not_applicable` を出す。`unknown` のときは従来どおり `watch_list_empty` を出す（このツールは落ちない）。アプリは起動時に 1 度だけ読むので、変更後の最初の起動までは実際のブローカーと違いうる。kabu の実行時の監視リストの大きさは、この DB からは分からない |
| `incomplete[]` | 読めなかった項目: `current` / `market_data_jobs` / `other_queue_jobs` / `watch_lists` / `daily_records` / `broker_provider` / `snapshot_rows:<JST 日>` / `succeeded_without_snapshot:<JST 日>`。全部読めたら空配列。日は新しい順に読むので、読めないのは古い日。ここに挙がった項目から作る数は `null`（`null` は「保持期間で消えた」ではなく「読めなかった」）で、そこから作る `observations[]` は出さない |

`days[]` の要素:

| フィールド | 定義 |
|------------|------|
| `date` | JST の日付 |
| `snapshot_rows` | その日の `market_snapshots` の行数。保持期間（90 日）で消えうる日は `null` |
| `market_data_jobs.succeeded` / `failed` | その日（`finished_at` の JST 日）に終わった `market-data` ジョブの件数。成功 7 日・失敗 30 日の保持より前の日（日の 00:00 が削除の境より前）は `null`（0 ではない） |
| `market_data_jobs.succeeded_without_snapshot` | 成功したジョブのうち、`payload_json` の固定キー `instrument_id` の銘柄の `market_snapshots` 行が `started_at`〜`finished_at` の間に無いものの数（レート制限で板取得を見送った `nil` 成功など）。`succeeded` が `null` の日は `null` |
| `other_queue_jobs_finished` | `market-data` と廃止済み `feature-calc` 以外のキューで、その日に終わった（成功・失敗）ジョブ数。アプリが動いていたことの間接的な目安。`null` は成功ジョブの保持より前 |
| `watch_list` | （時間切れで読めなかった日だけ `null`）`present`（`watch_lists` に行があるか）、`source`（`daily_screen` / `fixed` / `carried_over` / `fixed_fallback`。無ければ `null`）、`entries`（`watch_list_entries` の件数） |
| `recorded` | アプリが大引けの後に残した日次記録（`session_diagnostics_daily`。issue #1054）。`collection_status.session_gaps.days[]` の要素と同じ形（`gap_kind`・`gap_codes[]`・`gaps[]`・`alert_codes[]`・`watch_checks`・`first_snapshot_at` など）。ジョブ・スナップショットの保持期間より後も残るので、上の件数が `null` の日でも読める。記録の無い日・観測中の日、または時間切れ（`incomplete[]` に `daily_records`）のときは `null` |
| `observations[]` | 固定コード。`no_snapshots`（行 0）/ `no_market_data_jobs`（成功も失敗も 0）/ `all_market_data_jobs_failed`（成功 0・失敗あり）/ `succeeded_without_snapshot`（上の数が 1 以上）/ `watch_list_empty`（ウォッチリストが無いか 0 件）/ `other_queues_idle`（他キューが 0）。件数が `null` のコードは出さない。原因の断定ではなく手がかり | 日次記録が日中の欠け（`gap_kind=intraday`）なら `intraday_gap`（コードは `recorded.gap_codes[]`。#1054）も付く

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
| 1.2 | 2026-10-26 | `broker_provider` を追加し、kabu のときは `watch_list_empty` の代わりに `watch_list_not_applicable` を出す（立花・`unknown` は従来どおり）。`incomplete[]` に `broker_provider` が加わりうる | issue #914 |

## 3.8.2 立会日のそろい（issue #1054）

`collection_status`・`health_today`・`session_diagnostics` が読む、アプリの日次記録 `session_diagnostics_daily`（マイグレーション000052。`docs/architecture/er/tables-system.md`）と、その判定。記録はアプリが大引けの 5 分後に 1 日 1 行で確定し、起動時に前営業日を確定する。成功ジョブの 7 日削除より長く残り、件数・時刻・固定コードだけで価格は持たない。MCP は読むだけで、記録の無い日を MCP が書き足すことも、欠けた日の市況を後から埋めることもしない。

判定は立会（前場 09:00〜11:30・後場 12:30〜15:30 JST。`marketcalendar.TSE.Sessions`）ごとに、スナップショットのある分を並べ、立会の始まりと最初の分・連続する 2 分・最後の分と立会の終わりが 15 分（`sessiongap.Tolerance`。市況データ停止の警告 `healthalert.IdleThreshold`＝5 分とは別の値）を超えて空いていれば欠けとする。休業日（2026-10-12、11-03 など）と土日は立会が無いので数えない。履歴の最初の日は、収集が始まる前の部分を欠けとしない。

`collection_status.session_gaps`（期間全体）:

| フィールド | 定義 |
|------------|------|
| `session_gaps` | 立会日ごとのそろい（issue #1054）。アプリが大引けの後に残す日次記録（`session_diagnostics_daily`。成功ジョブの 7 日削除より長く残る）を、期間全体（最大 366 日）で読む。`whole_day_dates[]`（終日 0 件）・`intraday_dates[]`（日中の欠け）・`insufficient_history_dates[]`（それ以前のスナップショットも無い日。欠けではない。#814）、確定した日の記録 `days[]`（古い順。最大 366）と固定の `note`。`days[]` の要素: `date`、`gap_kind`（`none` / `whole_day` / `intraday` / `insufficient_history`）、`gap_codes[]`（`morning_missing` / `afternoon_missing` / `late_start` / `mid_session_gap` / `early_end`）、`gaps[]`（`code`・`from`・`to`〔UTC RFC3339〕・`minutes`）、`alert_codes[]`（その日アプリが見た健康警告の固定コード。アプリが止まっていた間の警告は残らない）、`watch_checks`（立会中にアプリが見た回数。0 や極端に少ない日はアプリが止まっていた手がかり）、`snapshot_rows`・`snapshot_minutes`・`first_snapshot_at`・`last_snapshot_at`・`longest_gap_minutes`、`market_data_jobs_succeeded` / `_failed`（記録時点の件数。ジョブが既に消えていた日は `null`）、`recorded_at`。観測中（当日）の行は返さない。記録の無い日（記録が始まる前、アプリが止まったまま取りこぼした日）はどれにも入らない。休業日は数えない。価格は返さない |

`health_today.intraday_gaps[]`（`partial_session`）:

| フィールド | 定義 |
|------------|------|
| `intraday_gaps[]` | 同じ期間の東証立会日のうち、スナップショットはあるが立会がそろっていない日（issue #1054）。要素は `date`、`codes[]`（`morning_missing` 前場が無い / `afternoon_missing` 後場が無い / `late_start` 立会の始まりから 15 分を超えて遅れて開始 / `mid_session_gap` 立会中に 15 分を超える途切れ / `early_end` 立会の終わりの 15 分以上前に終了）、`gaps[]`（`code`・`from`・`to`・`minutes`）。終日 0 件の日は含めず `missing_session_dates[]` に出る（種別は重ならない）。日次記録がある日は記録から、無い日（アプリが止まっていて記録できなかった前営業日など）は読んだ時点の `market_snapshots` から判定するので、再起動後や MCP を読んだときにも出る。休業日は数えない。該当が無ければ `[]` |

要対応コード `partial_session`: `intraday_gaps[]` が空でない（#1054。終日欠けの `missing_session` とは別のコードで、履歴不足は含まない）。外部通知は無く、Home の要対応と本ツールにだけ出る
