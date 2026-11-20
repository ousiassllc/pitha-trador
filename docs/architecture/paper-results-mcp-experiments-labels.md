# Paper 実績 MCP: 取引ゼロでも判定できる実験の比較（ラベルによる前後比較）と、変更の記録漏れの検出（issue #1088）

`docs/architecture/paper-results-mcp-experiments.md`（調整の実験。issue #871）の追加章（行数上限のため別ファイル）。ツールの引数と出力は `docs/api/endpoints/research-tools/paper-results-mcp-experiments-labels.md`。

## 1. 目的と決めたこと

`experiment_compare` の判定は、前後のクローズ済み Paper 取引の 1 取引あたり純損益を比べる（Welch。前後それぞれ `min_trades`＝下限 64 件。`docs/architecture/paper-results-mcp-experiments.md` §4・§5）。Paper の取引はほぼ 0 件なので、**どの実験も判定されない**（`insufficient_trades`）。また `settings_set` と `experiment_create` が別の呼び出しなので、実験の記録を忘れやすい。

| # | 決めたこと |
|---|---|
| 1 | 取引ゼロでも出る指標を、`experiment_compare` の応答の**別メンバー `label_based`** として足す。前後の窓は取引ベースと同一（`experiment.Experiment.Windows`）。 |
| 2 | 取引ベースの `verdict` / `judged` と、その他の既存メンバー（名前・値）は**変えない**。ラベルの判定は `label_based.label_verdict` / `label_judged` に分け、`verdict` には混ぜない。`TestTradeBasedFieldsAreUnchangedByTheLabelMember` が、`label_based` を除いた応答が `experiment.Compare` の直接の結果と一致することと、メンバー名を固定する。 |
| 3 | `settings_set` に任意の `hypothesis` を足す。渡すと、設定の保存・監査行・実験の作成を**同じトランザクション**で行う。省略時の動作と応答は従来どおり（バイト同一。`TestSetWithoutHypothesisIsUnchanged`）。 |
| 4 | 設定・モデル・プロンプトの版が変わったのに実験が記録されていない変更を、`experiments_list` の `unrecorded_changes`（読み取りだけ）で見えるようにする。 |
| 5 | **何も自動で変えない**: 判定（取引ベースも `label_verdict` も）は設定・モデル・プロンプトを変えも戻しもしない。`settings_set` のホワイトリスト（`internal/config/mcpwritesettings`）、書き込み許可のスイッチの意味、Live の Jev・Risk Engine・発注・Paper ガード・既存のシャドー（trader-v5）には触れない。 |
| 6 | **新しい表も移行も無い**。読むのは既存の表だけ: `jev_decisions`、`calibration_outcomes`、`trade_signals`、`research_label_days` / `research_label_day_stats`（issue #1074）、`mcp_settings_audit`、`jev_model_changes`、`tuning_experiments`。 |

## 2. ラベルによる前後比較（`internal/service/paperresultsmcp/experiment/labelcompare`）

`experiment/` 配下は行数の警告を超えているので、新しいパッケージに置く。`experimenttool`（`experiment_compare`）は `labeltool.Compare` を呼ぶだけで、`experiment.Compare` の結果に `labelcompare.Attach` で `label_based` を足した `labelcompare.Response`（`experiment.Comparison` を埋め込んだ構造体。JSON では既存のメンバーがそのまま並ぶ）を返す。

### 2.1 前後それぞれの窓で見るもの

