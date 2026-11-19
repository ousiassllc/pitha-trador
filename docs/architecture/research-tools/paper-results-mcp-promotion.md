# 設計: Paper 実績 MCP の昇格チェックリスト（promotion_checklist）

`docs/architecture/paper-results-mcp.md` の分割章（行数上限のため別ファイル）。issue #1091。ツールの引数と出力は `docs/api/endpoints/research-tools/paper-results-mcp-promotion.md`。数字は `candidate_ledger`（`docs/architecture/research-tools/paper-results-mcp-candidate-ledger.md`）と同じ計算。**Settings 画面の「昇格チェックリストの基準」（グループ `research-promotion`）は、基準の値をこの章に書いてある、と案内している。**

## 1. 位置づけ（何もしない）

候補（シャドーのプロンプト版・お試しのアーム・研究用モデル・ベースライン・別コホート。設定の組＝調整の実験は §6）を、いまのもの（既定はライブの Jev）に置き換える**提案のための証拠**を、固定の 11 項目で並べる読み取り専用のツール。

- **何も適用しない**: `applied` は常に `false`。設定・プロンプト版・モデル・`jev_prompt_promotions`・シャドー・お試しの表を変えない。何も書き込まない（MCP の読み取りトランザクションの中だけ。テストが全表の前後の一致と、SELECT だけが実行されたことを固定する）。
- `eligible_as_evidence`（全項目が `pass`）は**指示ではない**。採用は人間が決める（あおいの提案と悠太さんの OK の後）。出力の `notice` に固定文で出る。
- Jev・Risk・Policy・注文・Paper ガード・既存のシャドー・メタモデルの学習には使われない。`runtime_settings` は `research.promotion.*` の 5 キーだけを読む（それ以外のキーは SQL を発行する前に拒否する読み取り口）。
- 実装: `internal/service/paperresultsmcp/promotiondiag`（`Check` が型付きの結果、`Run` がツール）、基準の読み込みと下限は `internal/config/promotionsettings`、登録は `research_tools.go`。テストは `promotiontests`（HTTP）と `promotiondiag` の単体、隔離は `ledgertests/isolation_test.go`。
- 設定の組（調整の実験。`experiment` / `setting`）は同じ行で並べられないので、候補のとき（`current` なし）だけ、`experiment_compare` の前後比較を同じ 11 項目で見る（**§6**）。「いまのもの」にはできない。比べられない種類・組み合わせは専用コード `unsupported_candidate_kind`（422。`invalid_argument` とは別。MCP のエラー表は `docs/architecture/paper-results-mcp-errors.md`。応答の `error.data.detail` に規則の固定文を付け、引数の値や id は引用しない）で拒否する。別コホート（`live_cohort`）は別の判断を見ているので候補にはできるが比較の項目はすべて `unknown`（`different_decisions`）、「いまのもの」にはできない（`unsupported_candidate_kind`）。

## 2. 数値の基準（フロア）

次の 5 つが数値の基準で、**この表の値がフロア（文書化された値）**。`promotionsettings` の `FloorMinCommonRows` ほかと同じ値で、一致をテストが固定する。

| 基準 | フロア | 向き | Settings のキー | 呼び出しの引数 |
|---|---|---|---|---|
| 共通の行数の最小 `min_common_rows` | **300** | 大きいほど厳しい | `research.promotion.min_common_rows` | `min_common_rows` |
| 共通の行が及ぶ JST 日数の最小 `min_days` | **10** | 大きいほど厳しい | `research.promotion.min_days` | `min_days` |
| 差（候補 − いまのもの）の日クラスタ 95% 区間の下端の最小（bps）`min_diff_ci_lower_bps` | **0** | 大きいほど厳しい | `research.promotion.min_diff_ci_lower_bps` | `min_diff_ci_lower_bps` |
| 候補が勝った日の割合（%）の最小 `min_positive_day_pct` | **50** | 大きいほど厳しい | `research.promotion.min_positive_day_pct` | `min_positive_day_pct` |
| Holm 補正後の p の最大 `max_adjusted_p` | **0.05** | 小さいほど厳しい | `research.promotion.max_adjusted_p` | `max_adjusted_p` |

固定の定数（設定ではない）: `MinCoverage = 0.8`（項目 1）。

