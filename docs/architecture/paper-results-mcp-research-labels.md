# 設計: 研究用のホライズンラベル（30 分・60 分・引け）と research_horizon_summary

`docs/architecture/paper-results-mcp.md` §5 の分割章（行数上限のため別ファイル）。issue #1074（記録のオン・オフ・小分けの取り込み・保持期間は issue #1073 / #1074 の #1077 での直し）。ツールの引数と出力は `docs/api/endpoints/paper-results-mcp-research-labels.md` §3.34、表は `docs/architecture/er/tables-research.md`。

## 1. 目的と位置づけ

既存の 10 分・10bps のラベル（`calibration_outcomes`、トリプルバリア）はコスト控除後に優位性が見えない（11/11 時点でコスト前 +1〜2bps）。保有時間を変えると結果がどう変わるかを、取引判断とは無関係に**全スナップショット**について残し、ラベルが仮定するコストを実際のスプレッドと並べて読めるようにする。

**取引に影響しない**ことが条件で、次で担保する:

- **既定はオフ**: 記録は Settings の `research.labels.enabled`（`runtime_settings` の JSON 整数 1 / 0、既定 0。Settings「研究用ホライズンラベルの記録」。Paper 実績 MCP の `settings_set` からも変えられる）がオンのときだけ動く。オフ（未設定・0・壊れた値）の間は日次タスクが `maintenance.ErrSkipped` を返し、表にも `runtime_settings`（ランナーの最終成功日 `system.maintenance.research_labels.last_success_date`）にも何も書かない。つまり既定の取引プロセスには、このタスクによる新しい書き込みが増えない（`researchlabeljob` の `TestJob_IsOffByDefaultAndWritesNothingWhileOff` が、未設定・0・壊れた値のそれぞれで各表の行数が変わらないことと、最終成功日が書かれないことを固定する）。
- 書くのは新しい 3 表（`research_horizon_labels` / `research_label_days` / `research_label_day_stats`）だけ。`market_snapshots`・`jev_decisions`・`calibration_outcomes`・`counterfactual_labels`・メタラベルの表は読み取りだけで、列も制約もトリガも足さない。ラベルの定義・学習データ・メタモデル・既存のシャドー（`jev_shadow*`）・Policy・Risk・発注・Paper ガード・Live・Jev は変えない。
- 参照を固定する: `internal/repository/researchlabel/isolation_test.go` が、表名・リポジトリ・記録部品・MCP の問い合わせのシンボルを、記録部品・日次タスク・読み取り専用の MCP ツールのファイルと `db/migrations` だけに限る（Policy・Risk・発注・実行・Paper ガード・Live・Jev プロンプト・RAG・学習・自己改善が参照すると失敗する）。記録部品が書く表が自分の 3 表だけであることも同じテストが固定する。
- `counterfactual_labels` / `calibration_outcome_origins` / トライアルの表は読まない（`counterlabel` の隔離テストの許可リストを増やさない）。
- MCP のツールは読み取り専用（`mode=ro`）で、書き込みホワイトリストは変えない。

## 2. 定義

| 項目 | 規則 |
|---|---|
| エントリー | そのスナップショットの足。買いは ask、売りは bid で入る。ask が無い（0 以下を含む）と買いの側は NULL、bid が無いと売りの側は NULL。両方無い足には行を作らない |
| 出口 `30m` / `60m` | 足の時刻 + 30/60 分以前の最後の足。目標時刻から `calibration.HorizonBarTolerance`（2 分）以内でなければ行を作らない。昼休み・引け・欠測で窓が足りなければ行を作らない（短い窓は記録しない。`Labeler.Compute` と同じ規則） |
| 出口 `close` | 同じ JST 日の最後の足（15:30 以前）。15:25 JST 以降のときだけ。15:25 以降のエントリーには行を作らない。非立会日には行を作らない |
| gross | `(出口 − 入口) / 入口 × 10000 × 方向`（売りは −1）。`calibration.grossReturnBps` と同じ式 |
| cost | 記録時の `costsettings.LabelBarriers`（`calibration.barrier_cost_bps` を `cost.*` で動かした値）の買い・売り別。ライブのラベラーと同じ関数なので、研究ラベルは本番のラベルと別のコストを置かない。ラベル行には設定の写しが無いので、行ごとの値が記録時の値（設定変更後は日ごとに違いうる） |
| net | `gross − cost`（行ごと） |
| 日 | JST の日付。保存する時刻は UTC・固定 9 桁 |

