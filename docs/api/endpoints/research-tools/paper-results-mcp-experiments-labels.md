# API 仕様: Paper 実績 MCP の実験ツールへの追加（ラベルによる前後比較・仮説つきの設定変更・記録漏れの通知）

`docs/api/endpoints/paper-results-mcp-experiments.md`（§3.25〜§3.28）と `docs/api/endpoints/paper-results-mcp.md` §3.9〜§3.14（`settings_set`）への追加（行数上限のため別ファイル）。issue #1088。設計・計算の方法・暫定値は `docs/architecture/paper-results-mcp-experiments-labels.md`。共通の上限・トランスポート・エラー形式は `paper-results-mcp.md` と `docs/architecture/paper-results-mcp-errors.md`。**新しいツールもエラーコードも無い**（既存のツール 3 つのメンバー・引数の追加だけ）。

## 3. ツール

### 3.28 への追加: `experiment_compare` の `label_based`

既存のメンバー（`experiment` / `min_trades` / `min_trades_source` / `before` / `after` / `diff` / `per_trade_pnl` / `confounded` / `confounders` / `switch_check` / `verdict` / `judged`）は名前も値も**変わらない**。取引ベースの `verdict` / `judged` に何も混ぜず、ラベルによる判定は別メンバー `label_based` に返す。引数は変わらない（`experiment_id`、`min_trades`。`min_trades` は取引ベースだけに効く）。読み取り専用。

`label_based`（取引が 0 件でも返る）:

| メンバー | 内容 |
|---|---|
| `caution` | 固定の注意文（日本語）。ラベルは実現損益ではない・ラベルは時間的に重なる（前後の日の相関は Newey-West で補正しているが、日数が少ないと区間は楽観的になりうる）・前後は別期間で因果の証明にならない・補助の証拠としてだけ使う |
| `interval_method` / `interval_lag_days` | 区間の作り方。`newey_west_bartlett` と、補正のラグ（日。保有時間の最大を日に切り上げた値 = 1）。以下の区間はすべてこれ（issue #1129。設計 §2.2） |
| `label_verdict` | `aborted` / `confounded`（取引ベースと同じ交絡・中止の結果を使う）/ `insufficient_days`（前後どちらかが `days_needed.min_days` 日未満）/ `switch_unconfirmed` / `not_computable` / `improved` / `worsened` / `inconclusive`。判定の順は設計 §2.5 |
| `label_judged` | `improved` / `worsened` / `inconclusive` のとき true |
| `before` / `after` | 窓ごと。`from` / `to`（取引ベースの `before` / `after` と同じ窓）、`days`（ラベルの付いた LONG / SHORT 判断のある JST の日数）、`decisions`（`long` / `short` / `none` / `unknown` / `total`。Jev trader の判断の件数）、`skips`（`skip_count` / `risk_passed_count` / `skip_rate`（null: signal 無し）/ `top_reasons`（`reason` + `count`、最大 5 件。**固定コードだけ**）） |
| `decision_labels` | 判断が付けた方向の、既存のコスト控除後ラベル（`calibration_outcomes` の `label_version = 2` の `net_return_bps`）の前後比較。`horizon_minutes`（比べたラベルのホライズン。ラベルが無ければ 0）、`ignored_rows`（別のホライズンで比べなかった行数）、`long` / `short` / `all`（系列ごとの比較） |
| `research_labels` | 研究用ラベル（#1074）の前後比較。要素は `horizon`（`30m` / `60m` / `close`）と `long` / `short`（系列ごとの比較）。記録が済んだ、窓に丸ごと入る JST の日だけを使う |
| `days_needed` | 判定まであと何日か。`observed_per_side`、`min_days`（8）/ `min_days_source`（`measured_v1`。根拠は設計 §2.4。暫定値ではない）、`days_to_min`、`needed_per_side`、`remaining_days`、`reason` |
| `error` | 省略。付くのは、ラベルの比較を計算できなかったとき: `limit_exceeded`（窓の判断が 200,000 件超、または窓が 400 日超）/ `failed`。このとき `decision_labels` / `research_labels` / `days_needed` は null で `label_verdict` は `not_computable`。**呼び出しは成功のまま**で、取引ベースのメンバーは返る |

系列ごとの比較（`long` / `short` / `all`）: `before` / `after` は `n`（観測の件数）・`days`・`mean_net_bps`（日平均の平均）・`ci_low` / `ci_high`（日クラスタの 95% 区間を、前後の日の相関で Newey-West 補正したもの。日が 2 日未満は null）、`diff` は後−前の `diff_bps` と、日平均どうしの Welch の区間（各窓の分散は Newey-West の長期分散） `ci_low` / `ci_high`・`t`・`df`（計算できないものは null。`diff` 自体が null のことがある＝どちらかの窓に日が無い）。

`days_needed`:

- `needed_per_side`: 今の日ごとのばらつき（Newey-West の長期分散）と差のままで、前後それぞれ何日あれば区間の半幅が差の絶対値を下回るか（最小日数を下回らない）。
- `remaining_days` = max(0, `needed_per_side` − `observed_per_side`)。目安であり、日が増えれば差もばらつきも動く。
- `needed_per_side` / `remaining_days` が null のとき `reason`: `variance_undefined`（どちらかの窓が 2 日未満）/ `diff_near_zero`（差が 0）/ `beyond_horizon`（250 日でも足りない）。`days_to_min` は常に出る。

