# 設計: MCP から起動する研究用バックテスト（backtest_start / backtest_get / backtest_list）

`docs/architecture/paper-results-mcp.md` の分割章（行数上限のため別ファイル）。issue #1086。ツールの引数と出力は `docs/api/endpoints/paper-results-mcp-backtest.md`、表は `docs/architecture/er/tables-backtest.md`。

## 1. 目的と位置づけ

Grok Bot（あおい）が、期間と売買の条件（エントリーの規則・確率のしきい値・利確/損切り・保有時間・コストの仮定など）を変えたバックテストを MCP から起動し、結果（件数・勝率・コスト込みの期待値・最大ドローダウン・日別・信頼区間）を、使った条件・コードの版・実際にデータのあった期間と一緒に研究用の表へ残して読めるようにする。**調べるための道具**であり、設定・Policy・Risk・発注・資金配分の入力にはしない。

`internal/service/backtest/variant`（`Params`）が「何を変えてよいか」を決め、`internal/service/backtest` のリプレイエンジンが再生する。`variant.Params` は仮想 Paper（issue #1087）と共有する。

## 2. 取引に影響しないことの担保

- **読むだけ**: 再生は保存済みの `market_snapshots` / `jev_decisions` / メタモデルを読み、`runtime_settings` を含む設定を**書かない**。組の値は `RunConfig` の**コピー**に重ねるだけ（`Params.Apply`）で、元の設定・ライブの Policy・Risk・Execution には届かない。テスト（`backtesttests`）が、実行の前後で `runtime_settings` / `mcp_settings_audit` / `positions` の中身が同じこと、書き込みの文が `backtest_runs` のものだけ（実行 1 回あたり記録の INSERT 1 + UPDATE 2、プロセスごとに掃除の UPDATE 1）であることを固定する。
- **書く表は 1 つ**: `backtest_runs`（`docs/architecture/er/tables-backtest.md`）。研究専用で、Policy・Risk・発注・Paper ガード・Live・Jev のプロンプト・RAG・メタモデルの学習・自己改善は読まない。`internal/repository/backtestrun/isolation_test.go` が、表名・リポジトリ・ラボ・ジョブのシンボルを、研究ツールのファイル（`paperresultsmcp` の `backtesttool` と `backtestview`、`backtestjob`、`mcpset/researchwrite/backtest.go`、`server.go` / `tools.go`、`cmd/paper-results-mcp`）に限る。ラボとジョブのコードが `runtime_settings` を名指さないことと、書く文が `backtest_runs` だけであることも同じファイルで固定する。
- **既存の仕組みを変えない**: `settings_set` のホワイトリスト・MCP の書き込みスイッチの意味・Paper のガード・リスクや資金の既定値は変えない。`backtest_start` は書き込みスイッチ（`mcp.settings_write_enabled`）の下にあり、オフなら `write_disabled`。
- **取引処理を遅くしない**（§4）: 同時 1 件・1 回 10 分・組 12 まで・期間 31 日まで。再生は MCP プロセス（アプリとは別プロセス）の中で、アプリの DB には読み取り専用の**専用接続**（ツールの読み取り接続とは別）でつなぐ。

## 3. 構成

| 部品 | 場所 | 役割 |
|---|---|---|
| ラボ | `internal/service/backtestlab` | `ConfigSource`（入力の IF）、要求の検査と既定値（`Normalize`）、1 回の実行（`Run`）、組ごとの結果の集計（コスト込みの期待値・最大 DD・日別・クラスター区間・感度）、参考印。DB も設定も書かない |
| 記録 | `internal/repository/backtestrun` | `backtest_runs` の行（INSERT 1・UPDATE 3 種）と読み（`Get` / `List`）。マイグレーション `000057` |
| ジョブ | `internal/service/paperresultsmcp/backtestjob` | 同時 1 件・時間の上限・状態の遷移・再起動で残った行の掃除。入力の元（`SourceFactory`）と書き込み（`Store`）は注入 |
| 書き込み | `internal/service/paperresultsmcp/mcpset/researchwrite/backtest.go` | `Writer` の 4 つのトランザクション（作成・開始・終了・掃除）。作成だけ書き込みスイッチを見る |
| ツール | `internal/service/paperresultsmcp/backtesttool`（出力の形は `backtestview`） | `backtest_start`（`writes`）/ `backtest_get` / `backtest_list` |
| 配線 | `cmd/paper-results-mcp/backtest.go` | 初回の起動で `bootstrap.Run(ReadOnly)` と `backtestsource.NewForDB` を作る（`Options.BacktestSource`）。サービスは `internal/bootstrap` を import しない |

`ConfigSource` は `RunConfigs(ctx, backtest.Period)` だけで、`backtestsource.Source` がそのまま満たす。テストは `backtestlab/labtest` の偽の入力（合成の足と判断）で**本物のリプレイエンジン**を動かす。