| 項目 | 読み元 | 内容 |
|---|---|---|
| 判断の件数 | `jev_decisions`（`decision_type = 'trader'`） | LONG / SHORT / NONE の件数と合計（方向が無い行は `unknown`）。窓の判断が `MaxDecisions`（200,000）を超えると `limit_exceeded`（§2.6） |
| 判断のラベル | `jev_decisions` と `calibration_outcomes`（`label_version = 2`＝コスト控除後の triple barrier、`net_return_bps`） | 判断が付けた方向（LONG / SHORT）のラベルの、JST の日ごとの件数と和。LONG・SHORT・合算（`all`。ラベル判定の本体）。ラベルが 2 つ以上のホライズン（`horizon_minutes`）にあるときは、判断の数が最も多い 1 つ（同数なら短い方）だけを比べ、残りの行数を `ignored_rows` に出す。コストは既存のラベラーが置いたもの（`costsettings.LabelBarriers`）をそのまま使い、別のコストを置かない |
| 見送り | `trade_signals`（`query.Rejections`。`Compare` が既に前後で読んだ `risk_rejections` の値を使い、読み直さない） | `skip_count` = Policy Engine 自身の見送り（`other_reject_count`）＋ Risk Engine の拒否（`buckets` の合計）、`risk_passed_count`、`skip_rate` = `skip_count / (skip_count + risk_passed_count)`、上位 5 件の理由コード（`TopReasons`。`risk_engine_rejected:<コード>` / `meta_skip:<理由>` / `jev_none:regime=<相場>` などの**固定コードだけ**。自由文・数値は出さない）。`jev_none` は `other_reject_breakdown` に regime 別と信頼度帯別で 2 重に出るので、regime 別だけを使い、コードの合計が見送り件数に 1 回ずつ合う。注文案（order proposal）の signal も `risk_rejections` と同じく数に含まれる |
| 研究ラベル | `research_label_day_stats` と `research_label_days`（#1074） | ホライズン（30m / 60m / close）× 側（LONG / SHORT）の、全スナップショットの平均 net bps。記録が済んだ日（`research_label_days` に印のある日）で、**窓の中に丸ごと入る JST の日だけ**（端の欠けた日・記録の途中の日は入れない） |

### 2.2 日クラスタの信頼区間と差の区間（方法）

- 単位は **JST の日**。系列ごとに日の平均（その日のラベルの和 ÷ 件数）を作り、窓の平均は**日平均の平均**（どの日も同じ重み）。
- 窓ごとの 95% 区間: 日平均の平均 ± $t_{0.975}(G-1)\sqrt{v/G}$（$G$ は日数）。$v$ は日平均の系列の **Newey-West の長期分散**（下の「重なるラベルの扱い」）。日が 2 日未満だと区間は `null`（平均だけ）。
- 差（後−前）の区間: 前後は別の期間なので独立な 2 標本として、日平均どうしの **Welch の t 区間**。ただし各窓の「平均の分散」は標本分散 $s^2/G$ ではなく $v/G$（Newey-West）を使い、自由度は Welch-Satterthwaite をその値に当てる（`labelcompare.diffOf`。補正を 0 ラグにすると以前の Welch と同じ式）。どちらかの窓が 2 日未満、または両方の分散が 0 のときは区間 `null`（`diff_bps` は出る）。
- **重なるラベルの扱い（issue #1129。決めたこと）**: 判断のラベルは保有時間が重なるので、同じ日の中の判断どうしも、近い日どうしも互いに独立ではない。日を 1 まとまりにすることで日の中の相関は吸収する。日をまたぐ相関は、日平均の系列に **Newey-West（Bartlett の重み）** を当てて区間に入れる（`labelcompare/newey.go`）。
  - ラグ = ラベルの保有時間の最大を日に切り上げた値 = **1 日**（`NeweyWestLagDays`）。保存済みのラベル（triple barrier）の保有は最大 `barriersettings.MaxTimeoutMinutes` = 60 分、研究ラベルは同じ立会の引けまでで、どれも始まった日の中に収まるので 1 日。`TestNeweyWestLagIsTheLongestHoldInDays` が 60 分との関係を固定する。
  - 長期分散: $v = \frac{n}{n-1}\left(\gamma_0 + 2\sum_{l=1}^{L}\left(1-\frac{l}{L+1}\right)\gamma_l\right)$、$\gamma_l = \frac1n\sum_{t=l}^{n-1}(x_t-\bar x)(x_{t-l}-\bar x)$、$L$ = ラグ。$L=0$ のときは標本分散と一致する。Bartlett の重みなので $v \ge 0$。
  - 「隣」は、ラベルの付いた判断のある次の日（日の並びの隣）。週末や判断のない日は 1 歩に数えない（隣として数える側 = 慎重な側）。
  - `days_needed` の分散にも同じ $v$ を使う（区間と必要日数が同じ前提）。応答には `interval_method`（`newey_west_bartlett`）と `interval_lag_days`（1）を出す。
  - **補正の効き目は日数が少ないと限られる**。日平均が AR(1) で前後に差が無い設定の乱数実験（固定シード・2 万回。`TestNeweyWestFalsePositiveRate` が同じ数を出し、補正が日平均どうしの相関で悪化しないことを固定する）で、差の 95% 区間が 0 を外す割合（名目 0.05）:

    | 日平均の自己相関 ρ | 日数 n | 補正なし（日クラスタ） | Newey-West（ラグ 1） |
    |---|---|---|---|
    | 0 | 8 | 0.049 | 0.072 |
    | 0 | 12 | 0.049 | 0.063 |
    | 0 | 20 | 0.049 | 0.058 |
    | 0.3 | 8 | 0.139 | 0.137 |
    | 0.3 | 12 | 0.146 | 0.128 |
    | 0.3 | 20 | 0.150 | 0.118 |
    | 0.5 | 8 | 0.247 | 0.220 |
    | 0.5 | 12 | 0.255 | 0.207 |
    | 0.5 | 20 | 0.262 | 0.196 |

    つまり: 日が独立ならわずかに広めに誤る（0.072）。相関があると補正は誤りを減らすが、8 日では ρ = 0.3 でほとんど変わらず、名目の 0.05 には届かない（少ない日から自己共分散を推定するため低めに出る）。区間は**補正しても楽観寄りのことがある目安**で、`caution` もそう書く。取引の判断・注文・資金配分には使わない。

