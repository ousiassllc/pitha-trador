# ER / データモデル: テーブル定義（研究用バックテストの記録）

`docs/architecture/er.md` から分割した章。対象: `backtest_runs`（マイグレーション `000065`。issue #1086）。型・規約と全体 ER 図は `docs/architecture/er.md` を参照。設計は `docs/architecture/paper-results-mcp-backtest.md`、ツールは `docs/api/endpoints/research-tools/paper-results-mcp-backtest.md`。実装は `internal/repository/backtestrun`（表）・`internal/service/backtestlab`（再生）・`internal/service/paperresultsmcp/backtestjob`（実行）・`mcpset/researchwrite/backtest.go`（書き込み）。

## 位置づけ

Paper 実績 MCP の `backtest_start` が起動した研究用バックテスト 1 回分の記録。保存済みの `market_snapshots` / `jev_decisions` を再生して（読むだけ）、売買の条件の組（`variant.Params`）ごとの成績を、使った条件・コードの版・実際にデータのあった期間と一緒に残す。**研究専用**で、取引には使わない: Policy・Risk・発注・Paper ガード・Live・Jev のプロンプト・RAG・メタモデルの学習・自己改善は読まない（`internal/repository/backtestrun/isolation_test.go` が、参照できるファイルを研究ツールに限って固定する）。

書くのは MCP の書き込み専用接続（`mcpset.Writer`）だけで、この表の INSERT 1 種・UPDATE 3 種（開始・終了・再起動時の掃除）だけを実行する。読むのは MCP の読み取りツール（`backtest_get` / `backtest_list`）だけ。`runtime_settings`・`market_snapshots`・`jev_decisions` など既存の表には列も制約もトリガも足さず、再生は設定値を書き換えない。他の表への外部キーは持たない。

## backtest_runs

1 行は 1 回の実行。

| カラム | 型 | 制約 | 説明 |
|-------|-----|------|------|
| id | integer | PK（AUTOINCREMENT） | `run_id`。新しい順の並びと一覧のカーソルに使う |
| status | text | NOT NULL, CHECK (`queued` / `running` / `succeeded` / `failed`) | 記録して再生を始める前が `queued`。`failed` は `queued` から直接なることもある（再起動で残った行） |
| owner | text | NOT NULL | この行を作った MCP プロセスの印（起動ごとの乱数）。再起動で残った未完了の行を、印の違いで見つけて `failed`（`interrupted`）にする |
| client | text | NOT NULL | 呼び出し元のホスト（監査用。ツールの応答には出さない） |
| created_at | text | NOT NULL | 記録した時刻（UTC・固定 9 桁） |
| started_at | text | NULL 可 | 再生を始めた時刻。`queued` のままの行と、始める前に失敗した行は NULL |
| finished_at | text | NULL 可 | 終わった時刻。`succeeded` / `failed` のときだけ値がある（CHECK） |
| period_from / period_to | text | NOT NULL, CHECK (長さ 10) | 依頼した期間（JST の日付 `YYYY-MM-DD`。`to` を含まない） |
| first_snapshot_at / last_snapshot_at | text | NULL 可 | 期間内に**実際にあった**スナップショットの最初と最後の時刻（UTC）。データが無い期間・終了前は NULL |
| replay_mode | text | NOT NULL, CHECK (`paper` / `live`) | `min_entry_quality` の基準（`backtestsource.ReplayMode`） |
| code_version | text | NOT NULL | 記録時の `internal/version.Version`（ブランチ・PR のビルドは `dev`） |
| explored_count | integer | NOT NULL, CHECK (1〜12) | 試した組の数。最良値は複数の組から選んだ値なので、この数と一緒に読む |
| request_json | text | NOT NULL | 依頼した条件: `{"variants":[{"label","params"}],"extra_cost_bps":[…]}`（補った後の値。`params` は書いた項目だけ） |
| result_json | text | NULL 可 | `succeeded` のときだけ（CHECK）。`{"variants":[…]}`: 組ごとの `trade_count` / `win_rate` / `expectancy_pct` / `max_drawdown_pct` / 各 PnL / `expectancy_ci` / `days` / `sensitivity` / `is_reference_best` |
| error_code | text | NULL 可, CHECK (`timed_out` / `interrupted` / `unavailable` / `internal`) | `failed` のときだけ（CHECK）。固定コードのみで、生のエラー文・パスは入れない |

整合性の CHECK: `finished_at` は `succeeded` / `failed` のときだけ、`result_json` は `succeeded` のときだけ、`error_code` は `failed` のときだけ値を持つ。`queued` / `failed` 以外は `started_at` を持つ。

索引: `backtest_runs_status_idx (status, id)`（状態で絞る一覧と、再起動時に未完了の行を探す読み）。

保持: 古い行は自動では消さない。一覧は件数を絞って返す（既定 20・最大 50）。

## 変更履歴

| 版 | 日付 | 内容 | 出典 |
|---|---|---|---|
| 1.0 | 2026-11-15 | 新規 | issue #1086 |
