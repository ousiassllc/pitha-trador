# ER / データモデル: テーブル定義（Jev 評価の日次記録）

`docs/architecture/er.md` から分割した章。対象: `jev_evaluation_daily`（マイグレーション `000057`。issue #1089）。型・規約と全体 ER 図は `docs/architecture/er.md` を参照。設計は `docs/architecture/research-tools/paper-results-mcp-jev-trend.md`、読み取りツールは `docs/api/endpoints/research-tools/paper-results-mcp-jev-trend.md`。実装は `internal/repository/evalrecord`（表）・`internal/service/evalrecord`（記録部品）・`internal/bootstrap/nightlyjobs/evaluation.go`（日次タスク）。

## 位置づけ

`jev_evaluation`（#1073）は、呼ぶたびにその期間の判断のラベルを**いまの設定で**付け直す。そのため日々の推移（AI の平均 net bps が良くなっているのか、ベースラインとの差は縮んだのか）は、設定・モデル・質問版が変わると追えない。この表は、JST の 1 日が終わった後にその日の評価の主な数字を**その日の設定のまま**残し、推移を `jev_evaluation_trend` で読めるようにする、**研究専用**の記録。

取引には使わない: Policy・Risk・発注・Paper ガード・Live・Jev のプロンプト・RAG・メタモデルの学習・自己改善は読まない（`internal/repository/evalrecord/isolation_test.go` が、参照できるファイルを記録部品・日次タスク・読み取り専用の MCP ツール `jev_evaluation_trend` に限って固定する）。既存の表には列も制約もトリガも足さない。この表だけを書き、読むのは `jev_decisions`（どの JST 日に trader の LONG/SHORT 判断があるか）と、評価のために `jev_evaluation` と同じ読み取り（`market_snapshots`・`trade_signals`・`runtime_settings` の固定キー）だけ。

**オフ既定**: 記録は `research.eval.enabled`（既定 0）が 1 のときだけ。オフの間は何も書かない（実行の最終成功日も書かない）。

## jev_evaluation_daily

1 行 = **ある JST 日の、1 つの (model_id, question_version) の、1 つのホライズンでの評価**。

| カラム | 型 | 制約 | 説明 |
|-------|-----|------|------|
| id | integer | PK（AUTOINCREMENT） | |
| day | text | NOT NULL, CHECK (長さ 10) | 判断の時刻（`jev_decisions.timestamp`）の JST の日付（`YYYY-MM-DD`） |
| model_id | varchar(100) | NOT NULL | 判断をしたモデル |
| question_version | varchar(20) | NOT NULL | 判断の質問版（`trader-v4` など）。版が変わると別の行（数え直し） |
| horizon_minutes | integer | NOT NULL, CHECK (`> 0`) | ラベルの保有時間（分）。記録時の `calibration.barrier_timeout_minutes` |
| take_profit_bps / stop_loss_bps / barrier_cost_bps | numeric | NOT NULL | 記録時のラベル設定（`calibration.barrier_*`）。日ごとに設定が違うことが分かるよう行に残す |
| decisions | integer | NOT NULL, CHECK (`>= 0`) | その日のそのコホートの trader LONG/SHORT 判断の数（除外前。`trade_signals`・Risk では絞らない） |
| evaluated | integer | NOT NULL, CHECK (`0 〜 decisions`) | llm・買い・売りのラベルがすべて付き、判断の足がある判断の数。全アームの共通の行 |
| entered | integer | NOT NULL, CHECK (`0 〜 evaluated`) | evaluated のうち `trade_signals` に `risk_passed=1` の行がある判断の数（約定したかどうかではない） |
| wins | integer | NOT NULL, CHECK (`0 〜 evaluated`) | evaluated のうち llm の net が 0 より大きい判断の数 |
| llm_sum_bps | numeric | NOT NULL | evaluated の llm（その判断自身の方向）の net bps の合計。コスト控除後 |
| random_sum_bps | numeric | NOT NULL | 同じ行の random（買いと売りの net の平均 = 期待値）の合計 |
| momentum_sum_bps | numeric | NOT NULL | 同じ行の momentum（判断の足の `return_5m` の符号に従う）の合計 |
| contrarian_sum_bps | numeric | NOT NULL | 同じ行の contrarian（momentum の逆）の合計 |
| created_at | text | NOT NULL | 記録した時刻（UTC） |

制約・索引: `UNIQUE (day, model_id, question_version, horizon_minutes)`、`jev_evaluation_daily_day_idx (day)`（期間の読み）。

### 値の意味

- `*_sum_bps` は**コスト控除後の net bps の合計**（`evaluated` 行の合計）。日の平均は `sum / evaluated`（`evaluated = 0` の日は平均なし）。`excluded = decisions - evaluated`（列には持たない）で、約定できる気配なし・窓がホライズンに届かない・足なしに加え、学習 cutoff（`metalabel.llm_cutoff`）より前の判断の除外を含む。
- 判断がある日は、評価できる行が 1 件もなくても**必ず**コホートごとに 1 行（`evaluated = 0`・合計 0）が残る。「その日は判断があったが使えなかった」と「その日は記録していない」を区別するため。
- 日をまたぐ累積は、日ごとの `(evaluated, sum, wins)` を日クラスタとして `evaldiag.SummarizeDays` に渡して作る（`jev_evaluation` と同じブートストラップ）。

### 記録のタイミング・冪等性

- 記録は JST 16:00 以降（引け後で、その日の足が揃ってから）。1 回の実行は**最も古い未記録の 1 日**だけで、残りがあれば `maintenance.ErrMore` で呼び直される（実行の間に DB のロックを手放す）。対象は直近 7 日（当日を含む）のうち trader の LONG/SHORT 判断がある日で、保持期間より古い日は記録しない。
- `INSERT OR IGNORE`（`UNIQUE` キー）で書く。**記録は書いた日のもの**で、再実行しても重複せず、あとで設定を変えても既存行は書き換わらない。1 日が大きすぎて評価できない（`limit_exceeded`）ときは、その日の行を 1 件も書かずエラーにする（部分行は残さない）。

### 保持期間

`research.eval.retention_days`（既定 365、1〜3650）より古い `day` の行を、1000 行ずつの `DELETE` で消す（1 回の実行で最大 10 回。残りがあれば `ErrMore`）。この表以外は消さない。スイッチがオフの間は消さない。

### 取引の入力にしない

この表の値は研究の読み取りだけに使う。Jev・Risk・Policy・注文・資金・Paper ガード・メタモデルの学習の入力・採否の判断にしない。しきい値・合否・推奨も持たない。