### 2.3 あと何日か（`days_needed`）

- 観測済み = 前後それぞれの「ラベルの付いた LONG / SHORT 判断のある JST の日数」の小さい方（`observed_per_side`）。
- `needed_per_side` = 現在の日平均の長期分散 $v$（前後それぞれ。§2.2。補正なしの標本分散ではない）と差を据え置いたとき、前後それぞれ $g$ 日で $t_{0.975}(2g-2)\sqrt{(v_b+v_a)/g} < |\mathrm{diff}|$ になる最小の $g$（2〜`MaxDaysNeeded`=250）。ただし最小日数（§2.4）を下回らない。
- `remaining_days` = $\max(0,\ \text{needed\_per\_side} - \text{observed\_per\_side})$。
- 出せないとき（`needed_per_side` / `remaining_days` は `null`、`reason` に固定コード）: `variance_undefined`（どちらかの窓が 2 日未満で日ごとのばらつきが分からない）/ `diff_near_zero`（差が 0 に等しい。区間は 0 より狭くならない）/ `beyond_horizon`（250 日でも足りない）。このときも `days_to_min`（最小日数まであと何日）は出る。
- 前提: 日ごとのばらつきと差が今のまま続くこと。日が増えると差もばらつきも動くので、**目安**であって予約ではない。

### 2.4 最小日数: 8 日（`MinDaysFloor` = 8、`min_days_source` = `measured_v1`。issue #1129）

前後それぞれ 8 日（ラベルの付いた LONG / SHORT 判断のある JST の日）に満たないと `insufficient_days`（数値は返すが判定しない）。暫定値だった 5 日（`floor_provisional_unapproved`）をやめ、`min_days_source` は `measured_v1` を返す。統計的な検出力の計算ではなく、2〜3 日の偶然で `improved` が出ないための下限で、`days_needed.needed_per_side` がこれより大きければそちらが効く。

