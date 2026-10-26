# API 仕様: Paper 実績 MCP の entry_quality の仮定集計ツール

`docs/api/endpoints/paper-results-mcp.md` §3.30 の分割章（行数上限のため別ファイル）。issue #911。共通の上限・トランスポート・エラー形式は `paper-results-mcp.md` と `docs/architecture/paper-results-mcp-errors.md` に従う。読み取り専用（設定も signal も書かない）。

## 3. ツール

### 3.30 `entry_quality_whatif`

Policy Engine の `entry_quality_below_threshold` は、ガードを順に見て**最初に落ちた条件だけ**を `reject_reason` に残す。`min_entry_quality` を下げたとき、その判断が後ろのガード（継続確率・toxic_flow・流動性ストレス）も通るのかは、記録からは分からない。このツールは、保存された Jev の回答に Policy Engine と同じガード（`policy.ThresholdReject`。エンジンの判定と同じ関数で、写しではない）を、別の `min_entry_quality` で当てて数える。設定値は変えない。

引数: `from` / `to`（他のツールと同じ期間）、`min_entry_quality`（任意。`poor`〜`exceptional` の配列、最大 5 つ、買いにも売りにも同じ値。省略は `fair` / `good` / `strong`）、`prompt_version`（任意。その版の trader 判断だけを数える。省略は、ライブの版と同じコホートの行だけ。別コホートの版は `prompt_versions.excluded` に出る。#881）。それ以外の引数は `invalid_argument`（422）。LONG/SHORT の trader 判断が 50,000 件を超える期間は、一部だけ返さず `limit_exceeded` にする。

応答の `data`:

| キー | 内容 |
|---|---|
| `rows_from` / `rows_to` | 読んだ範囲（保存形式の時刻） |
| `prompt_versions` | `only`（`prompt_version` を渡したときだけ）と `excluded`（数えなかった版） |
| `thresholds` | `long` / `short` ごとの `min_continuation_probability`・`max_toxic_flow`・`max_liquidity_stressed`。今の `policy.*`（`config/strategy.yaml` に `runtime_settings` の上書きを当てたもの。上書きが効いたキーは `overridden_keys`）。過去の判断も今の値で判定する |
| `population` | LONG/SHORT の判断がどこへ行ったか。`trader_decisions`（NONE を含む全件）・`long_short`・`vwap_reversion`（VWAP 逆張りの規則で判定され、entry_quality が掛からない）・`not_lendable_short`（貸借なしの売りで、entry_quality の手前で見送り。`reject_reason` が `not_lendable`）・`rejected_before_guards`（`rejected_before_guards_by_reason` に理由別。signal の行が無い判断は `no_signal`）・`incomplete_answer`（ガードが読む回答の項目が欠けている）・`reached_guards`（エントリーのガードまで届いた判断）。`long_short` = 後ろの 5 つの合計 |
| `entry_quality_distribution` | `prompt_version` × `entry_quality`（`poor`〜`exceptional`、読めない回答は `unknown`）の件数。LONG/SHORT 全件 |
| `scenarios[]` | `min_entry_quality` ごと（低い順）に `total` / `long` / `short`。各 `candidates`（= `reached_guards`）・`below_entry_quality`・`pass_entry_quality`・`blocked_continuation_probability`・`blocked_toxic_flow`・`blocked_liquidity_stressed`・`pass_all_guards`。`candidates` = `below_entry_quality` + `pass_entry_quality`、`pass_entry_quality` = 後ろの 3 つの「落ちる」+ `pass_all_guards`（エンジンの順で最初に落ちた 1 つだけ数える） |

「手前で落ちた」の判定には、判断に紐づく `trade_signals.reject_reason` の先頭のコード（`:` の前）だけを使う（詳細文は読まない）。判断の回答は `jev_decisions.response_json` から `entry_quality`・`continuation_probability`・`toxic_flow`・`liquidity_stressed` の 4 項目だけを SQL の中で取り出し、応答そのものは返さない。

含まないもの: メタモデルの判定と確率のしきい値（ライブの規則はそれらを確率の判定の代わりにモデルの判定で置き換える。保存された回答からは再現できない）、リスクエンジン。したがって `pass_all_guards` は「ガードを通る」件数で、エントリーされる件数ではない。`min_entry_quality`（Paper は既定 `good`、Live は `strong`）を変える判断はこのツールの対象外で、人が決める。

## 変更履歴

| 版 | 日付 | 内容 | 出典 |
|---|---|---|---|
| 1.0 | 2026-10-26 | 新規 | issue #911 |
