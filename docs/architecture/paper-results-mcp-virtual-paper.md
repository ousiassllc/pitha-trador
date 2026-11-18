# 設計: 仮想 Paper（プロファイルの並走と比較。記録専用）

`docs/architecture/paper-results-mcp.md` の分割章（行数上限のため別ファイル）。issue #1087。API は `docs/api/endpoints/research-tools/paper-results-mcp-virtual-paper.md`、表は `docs/architecture/er/tables-virtual-paper.md`。

## 1. 目的と位置づけ

方針・出口の設定を 1 つ変えたら成績がどう変わるかを、本物の Paper を動かしたまま、同じ判断・同じ相場で並べて見たい。「仮想 Paper」のプロファイルは、本物の Paper との**差分**（`variant.Params`: エントリー規則・確率のしきい値・TP/SL・保有時間・コスト・サイズ）だけを持ち、本物の Paper と同じ `jev_decisions` と `market_snapshots` に対して「その設定なら入っていたか・どう出たか」を専用の表に記録する。

**取引に影響しない**ことが条件で、次で担保する:

- **記録の経路を持たない**: 相場の処理（market-data のジョブ）・Policy Engine・Risk・発注・約定には何もつながない。ライブの経路のコードは 1 行も変えない。記録は、終わった日の保存済みデータを日次の保守タスクが再生して書く（§3）。仮想 Paper が遅くても失敗しても、本物の Paper と相場の処理は止まらない。
- **既定は 0 件**: プロファイルを作るまで何も動かない。有効なプロファイルが 0 件のとき `Run` は DB に 1 行も書かず `maintenance.ErrSkipped` を返す（保守ランナーの最終成功日も書かない）。既定のインストールに書き込みは増えない。
- **書く表は新しい 3 表だけ**: `virtual_paper_profiles` / `virtual_paper_days` / `virtual_paper_trades`。`paper_orders`・`positions`・資金・`trade_signals`・`runtime_settings`・リスクの表・`market_snapshots`・`jev_decisions` には何も書かない（既存の表に列・制約・トリガも足さない）。
- **参照を固定する**: `internal/repository/virtualpaper/isolation_test.go` が、表名・リポジトリ・サービス・ジョブのシンボルを、記録部品・日次タスク・MCP の 3 ツールだけが名前にできると固定する。さらに仮想 Paper のパッケージが `repository/trading`・`repository/system`・`service/execution`・`service/policy`・`service/risk`・`service/jev`・`service/selfimprove`・`service/paper` などを import しないこと、書き込みの SQL が自分の 3 表だけを名指しすること、`services_build.go` の記述が import と `WithMaintenanceTask` の 1 行だけであることも固定する。
- **設定は変えない**: `runtime_settings` は読むだけ（基準になる設定を `backtestsource` が読む）。書き込みホワイトリスト（`settings_set`）・書き込みスイッチの意味・Paper のガード・リスクと資金の既定は変えない。

## 2. プロファイル

- `variant.Params`（`internal/service/backtest/variant`。バックテストラボと共有）の差分を正規形の JSON（`Params.Canonical()`）で `params_json` に持つ。空 `{}` は本物の Paper と同じ（基準）。範囲外は `Params.Validate()` が `*variant.InvalidError`（項目名つき）で拒否する。
- 名前は 1〜40 文字。停止したものも含めて重複不可。先頭・末尾の空白と制御文字は不可。
- 上限: **同時に有効なものは 5 件、合計 20 件（停止を含む）**。超える作成は `limit_exceeded`。上限は日次の再生の仕事量（有効 1 件 × 1 日 = 再生 1 回）を抑える。
- 複製: `clone_from` に元の `profile_id` を渡すと、その `params` を写し、引数の `params` の項目で上書きする（`virtual_paper_profile_create` が作成と複製を兼ねる）。上書き後の全体を検証する。
- 停止: 有効→停止だけ（再開はない。同じ設定は複製で作り直す）。停止したプロファイルは以後の日を記録しない。記録済みの取引・日は残り、比較にも使える。停止済みの停止は失敗にせず `already_stopped` で知らせる。
- 開始日（`first_day`）: 作成した JST の日の**翌日**。作成した日は途中なので入れない。これで「プロファイルが見た日」は常に丸 1 日になる。

## 3. 記録（internal/service/virtualpaper, internal/bootstrap/virtualpaperjob）

日次の保守タスク `virtual_paper`（`internal/bootstrap/virtualpaperjob`）が、`researchlabeljob` と同じ作りで記録する。`internal/bootstrap/services_build.go` の `scheduler.WithMaintenanceTask` に 1 行だけ足して登録する。