**厳しくする方向にだけ変えられる**:

- Settings（グループ `research-promotion`、`runtime_settings` の `research.promotion.*`）は、フロアより緩い値を**保存できない**（`promotionsettings.Validate`）。保存済みの値がフロアより緩い・壊れているときは**無視してフロアを使う**（`promotionsettings.Effective`。ログに警告）。未設定はフロア。
- 呼び出しの引数は、その時点で有効な基準（フロアか、より厳しい Settings の値）より緩いと `invalid_argument`（422）。同じ値か厳しい値は受け付ける（`Criteria.Tighten`）。
- 応答の `criteria` に `effective`（有効な値）・`floor`（上の値）・`source`（キーごとに `floor` / `setting` / `call`）・`min_coverage` を出す。

## 3. 11 の項目（この順で固定）

各項目は `{name, status, actual, required, comparator, detail}`。`status` は `pass` / `fail` / `unknown`。`unknown` は判定に必要な入力・データが無いことを示し、`pass` ではない（`first_unmet` に出る）。`detail` は短いコード（`ok` ほか）。

| # | `name` | 合格の条件（`pass`） | `unknown` になる場合 |
|---|---|---|---|
| 1 | `same_rows_and_period` | 共通の行（候補といまのものの両方が答えた行）÷ 期間内の基準（ライブ）の行 **≥ 0.8**（`MinCoverage`） | 基準の行が無い |
| 2 | `common_rows` | 共通の行数 **≥ `min_common_rows`**（フロア 300） | `live_cohort`（`different_decisions`） |
| 3 | `common_days` | 共通の行が及ぶ JST 日数 **≥ `min_days`**（フロア 10） | 同上 |
| 4 | `diff_ci_lower` | 差（候補 − いまのもの。同じ行ごと・コスト控除後ラベル）の日クラスタ 95% 区間の**下端 > `min_diff_ci_lower_bps`**（フロア 0。厳密に超えること） | 2 日未満で区間が無い |
| 5 | `multiple_comparison` | Holm 補正後の p **≤ `max_adjusted_p`**（フロア 0.05）。族 = 台帳の p を持つ全候補 + `family_size_extra`（p=1 の比較として足す） | p が無い（2 日未満・ばらつきなし） |
| 6 | `positive_days` | 差の日合計が正の日の割合（%）**≥ `min_positive_day_pct`**（フロア 50） | 共通の行が無い |
| 7 | `not_one_day_wins` | 最も良かった 1 日を除いた差の合計（bps）**> 0** | 共通の行が無い |
| 8 | `max_drawdown` | 共通の行を時刻順に並べた候補の最大ドローダウン（bps。累積 net のピークからの最大の落ち込み。開始の 0 もピーク）**≤ いまのものの最大ドローダウン**（`required` はいまのものの値） | 共通の行が無い |
| 9 | `after_ai_cost` | AI の費用の差（候補 − いまのもの）を bps に換算して引いた後の差の平均 **> 0** | `usd_jpy` と `notional_jpy` が両方無い、または費用が不明な行がある（`detail`: `usd_jpy_and_notional_jpy_required` / `cost_unknown` / `reference_cost_unknown`） |
| 10 | `after_training_cutoff` | `metalabel.llm_cutoff` が仮置きでなく適用済み（`llm_cutoff.applied`。cutoff 前の判断は台帳が除外済み） | cutoff が仮置き（`llm_cutoff_is_placeholder`。LLM が評価期間の値動きを知っていた可能性を除けない） |
| 11 | `model_version_stable` | 期間内の `model_id` が、候補・いまのもの（ライブ）の**どちらも 1 種類**（`actual` に `candidate_model_ids` / `current_model_ids` の数。2 種類以上は `fail`: `more_than_one_model_id`） | どちらかに答えが無い |

全体: `first_unmet`（`pass` でない最初の項目名。全て `pass` なら null）・`eligible_as_evidence`（全項目が `pass`）・`applied: false`・`notice`・`key_numbers`（`reference_rows`・`n_common`・`coverage`・`days`・`candidate_mean_net_bps`・`current_mean_net_bps`・`diff_mean_bps`・`diff_ci`・`p_value`・`holm_adjusted_p`・`family_size`・`family_size_extra`・`positive_day_pct`）。