範囲と予算: 読み取り接続 1 本・3 秒の予算の中で、判断の件数・ラベルは窓ごとに集計の SQL 数回、研究ラベルは日ごとの和の表を日の範囲で読む（行は読まない）。見送りは同じ呼び出しの `risk_rejections` と同じ集計（読み直さない）。

### 3.9〜3.14 への追加: `settings_set` の `hypothesis`

| 引数 | 必須 | 内容 |
|------|------|------|
| `key` / `value` | ○ | 従来どおり |
| `hypothesis` | | 仮説（1〜500 文字。`experiment_create` と同じ規則）。**渡すと**、この変更を 1 件の実験（`kind` = `setting`、`name` = `key`、`before` / `after` = 保存の前後の有効な値、`audit_id` = この変更の監査行、`started_at` = 書き込みの時刻）として、**設定の保存・監査行と同じトランザクション**で記録する |

- 応答: 従来のメンバー（`key` / `unit` / `value` / `before` / `audit_id` / `at`。LLM cutoff のキーは `llm_cutoff_status`）に、**仮説を渡したときだけ** `experiment`（`experiment_create` の応答と同じ形）を足す。仮説なしの応答は従来とバイト同一。
- 検査の順（どれも何も保存する前）: ホワイトリスト（`key_not_allowed` 403）→ 書き込み許可（`write_disabled` 403）→ 仮説（空・空白だけ・500 文字超・制御文字は `invalid_argument` 422。`error.data.detail` に理由）→ 値の検証（`invalid_value` 422）。保存後に前後の値が同じ（変えていない）なら `invalid_argument`（実験は作れない）で、**設定・監査行・実験のどれも残らない**。実験の保存に失敗したときも同じ（全部ロールバック）。
- 書き込みの範囲は増えない: `runtime_settings`（そのキー 1 行）・`mcp_settings_audit`（1 行）・`tuning_experiments`（1 行）。ホワイトリスト・書き込み許可の意味は変えない。実験の判定が設定を変えたり戻したりすることは無い。

### 3.27 への追加: `experiments_list` の `unrecorded_changes`

既存のメンバー（`experiments`）と並びは変えない。引数も変わらない。読み取り専用で、何も書かない・実験を作らない。

`unrecorded_changes`（常に配列。空のことがある）: 直近 14 日の設定・Jev のモデル・Jev のプロンプトの版の変更のうち、どの実験にも記録されていないもの。新しい順、最大 20 件。

| メンバー | 内容 |
|---|---|
| `kind` | `setting` / `model` / `prompt` |
| `name` | 設定キー / `JEV_MODEL` / 判断の種類（`scout` / `trader`。その種類のプロンプトの版が変わった） |
| `changed_at` | 保存（設定・モデル）または最初に使われた（プロンプトの版）時刻。UTC RFC3339 |
| `audit_id` | `setting` のみ。`settings_audit` の行 ID（`experiment_create` の `audit_id` に渡せる） |
| `before` / `after` | 変更前後の値（なければ null。秘密のキーは `***`。現状ホワイトリストに秘密のキーは無い） |

「記録済み」の規則: 設定は、その `audit_id` を持つ実験、または同じキーの設定の実験で `started_at` が保存時刻の ±15 分以内（交絡・切り替えの確認と同じ規則）。モデル・プロンプトの版は、`after` がその値の同じ種類の実験で、`started_at` が変更の 24 時間前以後。詳細は設計 §4。

### 受け入れ条件

- 取引が 0 件でも、`experiment_compare` の `label_based` に前後の判断の件数・ラベルの平均・日クラスタの区間・あと何日か（`days_needed.remaining_days`）が返る（`experimentlabeltests.TestLabelBasedComparisonWorksWithZeroTrades`）。
- 取引ベースの `verdict` / `judged` を含む既存のメンバーは、名前も値も変わらない（`TestTradeBasedFieldsAreUnchangedByTheLabelMember`。既存の `experimenttests` はそのまま通る）。
- `settings_set` に `hypothesis` を付けると、実験がちょうど 1 件でき、その `audit_id` が設定の監査行と一致する。付けないときは従来どおりで実験は作られない。実験が作れないときは設定も保存されない（`TestSetWithHypothesisCreatesOneLinkedExperiment` / `TestSetWithoutHypothesisIsUnchanged` / `TestSetWithHypothesisIsAllOrNothing`）。
- 設定・モデル・プロンプトの版の変更で実験が記録されていないものが `unrecorded_changes` に出る（`TestUnrecordedSettingChangesAreListedAndCoveredOnesAreNot` / `TestUnrecordedModelAndPromptChanges`）。
- 取引・Risk・発注・Jev に差分が無く、新しい表も無い。`internal/service/paperresultsmcp/experimentlabeltests/isolation_test.go` が参照元を固定する。

## 変更履歴

| 版 | 日付 | 内容 | 出典 |
|---|---|---|---|
| 1.0 | 2026-11-15 | 新規 | issue #1088 |
| 1.1 | 2026-11-20 | `label_based` に `interval_method` / `interval_lag_days` を追加。`min_days` を 8、`min_days_source` を `measured_v1` に変更。区間と `days_needed` を Newey-West（ラグ 1 日）で補正 | issue #1129 |