## 3. 記録（internal/service/researchlabel）

- `LabelBars`（純粋関数）: 1 銘柄・1 JST 日の足から行を作る。テストは手計算の gross / net、片側の気配、30/60 分の許容、昼休みをまたぐ窓、引け近くの 60 分、`close` の規則（15:25 以降の最終足を採る・15:10 止まりは行なし・15:25 以降のエントリーは行なし）、コストが `costsettings` の関数と一致することを固定する。
- `Recorder.Run`（1 回 = 1 チャンク。`DefaultMaxDaysPerRun` = 1）: 先に保持期間より古い行の削除（§5。最大 10 バッチ）、そのあと今日（JST）より**前**の、`research_label_days` に印のない日を古い順に **1 日**、記録する。まだ残りがあれば `Result.More` を立てる。日の中は銘柄ごと（1 銘柄 1 日分 = 1 トランザクションの `INSERT OR IGNORE`）に書き、全銘柄が済んでから日次の集計（`research_label_day_stats`）と印（`research_label_days`）を 1 トランザクションで書く。
  - 再開可能: 途中で止まった日は印がないので、次の実行がその日をやり直す。行は `UNIQUE (snapshot_id, horizon)` と `INSERT OR IGNORE` で二重にならず、集計は印のときに行から作り直す（置き換え）ので行と必ず一致する。
  - 今日の日は決してラベルしない（足がそろっていない日を半分だけ付けない）。
  - `market_snapshots` が削除済みの日は対象に挙がらない（エラーにしない）。印のある日の行と集計は削除後も残る。
  - 書き込みは短い: 1 トランザクション = 1 銘柄 1 日分（数百行）。ライブの取引と同じ DB を WAL で共有するので、長い書き込みトランザクションは持たない。 銘柄ごとの書き込みのあいだには 200ms の間（`researchlabel.DefaultBatchPause`。`retention` の `defaultBatchPause` と同じ理由: WAL には待ち行列の公平さがなく、`busy_timeout` で待つライブの書き込みが入る隙を作る。#1077 の統合レビューの指摘）を置く。
  - **保守の mutex を長く持たない**: `maintenance.Runner` は 1 回の `CatchUp` / `RunNow` のあいだ `mu` を持ち、バックアップ（16:00 の `RunNow` を含む）と保持パージはこの `mu` を待つ。初回の 90 日分を 1 回の呼び出しで取り込むと、その全体が待ち時間になる。そこで `Recorder.Run` は 1 日分（または削除の 1 チャンク）だけして `More` を返し、`researchlabeljob` は `maintenance.ErrMore` を返す。ランナーは `ErrMore` を「進んだが今日の成功ではない」として扱い（最終成功日は書かない・失敗にも数えない）、`mu` を放して `DefaultChunkPause`（200ms）待ってから同じタスクを呼び直す。この隙間にバックアップ・保持パージ・固定時刻のトリガーが `mu` を取れる。待ち時間の上限は 1 チャンクの長さ（1 日分 ≒ 銘柄数 × 200ms の間 + 書き込み）になる。テスト: `maintenance_test.go` の `TestCatchUp_ErrMoreReleasesTheLockBetweenChunks`（隙間で `RunNow` と `CatchUp` を呼ぶ。`mu` を放していなければ期限内に終わらず失敗する）、`researchlabeljob` の `TestRunner_BackfillIsOneDayPerChunkAndBackupAndPurgeRunBetweenChunks`（実物の `backupjob` と、`market_snapshots` の削除前退避（`snapshotarchive`、issue #1072）つきの `retention.Purge` を、6 日分の取り込みの各チャンクの隙間で動かし、退避先に日別ファイルが書かれて 2026-10-13 の足が削除されてもそのラベルが残ること、各隙間でバックアップ・パージが `mu` を 2 秒以内に取れて実際にバックアップが書かれることと、1 チャンクごとに 1 日分（179 行）ずつ増えることを固定する）。