## 4. 計算

台帳（`ledgerdiag.Compute`）を `alignment=pairwise` で呼ぶ。候補は、いまのものと**両方が答えた行**（共通の行）で、同じ行ごとの差（候補 − いまのもの）を取る。Holm の族は、期間に見つかった全候補（`live_cohort` を除く、p を持つもの。提案する候補は期間に答えが無くても族に数える）。土台の行はいつも**ライブの版の判断の行**で、被覆率・欠測は基準（ライブ）の行数に対して数える。「いまのもの」がライブならそのまま、別の候補（シャドー・trial・研究用モデル・ベースライン）なら、その候補の答えを基準として差・ドローダウン・費用の差を取る。区間・p・Holm は台帳の値そのもの（`candidate_ledger` の `alignment=pairwise` の呼び出しと一致する。テストが `stats.Holm` で固定）。

## 5. 隔離とテスト

- 全表の前後の一致（`runtime_settings`・`jev_prompt_promotions`・シャドー・お試し・研究用の表）、実行した文が SELECT だけであること、本文の列を読まないこと、`runtime_settings` の他の値・API キーが応答に出ないこと（`promotiontests/criteria_test.go`）。
- 5 つの基準それぞれについて、緩い値の拒否・フロアの受理・厳しい値の反映と `source`。フロアより緩い保存値の無視。保存値より緩い呼び出しの拒否（`criteria_test.go`）。
- 強い合成候補が全項目 `pass`、弱い候補が正しい `first_unmet` と `actual` / `required`、`unknown` の扱い（費用・cutoff）、被覆率・行数・日数、モデルの変更、候補・いまのものの種類（`checklist_test.go`・`kinds_test.go`）。
- `unsupported_candidate_kind` が `invalid_argument` と見分けられること（`current` への `experiment` / `setting`・`live_cohort`、`experiment` / `setting` と `current` の組み合わせ、ホワイトリスト外の設定キー）、応答に引数の値が出ないこと、エラー表に載っていること（`promotiontests/unsupported_test.go`・`promotiondiag/settingsset_items_test.go`）。
- 設定の組の候補（§6）: 実験の前後比較が適用できる項目で `pass`、適用外の 3 項目が `unknown`（`not_applicable`）で `fail` にならないこと、実験の判定保留（件数不足・交絡）で `unknown`、`setting` が最新の実験を読むこと・実験が無いときの全 `unknown`、ツールが何も変えない（全表の前後の一致。`promotiontests/settingsset_test.go`）。`ledgertests/isolation_test.go` が、チェックリストが実験を読むだけ（記録・終了の関数を使わない）ことと、読む表・取り込むパッケージの許可リストを固定する。

## 6. 設定の組・調整の実験を候補にする（`kind=experiment` / `setting`）

issue #1125。設定の組（調整の実験）は、同じ判断の行で候補と「いまのもの」を並べられない（前後は別の期間・別の取引）。そこで行をそろえず、**`experiment_compare` の前後比較の結果**を、11 項目と同じ物差し（件数・日数の下限、差の区間、多重比較、一部の日だけの勝ちでないか、最大ドローダウン）で見る。結果は §1 と同じく**提案のための証拠**を返すだけで、何も適用しない（`applied` は `false`。設定・プロンプト版・モデル・実験の表を変えず、何も書かない）。

### 6.1 指定のしかた

| `candidate` | 読む実験 | `current` | 期間・`horizon`・`usd_jpy`・`notional_jpy` |
|---|---|---|---|
| `kind=experiment`、`id` = 実験の id（正の整数） | その実験（無ければ `experiment_not_found`） | **指定しない**（実験の前の期間で決まる。応答の `current` は `kind=experiment_before`・同じ実験の id） | 指定しない（`invalid_argument`。前後は実験自身の期間。取引の損益に AI の費用は紐づかない） |
| `kind=setting`、`id` = MCP の設定ホワイトリストのキー | そのキーを変数にした実験のうち**最も新しい**もの（実験の状態は問わない。判定が出ていなければ項目が `unknown` になる） | 同上 | 同上 |