**根拠（導出。日ごとのばらつき σ の値に依らない）**: 前後それぞれ $n$ 日のとき、日平均の差の 95% 区間の半幅は $1.96\,\sigma\sqrt{2/n}$。これが日ごとのばらつき $\sigma$ 1 つ分以下になる条件は $n \ge 2\times1.96^2 \approx 7.68$、よって $n = 8$（$\sigma$ が消えるので、$\sigma$ を測らなくても成り立つ）。注意: 1.96 は正規分布の値で、実際の区間は $t(2n-2)$ を使う（$n=8$ で $t(14)=2.145$、半幅は $1.07\,\sigma$）。Newey-West の補正（§2.2）で区間はさらに変わる。8 日は「半幅が $\sigma$ 程度」までで、見分けたい差が $\sigma$ より小さければ `needed_per_side` がもっと長い日数を返す。

**何を測り、何を測れなかったか**:

- 本番の日ごとのラベル平均の標準偏差 $\sigma$（保存済みの研究用ラベル `research_label_day_stats` と `calibration_outcomes` から）: **測れなかった**。この作業環境には本番の DB の写しが無く（作業ツリーと同じ階層・ホームの下に研究用ラベルを持つ DB ファイルは見つからなかった。MCP の読み取り接続もこの環境からは使えない）、有料 API も呼ばない決まりなので、値を作らなかった。8 日はそのまま進める（上の導出は $\sigma$ に依らない）。測るときは、読み取りだけで、ホライズンと側ごとに `sum_net / row_count`（`research_label_day_stats` の日ごとの平均）と、日ごとの `AVG(net_return_bps)`（`calibration_outcomes`、`label_version = 2`、判断が付けた方向）を取り、日数 8 日以上の標本標準偏差と、日をまたぐ 1 次の自己相関 $\hat\rho_1$ をこの節に書く。
- 再現できるフィクスチャ: `experimentlabeltests` のフィクスチャ（`jitter` = ±0.4 bps の 8 日分）の日平均の標準偏差は約 0.29 bps（標本標準偏差、8 日）。これは**テスト用に置いた値であって、本番の測定ではない**ので、8 日の根拠には使わない。
- 自己相関の影響: §2.2 の乱数実験（固定シード）。

### 2.5 `label_verdict`

順に当てる。取引ベースの `verdict` の語彙とは別のフィールドで、`label_judged` は `improved` / `worsened` / `inconclusive` のとき true。

1. `aborted`（中止した実験）/ `confounded`（交絡あり）: `experiment.Compare` が同じ呼び出しで出した交絡・切り替えの結果をそのまま使う（検出を二度走らせない）。判定しない。数値は返す。
2. `insufficient_days`: 前後どちらかが `min_days` 未満。
3. `switch_unconfirmed`: 宣言した変更前→変更後の切り替えが記録で確認できない（`switch_check`）。判定しない。
4. `not_computable`: 日は足りるが差の区間が無い。
5. `improved`（区間の下限が 0 より上）/ `worsened`（上限が 0 より下）/ `inconclusive`（区間が 0 をまたぐ。下限がちょうど 0 も含む）。取引ベースの `mixed`（ドローダウン）に当たるものは無い（ラベルに経路が無い）。

`improved` は「ラベル上は後の方が良かった」であって、取引ベースの `improved` ではなく、採用の基準でもない。固定の注意文 `caution`（日本語）を毎回返す: ラベルは実現損益ではない・保有の重なるラベルは時間的に重なる・前後は別期間で因果の証明にならない・補助の証拠としてだけ使う・設定を変えも戻しもしない。

### 2.6 読み取りの予算（3 秒・1 接続）

- 窓ごとの読みは、判断の件数 1 回（`COUNT`、`jev_decisions` の `(decision_type, timestamp)` 索引）、方向別の件数 1 回、ラベル 1 回（日×方向×ホライズンで集計した 1 文。判断 1 件ごとには読まない）。研究ラベルは前後で `research_label_day_stats` を日の範囲で 1 回ずつ（1 日 6 行。行は読まない）。見送りは `Compare` が読んだ値を使うので追加の読みは無い。
- 上限: 窓の判断が `MaxDecisions`（200,000）超、または窓が `MaxWindowDays`（400 日）超なら、`label_based.error` = `limit_exceeded` を返し、判断の件数・ラベル・研究ラベル・`days_needed` は空にして `label_verdict` を `not_computable` にする（見送りは `Compare` が読んだ値なので残る）。**呼び出しは失敗にしない**（取引ベースの結果は返る）。それ以外の失敗は `error` = `failed`。呼び出しのコンテキストの期限切れ・取り消しだけは、通常どおり呼び出しの失敗（`query_timeout` など）になる。