- 日次タスク `research_labels`（`internal/bootstrap/researchlabeljob`）: メンテナンスタスクとして `internal/bootstrap/services_build.go` の `scheduler.WithMaintenanceTask` に 1 行で登録する（`metalabel_retrain` と同じ作り）。**16:00 JST より前は `maintenance.ErrSkipped`** を返すので、「今日」の規則が単純になる（大引け後・足が確定したあとだけ記録する）。設定（オン・オフ・保持期間・コスト）は `system.RuntimeSettingsRepository` から実行ごとに読むので、保存すると再起動なしで次のチェック（10 分以内）から効く。失敗はランナーが他のタスクと隔離して再試行し（既定 30 分後、3 回連続で通知）、記録部品の panic はエラーに変えて取引と同じプロセスを落とさない。
- 初回（スイッチをオンにした直後）は DB に残る全期間（最大 90 日。保持期間が短ければその日数）を、1 日ずつのチャンクで同じ `CatchUp` の中で追いつく。途中で再起動しても、印のある日は飛ばして続きから進む。

## 4. 読み取りツール（research_horizon_summary）

- 実装: `internal/service/paperresultsmcp/researchdiag`（SQL と集計）、登録は `internal/service/paperresultsmcp/jev_models_tool.go`（`queryTool`。`init()` で `tools` に追加）。テストは `internal/service/paperresultsmcp/researchtests`。
- 集計: `snapshots` scope は `research_label_day_stats`（日ごとの和）から平均・勝率・コスト・スプレッドを出し、日ごとの和をそのままブートストラップの 1 まとまり（JST 日）にする（`internal/service/metaablation/stats` の `Cluster` / `Bootstrap`）。行を全部読まないので、数百万行でも 3 秒の読み取りに収まる（合成データ 300 万行・40 日で約 1 秒）。`trader` / `scout` は判断（`jev_decisions`）と行を `(instrument_id, snapshot_at)` で結んだ行を読んで集計する（判断は足より少ない）。
- 中央値は和から出せないので行を読む: ホライズンの行が 10 万を超えると、`sample_bucket`（`snapshot_id` の混ぜ関数）が k 未満の k% だけを、日・ホライズンごとに索引の範囲で読む。標本のとき `median_sample_percent` が 100 未満になる。平均・勝率・件数・区間は常に全行。
- 区間と「検出できる最小差」は記述であり、採用のしきい値ではない。`notice` を毎回返し、しきい値の判定・推奨は返さない。
- 任意の引数 `fx_symbol`（issue #1134。`jev_evaluation` の同名の引数、#1084 と同じ区分）: 指定すると、集計に入った行を、対象時刻（スナップショットの時刻。`trader` / `scout` では判断のあった足）の直前 60 分の mid 価格の変化の符号（`up` / `flat` / `down` / `unmatched`）で分け、ホライズン・側ごとに件数と平均 net bps を `fx_context` として返す。区分と足の結合は `evaldiag.FxTrailingBuckets`（`fxdiag.AtOrBefore` による at-or-before の結合）を共有し、複製しない。`research_fx_candles` を SELECT で読むだけで、記述のみ（しきい値なし・取引には使わない）。**指定しなければ `fx_context` というキーはなく、出力は引数が無かった頃とバイト単位で同じ**（`researchtests` の `TestWithoutFXSymbolTheOutputIsByteIdentical` が、引数の追加前の `main` で取った scope ごとの応答（`testdata/`）と一致すること、指定時は `fx_context` 以外の値が同じで何も書かれないことを固定する）。記録のスイッチ（`research.fx.enabled`）には触れず、記録が無い・止まっているときは該当の行が `unmatched` に入る。実装は `researchdiag/fx.go`（行ごとの対象時刻と net を `snapshot_at` 別に足し込み、集計の最後に 1 回だけ結合する）。中央値の標本（ホライズンの行が 10 万超の `snapshots`）では、区分も同じ標本の行を数える（`sample_percent`）。
- 期間の上限は標準の 366 日で足りる（日ごとの読みは最大 366 日 × 3 ホライズン）。