基準の 5 つ（`min_common_rows` ほか）と `family_size_extra` は、ほかの候補と同じに指定でき、**フロアより厳しい方向にだけ**変えられる（§2。緩い値は `invalid_argument`）。`setting` の実験が 1 件も記録されていないときは、エラーにせず全項目 `unknown`（`no_experiment_result`）と説明（`evidence_note`）を返す。

### 6.2 何を読むか

`experiment.Compare`（`experiment_compare` と同じ関数・`min_trades` は下限 64）の結果をそのまま使う。後 = 実験の開始から終了（実行中は今）まで、前 = 開始の直前の同じ長さ（`docs/architecture/paper-results-mcp-experiments.md` §3）。区間・p の元は 1 取引あたりの純損益（コスト控除後・円）の Welch 検定の差（後 − 前）。日ごとの数字は、同じ取引集合（`query.DailyPnL`・クローズ日の JST）から作る。応答には、読んだ実験（`experiment`: id・状態・変数の種類と名前・`experiment_compare` の `verdict` / `judged`・交絡の件数・前後の期間）と、項目の根拠の数字（`experiment_numbers`）を付ける（`key_numbers` は付かない）。変数の before / after の値は出さない。

実験自身の判定が保留（`aborted`・`confounded`・`switch_unconfirmed`・`insufficient_trades`・`not_computable`）のときは、**区間や損益を読む項目 4〜8 を `unknown`**（`detail` は `experiment_<判定>`）にして、実験の判定の外で合格にしない。数字そのものは `experiment_numbers` に出る。

### 6.3 11 項目の対応

| # | `name` | 設定の組での見方 |
|---|---|---|
| 1 | `same_rows_and_period` | **適用外**。前後は別の取引で、同じ行がない。`unknown`（`not_applicable_unpaired_periods`）。`fail` にしない |
| 2 | `common_rows` | 前後の取引件数の**少ない方** ≥ `min_common_rows`（フロア 300。実験自身の最低件数 64 より厳しい基準をそのまま使う） |
| 3 | `common_days` | 前後それぞれの取引がある JST 日数の**少ない方** ≥ `min_days`（フロア 10） |
| 4 | `diff_ci_lower` | 1 取引あたり純損益（円）の差（後 − 前）の Welch 95% 区間の下端 **> 0**（`experiment_compare` の区間そのもの）。基準 `min_diff_ci_lower_bps` は bps（判断の単位）で、円と換算する根拠がないので、**フロアの 0 のときだけ**判定する（0 はどちらの単位でも符号の判定で同じ）。0 より厳しくしたときは `unknown`（`criterion_in_bps_not_comparable_to_yen_per_trade`）で、緩めずに判定を保留する |
| 5 | `multiple_comparison` | 差の両側 p に補正をかけた値 ≤ `max_adjusted_p`。族 = 記録された**全実験** + `family_size_extra`。ほかの実験の p は計算せず 1 とみなす（`family_size_extra` と同じ扱い）ので、補正後の p = min(1, p × 族の大きさ)（この実験を族で最も良いものとして Holm の第 1 段を当てる。本物の Holm より低くならない） |
| 6 | `positive_days` | 後の期間の取引がある日のうち、その日の純損益が前の期間の 1 日あたり平均（前の合計 ÷ 前の取引がある日数）を超えた日の割合（%）≥ `min_positive_day_pct`。前後で日を 1 対 1 に組めないため、日ごとの差ではなくこの形にした |
| 7 | `not_one_day_wins` | 後の合計から最も良かった 1 日を除いた額 − 前の合計（円）**> 0** |
| 8 | `max_drawdown` | 後の期間の最大ドローダウン ≤ 前の期間のもの（`experiment_compare` の `performance` の値。円） |
| 9 | `after_ai_cost` | **適用外**。取引の損益に AI の費用が紐づいていない。`unknown`（`not_applicable_ai_cost_not_in_trade_pnl`） |
| 10 | `after_training_cutoff` | **適用外**。取引は LLM の学習の cutoff で選別されていない。`unknown`（`not_applicable_trades_not_cutoff_filtered`） |
| 11 | `model_version_stable` | 実験の変数以外の Jev のモデル・プロンプトの版が前後の期間と境目で変わっていない（`experiment_compare` の交絡のうち `model` / `prompt`。件数を `actual`、0 を `required`）。変わっていれば `fail`（`model_or_prompt_version_changed`）。設定・方針の提案の変更は交絡として実験の判定が保留になり、項目 4〜8 が `unknown` になる |