## 3. `settings_set` に仮説を付けて 1 回で実験を記録する

- 実装: `mcpset.Writer.SetWithExperiment`（`mcpset/setexperiment.go`）。ツール側は `settingstool` の `settings_set` が `labeltool.Set`を呼ぶ。
- 1 トランザクション（`runInTx`。`Set` と同じ接続・同じ `_txlock=immediate`）の中で、順に: (1) ホワイトリスト（`ErrKeyNotAllowed` → `key_not_allowed`。デコード時にも先に検査済み）、(2) 書き込み許可（`ErrWriteDisabled` → `write_disabled`）、(3) 仮説・実験の形の検査（`experiment.Args.Validate`。空・空白だけ・500 文字超・制御文字は `invalid_argument` に理由つき。**設定を書く前**）、(4) 値の検証と保存・監査行（`Writer.Set` と同じ `saveAndAudit`）、(5) 保存後の前後の値が分かったところで実験を検査し（変更前と変更後が同じなら `invalid_argument`＝何も変えていない実験は作れない）、`experiment.Insert`。どれかが失敗すると全部ロールバックされ、**設定・監査行・実験のどれも残らない**（`TestSetWithHypothesisIsAllOrNothing` が、実験の挿入を強制的に失敗させて設定が保存されず監査行も無いことを固定する）。
- 実験の中身: `kind` = `setting`、`name` = キー、`before` / `after` = 保存の前後の**有効な値**（保存した値が無かったときの既定値を含む。監査行の `before` が「保存値なし」で null になるのとは違い、実験には既定値が入る）、`audit_id` = この変更の監査行（`Writer.Set` の `audit_id`）、`started_at` = 書き込みの時刻（応答の `at`）、`actor` = `mcp`、`client` = 呼び出し元。
- 応答: 仮説を渡したときだけ `experiment`（`experiment_create` の応答と同じ形）を足す。仮説を渡さないときの応答のメンバーは従来と同じ（`key` / `unit` / `value` / `before` / `audit_id` / `at`、LLM cutoff のキーは `llm_cutoff_status`）。
- ホワイトリスト・書き込み許可・値の検証は変えていない。仮説が空でも書き込み許可がオフなら `write_disabled` が先に出る。
- 狭い書き込み経路（`settingstests.TestSettingsWritePathIsNarrow`）が許す文は増えない: `runtime_settings` / `mcp_settings_audit` / `tuning_experiments` への既存の INSERT だけ。

## 4. 記録されていない変更の通知（`unrecorded_changes`。`experiment/unrecorded`）

`experiments_list` の応答に足す、読み取りだけのメンバー。直近 14 日（`WindowDays`）の変更のうち、どの実験にも記録されていないものを、新しい順に最大 20 件（`MaxChanges`）。`experiments` と既存のメンバー・並びは変えない。実験を作らず、何も書かない。

| 種類 | 読み元 | 「変更」と数えるもの | 「記録済み」の規則 |
|---|---|---|---|
| `setting` | `mcp_settings_audit`（`actor` が `mcp` / `gui`、`action` が `set` / `reset`） | ホワイトリストのキーで、値が変わった行（同じ値の保存し直し・保存値の無いキーのリセットは変更ではない。`experiment` の `noChange` と同じ） | その行の `audit_id` を持つ実験がある、または**同じキー**の設定の実験で、`started_at` が保存時刻の ±15 分（`experiment.StartTolerance`。切り替えの確認と同じ許容）以内 |
| `model` | `jev_model_changes`（Settings 画面での `JEV_MODEL` の切り替え） | 14 日の中の切り替え | `kind = model` の実験で、`after` がその切り替えの後の値、かつ `started_at` が切り替えの保存の 24 時間前（`JevCoverSlack`）以後（モデルは保存してから本体の再起動で効くので、実験の開始は保存より後になる） |
| `prompt` | `jev_decisions`（`question_version`） | 判断の種類（scout / trader）ごとに、窓より前（なければ窓の最初）に使われていた版から、別の版が**初めて現れた**とき | `kind = prompt` の実験で `after` がその版、かつ上と同じ開始の範囲 |