## 4. 実行と上限

- 流れ: `backtest_start` → 検査（引数・組・期間）→ 同時実行の枠を取る → 行を `queued` で INSERT（書き込みスイッチを同じトランザクションで確かめる）→ すぐ応答 → バックグラウンドで `running` に更新 → 入力を 1 回読む → 組ごとに `Params.Apply` → `backtest.ShadowBacktestPortfolioTrades` → 集計 → `succeeded`（結果の JSON を保存）か `failed`（固定コード）に更新。
- 定数（名前つき）: `backtestlab.MaxVariants` = 12、`MaxExtraCostPoints` = 6、`MaxExtraCostBps` = 200、`MaxLabelRunes` = 40、`MaxPeriodDays` = 31、`backtestjob.MaxRunning` = 1、`DefaultRunTimeout` = 10 分。
- **同時 1 件**: 枠が埋まっていれば `ErrBusy` → `concurrency_limited`。**枠を取ってから書く**ので、断られた呼び出しは何も書かない（`run_id` も消費しない）。枠は最終状態を書き終えてから空く。
- **時間の上限**: 実行ごとの `context` に 10 分の期限。リプレイエンジンは足ごとに判断の入力（`DecisionSource.Decision(ctx, …)`）を呼ぶので、ラボがそれを包み、`ctx` が終わっていればエラーを返して**組の途中でも止める**。期限なら `timed_out`、MCP の停止なら `interrupted`、入力を開けなければ `unavailable`、それ以外（エンジンの `panic` を含む）は `internal`。`failed` の `error_code` は固定コードだけで、生のエラー文・パスは記録も応答もログもしない。
- **最終状態は必ず書く**: 時間切れ・停止のあとでも、最終状態の書き込みは実行の `context` から切り離した 5 秒の期限で最大 3 回試す。書き込みスイッチは見ない（受け付けた実行が `running` のまま残ると次の実行を止めるため）。それでも書けなければ行は残り、次の起動の掃除で `interrupted` になる。
- **再起動で残った行**: 行には作ったプロセスの印（`owner`。起動ごとの乱数）がある。最初の `backtest_start` の前に、印の違う `queued` / `running` を `failed` / `interrupted` に直す（`mcpset` の書き込み接続）。読み取りツールは書かないので、印の違う未完了の行を**読んだ時点で** `failed` / `interrupted`（`finished_at` は `null`）として返す。MCP のプロセスは 1 つだけ動かす前提（2 つ動かすと互いの実行を中断扱いにする）。
- `Server.Close` は実行中のものを止め（`interrupted`）、終わるのを待ってから書き込み接続を閉じる。`cmd/paper-results-mcp` は `Serve` が返った後に専用の読み取り接続を閉じる。

## 5. 結果と決定性

- 組ごとの結果: 取引件数・勝率・`expectancy_pct`（`backtest.Aggregate` の Expectancy＝コスト込みの平均純リターン）・最大 DD・コスト前/スリッページ後/手数料後/純の合計・JST の決済日ごとの件数と純損益・`expectancy_ci`（`actuals.ClusterMean`: 決済日をクラスターにした 95% 区間。2 日未満は区間なし）・追加コストの感度（`sensitivity.Table`。`0` の行は結果そのもの）。
- **同じ入力なら同じバイト列**: 取引は全順序（決済時刻→純リターン（高い順）→エントリー時刻→銘柄→…）に並べ直してから足し上げ、日別・区間・感度を作る。結果に時計・乱数・マップの順序を入れない。組のあいだは `RunConfig` のコピーで、前の組が次の組へ漏れない（テストが同じ組を後ろにもう一度入れて一致を確かめる）。`code_version`（`internal/version.Version`）は行の列で、結果の JSON には入れない。
- **最良値は参考だけ**: 試した組の数を `explored_count` として毎回返し、取引のあった組のうち期待値が最大の 1 組だけに `is_reference_best` を付ける。固定の `caution`（多重比較）を毎回返す。「採用してよい」という判定・推奨は返さない。
- データの無い期間はエラーにせず、取引 0 件・`win_rate` / `expectancy_pct` は `null`・`period.first_snapshot_at` は `null` の結果にする（期間は依頼どおり、実際に読めた範囲は別に出す）。

## 6. 保持と量

古い行は**自動では消さない**。一覧は件数を絞って返す（既定 20・最大 50）。1 行の結果は、組 12 × （日別 31 件 + 感度 6 行）ほどの JSON で、数十 KB 以下 [INFERENCE: 日別 1 行 60 バイト前後として]。件数が増えて困るようになったら、保持日数の設定と削除を別の issue で足す。

## 変更履歴

| 版 | 日付 | 内容 | 出典 |
|---|---|---|---|
| 1.0 | 2026-11-15 | 新規 | issue #1086 |
