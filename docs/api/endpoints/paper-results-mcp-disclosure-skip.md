# API 仕様: Paper 実績 MCP の適時開示による見送りルールの記録の集計ツール

`docs/api/endpoints/paper-results-mcp.md` §3.31 の分割章（行数上限のため別ファイル）。issue #935。共通の上限・トランスポート・エラー形式は `paper-results-mcp.md` と `docs/architecture/paper-results-mcp-errors.md` に従う。読み取り専用（何も書かない）。設計は `docs/architecture/disclosure-skip.md`。

## 3. ツール

### 3.31 `disclosure_skip_summary`

アプリが**記録だけ**している「適時開示による見送りルール」（決算短信・業績予想の修正の直後のエントリーを見送るルール。取引・Policy・Risk・発注には使っていない）が、Jev のエントリー候補の判断についてどう判定したかと、その結果ラベルを数える。`entry_quality_whatif`（§3.30）と同じ考え方で、設定は変えない。

引数: `from` / `to`（他のツールと同じ期間）、`prompt_version`（任意。その版の trader 判断だけを数える。省略は、ライブの版と同じコホートの行だけ。別コホートの版は `prompt_versions.excluded` に出る）。それ以外の引数は `invalid_argument`（422）。

対象は `jev_decisions` の `decision_type = 'trader'` かつ `direction` が `LONG` / `SHORT` の行（エントリー候補。`entry_quality_whatif` と同じ）で、期間は判断の時刻 `timestamp` の `[from, to)`。

応答の `data`:

| キー | 内容 |
|---|---|
| `rows_from` / `rows_to` | 読んだ範囲（保存形式の時刻） |
| `prompt_versions` | `only`（`prompt_version` を渡したときだけ）と `excluded`（数えなかった版） |
| `rule` | `rule_versions`（期間の記録が使ったルールの版）と、`skip` / `no_skip` / `unknown` の意味の説明文 |
| `population` | `entry_candidates`（期間のエントリー候補の判断）= `recorded`（記録のある判断）+ `not_recorded`（記録のない判断。判断から最大 60 分は覆う取得を待つ・アプリが止まっていた・記録部品が動いていない・`NEWS_FEED_URL` で汎用フィードに替えている） |
| `verdicts.skip` / `.no_skip` / `.unknown` | 該当 / 非該当 / 不明の判断のグループ。各 `count`・`labeled`・`unlabeled`・`wins`・`win_rate`・`net_return`（`mean_bps`・`sum_bps`）。`count` = `labeled` + `unlabeled`。`recorded` = 3 グループの `count` の合計 |
| `unknown_reasons[]` | `unknown` の理由別の件数（`code` = `no_fetch`: 判断の後に成功した取得がなかった / `incomplete_fetch`: 成功した取得はあったが件数上限で窓の始まりに届かない・開示時刻を読めない項目があった） |
| `skip_kinds[]` | `skip` の判断を、該当した開示の種類（`earnings` / `forecast_revision`）別に数えた件数（両方に該当した判断は両方に数える） |
| `labels` | 結果ラベルの選び方（`label_version` = 2、`selection`、`win`） |

**ルールの定義**（`rule` に説明文も出る）: 判断時刻 `t` 以前の開示のうち、`t` の JST 日付の当日、または直前の東証営業日の 15:00 JST 以降（15:00:00 を含み 14:59:59 は含まない。土日・祝日をまたぐ）に出た、表題が決算短信・業績予想の修正（上方・下方とも）の開示が 1 件以上あれば `skip`。`t` より後の開示は見ない。配当予想だけの修正・社債の償還完了などは対象外。取得が窓を覆っていて対象が 0 件なら `no_skip`。取得の失敗・欠け・件数上限で窓を覆えず、対象も見つからなければ `unknown`（`no_skip` と区別する）。

**結果ラベル**: 各グループに、既存の Outcome Labeling の結果（`calibration_outcomes`）を当てる。ラベルは `label_version = 2` かつ `net_return_bps` がある行で、1 判断に複数あれば新しい行を 1 つ採る（`entry_quality_whatif`・メタモデルの学習行と同じ規則）。`net_return_bps` はコスト控除後の bps。`wins` = `net_return_bps > 0` の件数（0 は勝ちに数えない）、`win_rate` = `wins / labeled`（的中率。`entry_quality_whatif` の `outcomes` とメタモデルの学習と同じ定義で、`jev_calibration` の `hit_rate`（`was_direction_correct`）とは別の定義）。`labeled` が 0 のグループは `win_rate` と `net_return` が `null`（0 ではない）。ラベルのない判断は `unlabeled` に数え、`win_rate`・`net_return` に入れない（件数 `count` はラベルの有無で変わらない）。

返さないもの: 開示の表題・URL・判断の応答・`state_json`・`response_json`（FR-MCP-5）。集計はこのツールの SQL の中で行い、固定の `SELECT` だけを発行する。

## 変更履歴

| 版 | 日付 | 内容 | 出典 |
|---|---|---|---|
| 1.0 | 2026-10-30 | 新規 | issue #935 |