出力の各要素: `kind` / `name`（設定キー、モデルは `JEV_MODEL`、プロンプトは判断の種類）/ `changed_at`（UTC RFC3339）/ `audit_id`（設定のみ）/ `before` / `after`（秘密のキーは `***`）。モデル・プロンプトは `jev_decisions` からは切り替えの時刻を厳密には言えない（最初に現れた判断の時刻）。モデルの切り替えの記録は migration 000048 以降のみ（それ以前の切り替えは行が無く、「無い」＝「切り替えが無かった」ではない）。

## 5. 隔離

`experimentlabeltests/isolation_test.go`:

- `TestLabelComparisonIsReachedOnlyByTheExperimentTools`: `internal` と `cmd` の非テストのソースを歩き、`experiment/labelcompare`・`experiment/unrecorded`・`SetWithExperiment` を、その 2 パッケージ・`labeltool`・`mcpset/setexperiment.go` 以外が参照したら失敗する（Policy・Risk・発注・実行・Paper ガード・Live・Jev・学習・自己改善が参照できない）。
- `TestLabelComparisonPackagesAreReadOnlyAndTradingFree`: 2 パッケージに書き込み文（`INSERT` / `UPDATE` / `DELETE` / `DROP` / `ALTER` / `CREATE`）が無いことと、import が許可リスト（ドメインの型・`actuals`・MCP の `query` / `experiment`・書き込みのホワイトリスト・`sqlutil`）だけであることを固定する。

これらの値は、取引の判断・注文・資金配分の入力に**ならない**。

## 6. テスト

- `internal/service/paperresultsmcp/experimentlabeltests/`（HTTP レベル。既存の `experimenttests` には足さない）: 取引ゼロで判断とラベルがあるとき `label_based` に件数・平均・区間・あと何日かが出る / 日数が足りないと `insufficient_days` と `remaining_days` / ばらつきに対して差が小さいと下限より多い日数が出る / 交絡のある実験・切り替えが確認できない実験・データが無い実験 / 取引ベースのメンバーが `experiment.Compare` と一致する（メンバー名の固定つき）/ `settings_set` の仮説つきで実験がちょうど 1 件・監査 ID が一致・設定が変わる / 原子性（実験の挿入を失敗させる）/ 無効な仮説・変更なしで何も書かない / 仮説なしは従来どおり / 書き込み許可オフ・ホワイトリスト外 / `unrecorded_changes`（未記録を出し、記録済みを出さない。件数の上限と新しい順。モデル・プロンプト）/ 出力に秘密が無い / 隔離。
- `experiment/labelcompare`（単体）: 日クラスタ（Newey-West）の区間と Welch の手計算、長期分散の手計算（ラグ 0 は標本分散と一致）、相関のある日が区間を広げること、補正の乱数実験（§2.2。固定シード）、日数が足りないときの区間、必要日数の最小性、`days_needed` の理由コード、判定の順、窓の丸ごとの日、見送りの理由コード。

## 変更履歴

| 版 | 日付 | 内容 | 出典 |
|---|---|---|---|
| 1.0 | 2026-11-15 | 新規 | issue #1088 |
| 1.1 | 2026-11-20 | 最小日数を暫定の 5 日から 8 日（`min_days_source` = `measured_v1`）にし、根拠を §2.4 に記載（本番の σ は測れなかったと明記）。重なるラベルの扱いを注意文だけにせず、日平均の系列への Newey-West（ラグ 1 日）の補正で区間・必要日数に入れ（§2.2）、`interval_method` / `interval_lag_days` を応答に足した | issue #1129 |
