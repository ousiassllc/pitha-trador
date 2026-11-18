# 設計: Paper 実績 MCP の Jev 評価の推移ツール

`docs/architecture/paper-results-mcp.md` §5 の分割章（行数上限のため別ファイル）。issue #1089（その 2）。ツールの引数と出力は `docs/api/endpoints/research-tools/paper-results-mcp-jev-trend.md` §3.42。表の定義は `docs/architecture/er/tables-research-eval.md`。評価そのものは `docs/architecture/paper-results-mcp-jev-evaluation.md`。

## 1. 位置づけ

`jev_evaluation` は呼ぶたびに期間の全判断のラベルを**いまの設定で**付け直すので、設定やモデル・質問版が変わると「日々良くなっているか」を同じ物差しで追えない。そこで JST の日が終わった後に、その日の評価の主な数字を**その日の設定のまま**1 行ずつ `jev_evaluation_daily` に残し（記録部品）、`jev_evaluation_trend` がそれを読んで系列にする。

- 読む表: `jev_evaluation_daily`（`day` の索引）と、`runtime_settings` の `research.eval.enabled` / `research.eval.retention_days` の 2 キーだけ（`trenddiag.settingsReader` が他のキーを SQL の前に拒否する。`runtime_settings` には秘密の置き場がありうるため）。ラベル・足・判断は読み直さない
- 書くもの: ツールは何も書かない（MCP の読み取り接続 `mode=ro` + `query_only`）。記録は別の夜間タスクが書く（§2）
- 取引への影響: 無い。Jev・Risk・Policy・注文・Paper ガード・既存のシャドー・メタモデルの学習・採否には使わない。しきい値・合否・推奨の項目は無い
- 実装: `internal/service/paperresultsmcp/trenddiag`（引数・読み込み・系列）、登録は `research_tools.go`（`queryTool`）、統計は `evaldiag.SummarizeDays`（`stats.Bootstrap`）。記録は `internal/service/evalrecord`、表の SQL は `internal/repository/evalrecord`、タスクは `internal/bootstrap/nightlyjobs/evaluation.go`

## 2. 記録（夜間タスク `jev_evaluation_daily`）

- **オフ既定**: `research.eval.enabled`（既定 0）が 1 でなければ `ErrSkipped`。そのとき何も書かず、実行の最終成功日も書かない。16:00 JST より前も `ErrSkipped`（その日の足が揃うまで）
- 1 回の実行 = 最も古い未記録の 1 日。対象は直近 7 日（当日を含む）の、trader の LONG/SHORT 判断がある日（`jev_decisions` の `timestamp` の索引で読む）。保持期間より古い日は、記録してもすぐ消えるので対象にしない。未記録の日が残れば `maintenance.ErrMore`（ランナーは実行の間にロックを手放す）。判断がある日は評価できる行が 0 でもコホートごとに 1 行ができるので、1 回の実行は必ず前に進む
- 1 日の計算は `evaldiag.Rows`（`jev_evaluation` と同じ読み込み・ラベル・除外）を JST の 1 日 `[D, D+1)` に、既定のホライズン（いまの `calibration.barrier_timeout_minutes`）・全モデル・全版で呼ぶ。コホートごとに 1 行: `decisions`・`evaluated`（`evaldiag.Meta` のコホート別の件数）・`entered`・`wins`（llm の net > 0）・llm / random / momentum / contrarian の net bps の合計・ラベル設定（take_profit・stop_loss・コスト）・ホライズン。`evaluated` の行数と評価の件数が食い違えば保存せずエラー
- 書き込みは 1 日分を 1 トランザクションの `INSERT OR IGNORE`（`UNIQUE (day, model_id, question_version, horizon_minutes)`）。再実行しても重複せず、記録済みの日は書き換えない（記録は書いた日のもの）。1 日が大きすぎる（`evaldiag` の `limit_exceeded`）・評価が失敗したときは、その日は 1 行も書かずエラーを返す（ランナーが隔離し、次の実行で再試行する）
- 保持: 実行の最後に、`research.eval.retention_days`（既定 365）より古い `day` の行を 1000 行ずつ削除（1 実行で最大 10 回、残れば `ErrMore`）。`DELETE` はこの表だけ
- 部品の層: `repository/evalrecord`（SQL。`sqlutil` だけに依存）→ `service/evalrecord`（日の計算・順序・保持）→ `bootstrap/nightlyjobs`（スイッチ・時刻・panic の包み）。panic はエラーにして取引のプロセスを落とさない
- 隔離: `internal/repository/evalrecord/isolation_test.go` が、表名・パッケージを参照できるファイルを上の部品・`nightlyjobs`・`services_build.go` の登録・`trenddiag`・`research_tools.go`・マイグレーションに限り、記録のパッケージの SQL の書き込みが `jev_evaluation_daily` だけであること、`services_build.go` が `scheduler.WithMaintenanceTask(nightlyjobs.X(...))` 以外でこれらを参照しないことを固定する

## 3. 系列の作り方（ツール）

- 期間 `[from, to)` の行を `(model_id, question_version, horizon_minutes, day)` の順に 1 回の SELECT で読む（絞り込み: `prompt_version`・`model_id`・`horizon_minutes`）。行数の上限は 366 日 × 50 コホート = 18,300 行で、超えたら部分結果を返さず `limit_exceeded`（計算は最大でも 0.6 秒ほどで、MCP の 3 秒の予算に収まる）
- 系列は `(model_id, question_version, horizon_minutes)` ごと。日の行: `decisions`・`evaluated`・`excluded`（= decisions − evaluated）・`entered`・`win_rate`（wins / evaluated）・各アームの平均（合計 / evaluated）・llm − random の平均・その日のラベル設定。`evaluated = 0` の日の平均は `null`
- `label_settings_changed`: 系列の隣り合う記録日で take_profit・stop_loss・コスト・ホライズンのどれかが違えば true（違った日は `label_settings_changed_on`）。累積はラベルが違う日を混ぜることになる
- 累積: 日の行 `(evaluated, 合計, wins)` を日クラスタにして `evaldiag.SummarizeDays` で llm の平均・95% 区間・勝率・検出できる最小の差・日数、llm − random（日の合計の差を日クラスタ）の平均・区間・検出できる最小の差を出す。生の判断の行から `evaldiag.Summarize` で出した値と一致する（`trendtests` が確かめる）
- `recording`: スイッチと保持日数（いまの設定）・表全体の最初と最後の記録日・記録日数（期間に関係なく）
- 出力に `definitions`・`caveats`（日が飛びうる・書いた日の設定のまま・検出できる最小の差は記述的・日クラスタの区間は日数が少ないと出ない・random は期待値・スイッチがオフなら新しい日は記録されない）を日本語で付ける。しきい値・合否は付けない

## 4. 制限

- 行の上限 366 × 50 超は `limit_exceeded`。期間・`model_id`・`prompt_version`・`horizon_minutes` で狭める
- 記録は夜間タスクがオンの日だけ。オフの間・記録前・保持期間を過ぎた日は行がなく、系列に日が飛ぶ
- 同じ日の判断は相関するため、日クラスタの区間でも楽観的になりうる。2 日未満は区間が出ない