## 5. 保持と量

- 保持期間は Settings の `research.labels.retention_days`（JSON 整数。1〜3650、**既定 90 日**。Settings「研究用ホライズンラベルの記録」。MCP の `settings_set` ホワイトリストには入れない）。日次タスクが実行のたびに、JST で `今日 − 保持日数` より前の日の行を `research_horizon_labels` から削除する（`DELETE ... WHERE id IN (SELECT id ... WHERE day < ? ORDER BY day LIMIT 1000)`。索引 `(day, horizon, sample_bucket)` の範囲走査、1000 行ごとに 1 トランザクション、バッチのあいだに 200ms の間（`retention` と同じ形）、1 回の実行で最大 10 バッチ。残りは `More` で次のチャンクへ）。
- **消すのは研究用の表だけ**: 削除は `research_horizon_labels` の行だけ。`market_snapshots`・`jev_decisions`・`calibration_outcomes` など取引系の表、および `research_label_days`（印）・`research_label_day_stats`（日ごとの和、1 日 6 行ほど）は消さない。印を残すので削除済みの日が再びラベルされず（保持期間が `market_snapshots` の 90 日より短くても、削除した日を作り直しては消す、を繰り返さない。保持期間より古い日は、未記録でも記録の対象にしない）、日ごとの和を残すので期間の平均・勝率・区間は消えない（中央値と `trader` / `scout` の判断別の集計は行が残る期間だけ）。`TestResearchPackagesWriteOnlyTheirOwnTables` が、記録部品の `DELETE` が研究用の表だけを指すことを固定する。
- スイッチがオフの間は削除もしない（オフのとき日次タスクは何も書かない）。保持期間を過ぎた行は、オンに戻したときの最初の実行で削除される。
- テスト: `researchlabel` の `TestRecorder_RetentionDeletesOldRowsOfTheLabelTableOnly`（古い日の行だけが消え、印・日ごとの和・`market_snapshots` の行数は変わらず、保持期間外の日は記録されない）、`TestRecorder_PurgeIsChunkedAndRecordingWaitsForIt`（1 バッチ 1000 行・チャンクに分かれる）、`researchlabeljob` の `TestJob_RetentionFromSettingsDeletesOldRowsOnlyFromTheResearchTable`（既定 90 日の境目と、Settings で 7 日にしたとき）。
- 量の見積もり [INFERENCE]: 行数/日 ≈ スナップショット数/日 × 約 2.5（`30m`・`60m` は最終 30/60 分のエントリー、`close` は 15:25 以降のエントリーに行が無いので、足 1 本あたり最大 3 行に届かない）。1 行は索引込みで約 300 バイト。監視銘柄 120 × 立会 330 本 ≈ 4 万足/日なら、約 10 万行/日・約 30MB/日・90 日で約 2.7GB。保持期間を 90 日にすると、この大きさで頭打ちになる（以前は無制限に増えた）。

## 変更履歴

| 版 | 日付 | 内容 | 出典 |
|---|---|---|---|
| 1.0 | 2026-11-14 | 新規 | issue #1074 |
| 1.1 | 2026-11-15 | 記録を Settings の `research.labels.enabled`（既定オフ。MCP `settings_set` でも変更可）の下に置き、オフの間は何も書かないことを追記。取り込みを 1 日ずつのチャンクにし、`maintenance.ErrMore` でランナーが `mu` を放す間（バックアップ・保持パージを待たせない）を追記。§5 を、保持期間 `research.labels.retention_days`（既定 90 日・研究用の表だけを削除）の実装に書き換え | issue #1073 / #1074（PR #1077 の直し） |
| 1.2 | 2026-11-20 | §4 に任意の引数 `fx_symbol`（`fx_context`。#1084 と同じ区分を `evaldiag.FxTrailingBuckets` で共有。省略時は出力不変）を追記 | issue #1134 |