- **16:00 JST から動く**（`RunFrom`）。その日の足と判断がそろったあとで、それより前は `ErrSkipped`。結果は**取引日の終わった後（最大で翌日）に出る**。これは意図した設計で、ライブの経路に手を入れないための代償である。
- **基準になる設定は、その日を処理した時点で本体が使っている設定**: `backtestsource.Source`（`NewForDB`。Paper の再生モード `ReplayPaper`＝Paper の最低エントリー品質、`policy.*` のしきい値、`cost.*` の約定コスト、Paper の資金とリスク上限、有効なメタモデル）。ある日を処理したあとに設定を変えても、処理済みの日は書き換えない。日を処理するまでに設定を変えると、その日はその新しい設定が基準になる。プロファイルの差分はその上に重なる。
- 1 回の `Run` = 1 チャンク = **JST の 1 日**（有効な全プロファイルのうち、その日がまだのもの）。日の `RunConfig` を 1 回だけ読み込み、各プロファイルの `Params.Apply`（コピーを返す。入力は書き換えない）を重ねて `backtest.ShadowBacktestPortfolioTrades`（本物のバックテストの再生。約定は `fillmodel`、リスクエンジンの再現つき）を回す。全プロファイルが**同じ判断・同じ足**を見る。
- 仕事が残れば `maintenance.ErrMore`。ランナーはチャンクの間にロックを放すので、日数が溜まってもバックアップ・保持期間の削除を待たせない。1 回の `Run` には壁時間の上限（`DefaultChunkBudget` = 3 分。コンテキストの期限）があり、超えるとエラーで返す（ランナーが後で再試行）。
- **溜まった日の上限**: 各プロファイルが記録する日は、`max(first_day, 今日 − 10 日)` から昨日まで（`BacklogDays` = 10）。長く止まっていても最近 10 日分だけ取り戻す。「同じ期間」は、プロファイルごとの `first_day` 以降の、記録を終えた日の共通部分で決まる（§4）。
- **冪等・再開可能**: (プロファイル, 日) の取引と印（`virtual_paper_days`）を 1 トランザクションで書く。印の無い日（途中で止まった・未着手）は次の実行がやり直し、取引は `UNIQUE (profile_id, instrument_id, entry_at)` と `ON CONFLICT DO NOTHING` で二重にならない。
- **データ終了で閉じた取引**: 再生がデータの終わりまで条件に当たらず強制的に閉じた取引（`exit_reason = data_ended`）は、その理由で記録するが、**比較の指標からは外す**（`unfinished` として数える）。再生は 1 日ごとなので、引け前に建てて手仕舞い条件に当たらなかった取引が該当する。
- 失敗の扱い: 再生が失敗したプロファイルの日は `failed`（理由は 200 文字まで）の印を付け、同じ日を自動ではやり直さない（毎回同じ失敗で詰まらないため）。エラーは保守ランナーに返り、他のタスクとは独立に扱われる。設定の読み込み・書き込みの失敗は印を付けず返し、次の実行が再試行する。パニックはエラーに変えて返し、取引のプロセスを落とさない。
- 書き込みは短い: 1 トランザクション = 1 プロファイル 1 日分（数十行）。

## 4. 比較（virtual_paper_compare）

- 比べる日 = **全員が記録を終えた日（`done`）の共通部分**。誰かが欠く日は `excluded_days`（欠く `profile_ids` つき）に出し、どのプロファイルの数字にも入れない。これで「同じ期間・同じ判断の範囲」が定義される。`days` に比べた日を明示する。
- 指標（プロファイルごと）: `trade_count`・`win_rate`・`expectancy_pct`（1 取引あたりの純損益＝コスト控除後の平均、%）とその 95% 信頼区間（日ごとのまとまりで計算する `actuals.ClusterMean`。日数が 2 日未満だと区間なし）・`total_pnl_jpy`（記録した `pnl_jpy` の和）・`max_drawdown_pct`（取引を決済順に並べた累積純損益の山からの最大の落ち込み。`backtest.Aggregate` と同じ定義）・`unfinished`。
- 先頭のプロファイルとの差（`vs_first`）: 日ごとの純損益（%の合計）の差の平均とその日単位の 95% 信頼区間（取引の無い日は 0）。
- 固定の注意書き `caution`: 記録専用・標本が小さい間は偶然・多重比較・何も反映しない。結論（どれを採用する）は返さない。
- 読み取り専用（`mode=ro` の接続）。1 プロファイルの期間内の取引が 20,000 件を超えると `limit_exceeded`（期間を狭める）。

## 5. MCP の書き込み（mcpset）

`virtual_paper_profile_create` / `virtual_paper_profile_stop` は、`mcpset/researchwrite/virtualpaper.go` の 2 つのトランザクション（INSERT 1 回・UPDATE 1 回）で `virtual_paper_profiles` だけを書く。書き込みスイッチがオフのときは `write_disabled`（トランザクションの先頭で確かめる）。設定・監査ログ・注文・ポジションには触らない。取引・日の行を MCP が書くことはない。

## 6. 量と保持

1 プロファイル 1 日あたり数十〜数百行。有効 5 件・合計 20 件の上限と 90 日の相場の保持から、表は数万行に収まる。仮想 Paper の表には保持期間の削除を付けていない（プロファイルを増やせないので量が増えない）。
