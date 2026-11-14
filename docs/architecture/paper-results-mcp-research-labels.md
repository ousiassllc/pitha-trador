# 設計: 研究用のホライズンラベル（30 分・60 分・引け）と research_horizon_summary

`docs/architecture/paper-results-mcp.md` §5 の分割章（行数上限のため別ファイル）。issue #1074。ツールの引数と出力は `docs/api/endpoints/paper-results-mcp-research-labels.md` §3.34、表は `docs/architecture/er/tables-research.md`。

## 1. 目的と位置づけ

既存の 10 分・10bps のラベル（`calibration_outcomes`、トリプルバリア）はコスト控除後に優位性が見えない（11/11 時点でコスト前 +1〜2bps）。保有時間を変えると結果がどう変わるかを、取引判断とは無関係に**全スナップショット**について残し、ラベルが仮定するコストを実際のスプレッドと並べて読めるようにする。

**取引に影響しない**ことが条件で、次で担保する:

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
- `Recorder.Run`: 今日（JST）より**前**の、`research_label_days` に印のない日を古い順に 1 回の実行で最大 10 日、記録する。日の中は銘柄ごと（1 銘柄 1 日分 = 1 トランザクションの `INSERT OR IGNORE`）に書き、全銘柄が済んでから日次の集計（`research_label_day_stats`）と印（`research_label_days`）を 1 トランザクションで書く。
  - 再開可能: 途中で止まった日は印がないので、次の実行がその日をやり直す。行は `UNIQUE (snapshot_id, horizon)` と `INSERT OR IGNORE` で二重にならず、集計は印のときに行から作り直す（置き換え）ので行と必ず一致する。
  - 今日の日は決してラベルしない（足がそろっていない日を半分だけ付けない）。
  - `market_snapshots` が削除済みの日は対象に挙がらない（エラーにしない）。印のある日の行と集計は削除後も残る。
  - 書き込みは短い: 1 トランザクション = 1 銘柄 1 日分（数百行）。ライブの取引と同じ DB を WAL で共有するので、長い書き込みトランザクションは持たない。 銘柄ごとの書き込みのあいだには 200ms の間（`researchlabel.DefaultBatchPause`。`retention` の `defaultBatchPause` と同じ理由: WAL には待ち行列の公平さがなく、`busy_timeout` で待つライブの書き込みが入る隙を作る。#1077 の統合レビューの指摘）を置く。保守タスクは 1 本の mutex の下で順に動くので、初回の長い記録は後ろのタスクを遅らせ得る。
- 日次タスク `research_labels`（`internal/bootstrap/researchlabeljob`）: メンテナンスタスクとして `internal/bootstrap/services_build.go` の `scheduler.WithMaintenanceTask` に 1 行で登録する（`metalabel_retrain` と同じ作り）。**16:00 JST より前は `maintenance.ErrSkipped`** を返すので、「今日」の規則が単純になる（大引け後・足が確定したあとだけ記録する）。設定（コスト）は `system.RuntimeSettingsRepository` から実行ごとに読む。失敗はランナーが他のタスクと隔離して再試行し（既定 30 分後、3 回連続で通知）、記録部品の panic はエラーに変えて取引と同じプロセスを落とさない。
- 初回は DB に残る全期間（最大 90 日）を、1 回 10 日ずつ数日かけて追いつく。

## 4. 読み取りツール（research_horizon_summary）

- 実装: `internal/service/paperresultsmcp/researchdiag`（SQL と集計）、登録は `internal/service/paperresultsmcp/jev_models_tool.go`（`queryTool`。`init()` で `tools` に追加）。テストは `internal/service/paperresultsmcp/researchtests`。
- 集計: `snapshots` scope は `research_label_day_stats`（日ごとの和）から平均・勝率・コスト・スプレッドを出し、日ごとの和をそのままブートストラップの 1 まとまり（JST 日）にする（`internal/service/metaablation/stats` の `Cluster` / `Bootstrap`）。行を全部読まないので、数百万行でも 3 秒の読み取りに収まる（合成データ 300 万行・40 日で約 1 秒）。`trader` / `scout` は判断（`jev_decisions`）と行を `(instrument_id, snapshot_at)` で結んだ行を読んで集計する（判断は足より少ない）。
- 中央値は和から出せないので行を読む: ホライズンの行が 10 万を超えると、`sample_bucket`（`snapshot_id` の混ぜ関数）が k 未満の k% だけを、日・ホライズンごとに索引の範囲で読む。標本のとき `median_sample_percent` が 100 未満になる。平均・勝率・件数・区間は常に全行。
- 区間と「検出できる最小差」は記述であり、採用のしきい値ではない。`notice` を毎回返し、しきい値の判定・推奨は返さない。
- 期間の上限は標準の 366 日で足りる（日ごとの読みは最大 366 日 × 3 ホライズン）。

## 5. 保持と量

- `market_snapshots` は 90 日で削除される（`internal/service/retention`）。この 3 表は削除しない。理由: 足が消えたあとはラベルを作り直せず、削除は戻せない。保持の方針は**運用者の判断待ち**（未決）。
- 量の見積もり [INFERENCE]: 行数/日 ≈ スナップショット数/日 × 約 2.5（`30m`・`60m` は最終 30/60 分のエントリー、`close` は 15:25 以降のエントリーに行が無いので、足 1 本あたり最大 3 行に届かない）。1 行は索引込みで約 300 バイト。監視銘柄 120 × 立会 330 本 ≈ 4 万足/日なら、約 10 万行/日・約 30MB/日・90 日で約 2.7GB。`research_label_day_stats` は 1 日 6 行ほどで無視できる。
- 削除する場合の道筋: `day` 先頭の索引 `(day, horizon, sample_bucket)` で `DELETE ... WHERE day < ?` を小さなバッチで行えば、`retention` の `market_snapshots` と同じ形（索引の範囲走査・バッチ・間隔）で足せる。日次の集計表は残すので、期間の平均は消えない（中央値と判断別の集計は行が残る期間だけ）。

## 変更履歴

| 版 | 日付 | 内容 | 出典 |
|---|---|---|---|
| 1.0 | 2026-11-14 | 新規 | issue #1074 |