適用できない項目（1・9・10）は、**不合格にせず**理由つきの `unknown` で、項目に `not_applicable: true` を付ける。件数・日数・日の数字が出せない（取引が無い）ときも `unknown`。`first_unmet` は「適用外でない最初の `pass` でない項目」で、適用外の項目は `first_unmet` に出ない。

### 6.4 根拠に使えない（`eligible_as_evidence` は常に `false`）

設定の組の結果は、前後が別の期間・別の取引なので、同じ行ごとの差を取る候補と同じ強さの根拠にならない（項目 1 が成り立たない。区間は取引が同じ日・同じ相場に偏って楽観的になりうる。前後の差は変えたことが原因だという証明にもならない。`paper-results-mcp-experiments.md` §4.1）。そのため、適用できる項目が全て `pass` でも `eligible_as_evidence` は常に `false` で、理由を `evidence_note` に固定文で出す。項目ごとの数字は、人間が提案を読むための参考。採用は人間が決める（§1）。

### 6.5 `unsupported_candidate_kind` に残るもの

次は比べられないので `unsupported_candidate_kind`（422）で拒否する。`error.data.detail` は規則の固定文で、引数の値は含めない。

- `current` に `experiment` / `setting`（「いまのもの」にはできない。候補のときの「いまのもの」は実験の前の期間に決まっている）
- `current` に `live_cohort`（別の判断を見ていて同じ行がない。従来どおり）
- `candidate` が `experiment` / `setting` で `current` を指定した組み合わせ（前の期間と別の「いまのもの」を比べる方法がない）
- `candidate` が `setting` で、`id` が MCP の設定ホワイトリストにないキー（その設定は交絡を確かめられず、実験を作れない。`key_not_allowed` と同じ理由）

それ以外の引数の誤り（実験の `id` が正の整数でない、`experiment` / `setting` に `horizon` などを付ける、基準を緩める、未知の引数）は `invalid_argument`。

### 6.6 判断したこと（オーナーの確認が要る）

- `setting` は「そのキーの最新の実験」を読む（実験を指定したいときは `kind=experiment`）。複数の実験をまとめて 1 つの結果にはしない。実験が無いときはエラーでなく全 `unknown`（保守的）。
- 項目 4 の `min_diff_ci_lower_bps` は 0 のときだけ判定する（円と bps の換算を置かない）。0 より厳しくした場合の扱いは `unknown`。
- 項目 6・7 は、前後で日が 1 対 1 に組めないため、前の期間の平均・合計との比べ方にした（§6.3）。
- 項目 1・9・10 は適用外として `unknown`。そのため設定の組は `eligible_as_evidence` が常に `false`（§6.4）。適用外の項目を `pass` として数える形に変える（根拠に使えるようにする）かどうかは、あいり・悠太さんの判断。
- 多重比較の族は記録された全実験（ほかの実験の p は 1 とみなす）。

### 6.7 実装

`promotiondiag/settingsset*.go`（`Check` が `kind` で分岐。`checkSettingsSet` が実験を読み、`settingsSetItems` が 11 項目を作る）。実験は `experiment.List` / `Get` / `Compare` と `query.DailyPnL` で**読むだけ**（記録・終了・設定の変更を呼ばない。`ledgertests` が固定）。`unsupported_candidate_kind` は `promotiondiag.UnsupportedError`（`query.ErrBadCursor` にも畳める）を MCP の `research_tools.go` がコードに写す。

## 変更履歴

| 版 | 日付 | 内容 | 出典 |
|---|---|---|---|
| 1.0 | 2026-11-15 | 新規 | issue #1091 |
| 1.1 | 2026-11-19 | 専用エラーコード `unsupported_candidate_kind` を §1 に。設定の組・調整の実験（`experiment` / `setting`）を候補にする設計と実装を §6 に追加（`experiment_compare` の前後比較を同じ 11 項目で見る。適用外の項目は `unknown`、根拠には使えない）。§5 にテストを追記 | issue #1122・#1125（親 #1091） |
