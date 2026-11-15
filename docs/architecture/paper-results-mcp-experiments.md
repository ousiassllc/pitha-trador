# Paper 実績 MCP: 調整の実験の記録と前後比較（issue #871）

`docs/architecture/paper-results-mcp.md` から分割した章。ボットを調整する（設定・Jev のモデル・Jev のプロンプトの版を変える）とき、「何を変えたら、結果がどう変わったか」を**同じ物差し**で比べるための記録と比較。ツールの入出力は `docs/api/endpoints/paper-results-mcp-experiments.md`、表は `docs/architecture/er/tables-system.md`（`tuning_experiments`、マイグレーション `000046`）。Paper だけで、Live の取引・設定には触れない。

## 1. 決定事項（issue #871）

- 調整は 1 つずつ**実験**として記録する。1 つの実験で変えるのは**変数 1 つだけ**。2 つ以上を同時に変える実験は作れない（`experiment_create` と Settings 画面が作成時に弾く）
- 前後の比較は、どの実験でも同じ指標: 取引件数、コスト控除後の純損益、勝率、平均の勝ち・平均の負け、最大ドローダウン、拒否理由の内訳
- **判定の基準は、1 取引あたりの純損益（コスト控除後）の平均の前後差と、最大ドローダウン**。前後の平均を Welch の t 検定で比べ、差（後−前）の 95% 信頼区間を返す。「勝ち（`improved`）」は区間の下限が 0 より上で、かつ最大ドローダウンが前より悪化していないときだけ。合計の損益の大小では判定しない。勝率などは参考として並べるだけ（§4）
- 最低取引件数（前後それぞれ 64 件。サーバー側の下限）に届くまでは判定しない（`insufficient_trades`。数値は返す）。呼ぶ側は上げられるが下げられない。件数は**暫定値**（本番の σ は未確認で、測って見直す。あいりの承認も未。§5）
- 判定の前に、**対象の変数が宣言どおり切り替わったか**を記録で確かめる（§4.4。確認できなければ `switch_unconfirmed`）。開始直前の変更・方針の提案・同じ値の保存し直しの扱いは §4.5
- 実験は変更そのものをしない。設定は `settings_set`（または Settings 画面）、モデルの切り替えとプロンプトの版の切り替えは本体側（コード・設定）で行い、実験はそれが**効き始めた時刻**から比べる

## 2. 変数 1 つ

| `kind` | 変数 | `name` | 交絡の見方 |
|--------|------|--------|-----------|
| `setting` | `mcpwritesettings` のホワイトリストの設定キー | 設定キー（ホワイトリスト外は `key_not_allowed`。交絡を確かめられないため） | `mcp_settings_audit` |
| `model` | Jev のモデル | モデルの呼び名（自由。1〜200 文字） | `jev_decisions.model_id` と `jev_model_changes`（Settings 画面での切り替えの記録。§4.3） |
| `prompt` | Jev のプロンプトの版 | 版の呼び名（例 `trader-v3`） | `jev_decisions.question_version` |

検査（`experiment.Args.Validate`）は、変数の数（0 件は `invalid_argument`、2 件以上は `multiple_variables`。同じ変数の 2 回も 2 件と数える）、仮説（1〜500 文字）、`before` ≠ `after`、`audit_id`（`setting` のときだけ。その設定キーの `set` / `reset` の行）、`started_at`（RFC3339。未来は不可）の順。DB 側にも CHECK がある（種類・状態・文字数・`before <> after`・`ended_at` は `running` のときだけ NULL）。

## 3. 前後の取り方（実装者が決めたこと）

- **後** = 実験の開始時刻から終了時刻まで（実行中は現在まで）。半開区間 `[開始, 終了)`
- **前** = 開始時刻の直前の、**後と同じ長さ**の期間 `[開始 − 長さ, 開始)`
- 理由: 長さを揃えれば取引件数・ドローダウン（期間が長いほど大きくなりやすい）が公平に比べられ、「開始前」という定義が曖昧にならない。直前を使うので、相場・銘柄選別などの条件が最も近い。固定の日数にすると、短い実験では前が長すぎ、長い実験では短すぎる
- 交絡（§4）は**前と後の両方の期間**と、その境目で調べる（前の期間に対象以外が変わっていれば、前後は変数以外の条件も違う）
- 欠点（承知のうえ）: 前後は別の期間なので、相場環境の違いは取り除けない。判定は「この期間の結果はこうだった」であって因果の証明ではない。同時期の比較（A/B）は 1 つのボットでは作れない
- 取引はクローズ時刻（`positions.closed_at`）で期間に入れる。件数の上限は `paper_performance_summary` と同じ（5,000 件を超える期間は `limit_exceeded`）

指標は `query.PerformanceSummary`（`paper_performance_summary` の計算）と `query.Rejections`（`risk_rejections` の計算）をそのまま呼ぶ。別の計算はない。したがって `experiment_compare` の `before` / `after` の数値は、同じ期間で `paper_performance_summary` / `risk_rejections` を呼んだ値と一致する（テストが固定する。期間は半開の瞬間 `[from, to)`。`paper_performance_summary` の日付指定は JST の日の境界の瞬間で、実験の期間はその瞬間の指定ができる点だけが違う）。`Performance` には平均の勝ち `avg_win_jpy`（`winSum / win_count`）と平均の負け `avg_loss_jpy`（`lossSum / loss_count`。負の値）を足した（`paper_performance_summary` の応答にも出る）。

## 4. 判定と交絡

### 4.1 検定（`experiment.Welch`）

比べるのは、前後それぞれの**1 取引あたりの純損益（`realized_pnl`、コスト控除後）の平均**。系列は `paper_performance_summary` と**同じ取引集合・同じ値**（`query.PerformanceSummary` が取引ごとの損益の系列も返す）で、件数・平均は応答の `closed_trade_count` / `expectancy_jpy` と一致する（テストが固定）。

Welch の t 検定（等分散を仮定しない）。前の標本平均・不偏分散・件数を $\bar x_1, s_1^2, n_1$、後を $\bar x_2, s_2^2, n_2$ として

$$\Delta=\bar x_2-\bar x_1,\quad SE=\sqrt{\frac{s_1^2}{n_1}+\frac{s_2^2}{n_2}},\quad t=\frac{\Delta}{SE},\quad \nu=\frac{\left(\frac{s_1^2}{n_1}+\frac{s_2^2}{n_2}\right)^2}{\frac{(s_1^2/n_1)^2}{n_1-1}+\frac{(s_2^2/n_2)^2}{n_2-1}}\ (\text{Welch–Satterthwaite})$$

差の 95% 信頼区間は $\Delta \pm t_{\nu,\,0.975}\,SE$（`ci_low` / `ci_high`、`ci_level`=0.95）。t 分布の分位点は `gonum.org/v1/gonum/stat/distuv` の `StudentsT`（非整数の自由度を受け付ける定評のライブラリ。自前の実装は検証が重く誤りの余地があるため使わない）。R の `t.test`（既定が Welch）の結果と一致する固定ケースでテストする（`TestWelchMatchesR`）。

前提と限界（応答の説明にも書く）:

- 取引は互いに独立とみなしているが、実際は同じ日・同じ相場に偏る（独立でない）ので、**区間は実際より狭い（楽観的な）可能性がある**
- 前後は別の期間なので、相場環境の違いは取り除けない。**区間が 0 より上でも、変えたことが原因だという証明にはならない**（§3）
- 損益の分布が重い裾を持つと、平均の t 検定は不安定になる（件数の下限 64 はそれを和らげるだけ）
- 複数の実験を繰り返し見ると偶然の「勝ち」が混ざる（多重比較の補正はしない）

縮退: 前後どちらかが 2 件未満、または前後とも標本分散が 0（全取引が同じ損益で標準誤差が 0）だと区間は計算できず、`ci_low` / `ci_high` / `t` / `df` は `null`（NaN や無限大は返さない）。件数が下限以上のときは `not_computable`（判定保留）。

### 4.2 判定の順（`experiment.verdict`）

1. `aborted`（中止した実験）: 判定しない
2. `confounded`（交絡あり）: 判定しない。`confounders[]` に何が変わったかを返す
3. `insufficient_trades`（件数不足）: 前後どちらかのクローズ済み取引が `min_trades`（下限 64）件に届かない。判定保留（`judged:false`）。数値は返す
4. `switch_unconfirmed`（切り替えを確認できない）: 宣言した変更前→変更後の切り替えが、記録で確認できない（§4.4）。判定しない。数値は返し、`switch_check`（`status` と `detail`）に確認できなかった理由を返す
5. `not_computable`: 件数は足りるが区間が計算できない（上の縮退）。判定保留
6. 勝ち負け（区間と最大ドローダウンの前後差 `diff.max_drawdown_jpy` で決める。保守的に、勝ちは 1 通りだけ）:
   - `improved`（勝ち）: 区間の下限が 0 より上、**かつ**最大ドローダウンが前以下（`<= 0`）
   - `mixed`: 区間の下限は 0 より上だが、最大ドローダウンが前より大きい（悪化）。勝ちと言わない
   - `worsened`: 区間の上限が 0 より下（1 取引あたりの純損益が下がった）。ドローダウンは問わない
   - `inconclusive`: 区間が 0 をまたぐ（差があるとも無いとも言えない。「効果なし」の証明でもない）

`judged` は 4 つの判定（`improved` / `mixed` / `worsened` / `inconclusive`）のときだけ true。合計の損益の大小は判定に使わない（取引が増えただけで合計は増えるため）。

**交絡**は、前の期間（開始の直前の同じ長さ）と後の期間（開始から終了まで）の**どちらかで**、対象以外が変わったことをいう（issue #886。#871 では後の期間だけを見ており、前の期間の途中の変更を見逃して判定が出うる状態だった）。期間ごとに次を調べ、`confounders[]` の各要素の `period`（`before` / `after`）に、どちらの期間で見つけたかを出す。前の要素から先に並ぶ:

- ホワイトリストの別の設定キーの `set` / `reset`（`mcp_settings_audit`。GUI の変更も同じ表に入る。ただしシャドーの 2 キー `jev.shadow.*` の GUI での変更は入らないので、交絡として見つけられない。issue #929）。対象の設定キー自身は、実験の `audit_id` の行と、**同じ値**（実験の `after`）へ保存した行は、前の期間でも後の期間でも数えず（変更は開始の直前に行われ、前の期間に入りうる）、**別の値**へ変えたら数える
- `jev_decisions` の同じ判断種別（scout / trader）で、その期間に `model_id` が 2 種類以上（または `question_version` が 2 種類以上）。ただしモデル（プロンプト）の実験では、その軸だけは期間ごとに 2 つまで許す（切り替えが開始の前後どちらにずれても、自身の変更は交絡にしない）
- Self-Improvement の方針の提案（`policy_proposals`）の適用（`applied_at`）または取り消し（`rolled_back_at`）。適用・取り消しで Policy Engine のしきい値が動き、以後の全シグナルの `policy_version` が `policy-v2` ⇄ `policy-v2+sol-12` のように変わる。実験の変数が方針になることはないので、免除はない。要素は `kind=policy`、`name`＝その提案の版（`applied_policy_version`。無ければ空）、`at`、`values=["applied"]` または `["rolled_back"]`、`period`（`before` / `after`。時刻があるので `boundary` にはならない）。期間より前に適用され、期間中に取り消されていない提案は、前後の両方で効いているので交絡にしない（§4.5）
- Jev のモデル（`JEV_MODEL`）を Settings 画面で切り替えた記録（`jev_model_changes`。§4.3）。判断が 1 件も走っていない期間の切り替えも検出する。記録は時刻（`at`）を持つので、前の期間に保存したものは `period=before`、後の期間に保存したものは `period=after`（`boundary` にはならない）

上の検査は期間ごとなので、前は A だけ・後は B だけという**境目の切り替え**（例: 週末にモデルを替え、そのあと設定の実験をした）は、どちらの期間も 1 つの値しか見ず見逃す。そこで、**実験の変数自身の軸ではない**モデル・プロンプトの軸（モデルの実験ならプロンプトの軸、設定の実験なら両方）は、前の期間と後の期間の値を**合わせて 1 つの集合**として見て、同じ判断種別に 2 種類以上あれば交絡とし、`period` を `boundary` にして `values[]` に両期間の値を出す（期間ごとに報告済みの（軸, 判断種別）は重ねて出さない）。実験の変数自身の軸は、前→後の切り替えが実験そのものなので、この検査をしない。設定は監査の時刻で見るので、境目の変更もどちらかの期間に入って見つかる。

### 4.3 モデルの切り替えの記録（issue #885）

Jev のモデル（Settings 画面の秘密情報 `JEV_MODEL`）を切り替えると、**いつ・何から何へ・誰が**を `jev_model_changes`（マイグレーション `000048`、`er/tables-system.md`）に 1 行残す。書くのは Settings 画面の保存・削除の経路（`system.SecretsRepository.Set` / `Delete`）だけで、`JEV_MODEL` のときだけ、秘密情報の保存と同じトランザクションで書く（どちらかだけ残ることはない）。値が変わらない保存し直し・未設定の削除は行を作らない。前の値・後の値の列に入るのは**モデル名だけ**（NULL は未設定＝既定のモデル）で、API キーなど `JEV_MODEL` 以外の秘密情報の値は、この表の書き込み関数に渡らない（`TestSecretsRepository_JevModelChanges_NeverHoldOtherSecrets`）。`actor` は `gui`（Settings 画面のほかに秘密情報を書く経路はない）。保存してもモデルはアプリの再起動で効くので、判断の `model_id` が変わるのは `at` より後。

- **交絡の検出**: 前の期間と後の期間のそれぞれ `[from, to)` に保存された切り替えを、判断の有無にかかわらず交絡にする（`jev_decisions` に 1 件も無い期間でも検出できる）。要素は `kind=model`、`name=JEV_MODEL`、`period`（`before` / `after`。切り替えには時刻があるので `boundary` にはならない）、`at`（保存した時刻）、`values=[前, 後]`（未設定は「未設定（既定のモデル）」）。モデルの実験では、実験が `after` に挙げたモデルへの切り替えは実験自身の変更なので（変更は開始の直前に保存され、前の期間に入りうるため、どちらの期間でも）数えず、ほかへの切り替えは数える。`name` が `scout` / `trader` の要素は、従来どおり `jev_decisions` の `model_id` の種類から検出したもの
- **モデルの実験の開始時刻の確認**: `experiment_create`（と Settings 画面）で `kind=model` の `started_at` に、記録から「その時刻に使われていたモデル」を求める。`started_at` 以前で最新の切り替えの後の値、そのような切り替えが無ければ**最も古い切り替えの前の値**（記録の開始前から使われていたモデル）。このモデルが `after` でも未知（未設定、または記録が 1 行も無い）でもなく、`after` のモデルへの切り替えが `started_at` より**後**に保存されているときだけ、`invalid_argument` で拒否する（`after` のモデルがまだ使われていなかったはずなので。何も保存しない）。拒否の理由には、その時点で使われていたモデルと、切り替えを保存した時刻を **JST** で出す（Settings 画面の入力が JST のため）。`after` のモデルが使われていた（途中で別のモデルに替わり、また戻った場合も、その時点で `after` なら受け付ける）、モデルが未知、`after` への切り替えが記録に無い、切り替えの保存時刻と `started_at` が同じ、のいずれも受け付ける。以前の「`after` への切り替えの最新の 1 件より前なら拒否」は、B→C→B と戻る記録で B が使われていた時刻を誤って拒否していたため、この規則に改めた
- **記録の開始前は「記録なし」**: 記録は `000048` の適用後の切り替えだけで、過去の切り替えは遡れない。行が無いことは「切り替えが無かった」ことを意味しない。記録が無い `after` のモデルの実験の `started_at` は、従来どおり申告をそのまま信じる。判断が走る前の切り替えを検出できるのは、記録の開始後のものだけ

### 4.4 変数の切り替えの検証（issue #879）

交絡の検出は「対象以外が変わったか」を見る。もう一つ、**対象そのものが、宣言した変更前→変更後のとおりに切り替わったか**を確かめる（#871 では見ておらず、設定が変わっていなくても、`audit_id` を省くと前後の値が検証されないまま判定が出うる状態だった）。結果は応答の `switch_check`（`status` と、値を引用しない固定文の `detail`）で、`confirmed` のときだけ判定する。それ以外は `verdict=switch_unconfirmed`（判定なし。数値は返す）。交絡と違い `confounders[]` には入らず、`confounded` も false のまま。

| `status` | 意味 |
|----------|------|
| `confirmed` | 記録が、宣言した切り替えを、宣言した開始時刻のころに示している |
| `mismatch` | 切り替えの記録はあるが、宣言と違う（値が違う。または保存・使用の時刻が `started_at` から離れていて、前か後の期間の一部が、もう一方の値で動いている） |
| `not_switched` | 記録を見ると、値が変わっていない（同じ値の保存し直し。前後の期間で同じモデル・版が使われている） |
| `unverifiable` | 確認も反証もできる記録が無い（設定の監査ログに該当が無い。前または後の期間に Jev の判断が無い） |

- **設定**（`kind=setting`）: `mcp_settings_audit` の行を読む。`audit_id` があればその行、無ければ、その設定キーの `set` / `reset` のうち**値を変えた**最新の行（`started_at` + 15 分以前に保存されたもの。同じ値の保存し直しは切り替えではないので飛ばす）。その行の変更前・変更後が宣言の `before` / `after` と一致し（数値は数として比べる: `0.5` と `0.50` は同じ）、保存時刻が `started_at` の前後 `experiment.StartTolerance`（15 分）以内であることを求める。NULL の側（保存値なし＝既定が効く）は、宣言の値と照合できないので、その側は照合しない（既定への `reset` の `after`）。**監査に残らない変更は確認できない**: Settings 画面での `jev.shadow.*` の変更は監査に入らない（§4.2、issue #929）ので、その設定の実験は、`audit_id` を渡せないかぎり `unverifiable` になる
- **モデル・プロンプトの版**（`kind=model` / `prompt`）: `jev_decisions` の `model_id` / `question_version` を読む。宣言の `before` が前の期間の判断のどれかに、`after` が後の期間の判断のどれかにあること（判断種別を問わない）。前後とも同じ 1 つの値だけなら `not_switched`、どちらかの期間に判断が 1 件も無ければ `unverifiable`、宣言の値が見つからなければ `mismatch`。さらに、`after` の値が `started_at` − 15 分より前の判断に、`before` の値が `started_at` + 15 分以後の判断に現れたら `mismatch`（前か後の期間が、もう一方の値で動いた時間を含み、前後の比較にならない）。`jev_model_changes`（§4.3）は、保存してから使われるまでに間があり（反映は次の起動）判断の実際の値のほうが確かなので、確認には使わない（交絡の検出には使う）

`started_at` は「変更が効き始めた時刻」で、フォームは分単位、モデルの切り替えは保存から起動までに間がある。そのため、記録との食い違いは 15 分まで許す。**15 分は仕様上の判断**（短くすると GUI の分単位の入力で `mismatch` が出やすく、長くすると前後の期間に、もう一方の値で動いた時間が混ざる）。値は `experiment.StartTolerance` の 1 か所。

- **過去の実験への影響（判定が変わる）**: `audit_id` が無く、`started_at` の前後 `StartTolerance` 以内に監査の行も無い過去の実験は、この確認の導入後は `switch_unconfirmed`（`unverifiable`）として評価される。以前に出ていた判定が変わりうる。終了から `experiment.CacheSettle` 以上たってメモリ（`SummaryCache`、§6）に残った一覧の行は再集計しないので、その行は変わらない（再起動後の最初の一覧で新しい規則になる）
- **限界（承知のうえ）**: `reset` や初回保存の行（変更前または変更後が NULL）は、NULL の側を宣言の値と照合しない。そのため宣言の `before` / `after` がその側で食い違っていても、この確認は見つけられない

### 4.5 開始直前の変更・方針の提案・同じ値の保存し直し（issue #879）

前の期間は開始の直前の、後と同じ長さ（§3）。その中の変更の扱いを決めた:

- **対象以外の変更は、前の期間のどこにあっても交絡**（`period=before`）。開始の 1 分前でも同じ。前の期間に入るので見逃さない（#886 で実装済み。#879 でテストを足した）。逆に、前の期間より前（`[開始 − 長さ, 開始)` の外）の変更は交絡にしない。前後の両方がその変更の後に動いているので、前後の差には効かないから
- **対象自身の変更**は、開始の直前（前の期間の末尾）に保存されるのが普通なので、実験の `audit_id` の行、同じ値（`after`）への保存し直しは交絡にしない（#886）。その保存が `started_at` から 15 分より離れていれば、交絡ではなく**切り替えの食い違い**（`mismatch`、§4.4）になる。つまり「開始直前」の許容は、対象自身については 15 分、対象以外については 0 分（前の期間に入る変更は全て交絡）
- **同じ値での保存し直し**（偽陽性）: `before_value` と `after_value` が同じ（文字列として、または数として）の `set`、保存値が無いキーへの `reset`（NULL → NULL）は、値を変えていないので、対象以外のキーでも**交絡にしない**。保存値なし（NULL）から値への変更、値から NULL への `reset` は、既定の値が分からないので変更として数える
- **`policy_version`**: 方針の提案の適用・取り消しは交絡（§4.2 の `kind=policy`）。`policy_version` は提案の適用で `policy-v2+<版>` になるので、`trade_signals.policy_version` ではなく、時刻を持つ `policy_proposals` の適用・取り消しの行を見る（シグナルが出るまで版は現れないが、行は適用の時刻に残る）。実験の変数が方針になることはない
- **`config/risk.yaml`**（ファイルの編集）: DB に記録が残らず、検出できない。Paper の運用資金などのキーは `mcp_settings_audit` に入るもの（設定画面・MCP 経由）だけが見つかる。実験の期間中は `risk.yaml` を編集しない運用が前提で、判定の説明（応答の説明・本書 §4.1 の限界）に加えて、ここに限界として明記する
- **限界（承知のうえ）**: 取引は `closed_at` で期間に入れるので、開始より前に開いた建玉が後の期間に閉じる。その建玉は前の設定で建てられている。この混ざりは取り除かない（建玉の保有時間が期間に対して短いときに小さい）

## 5. 最低取引件数: 暫定値（本番の σ は未確認）

**64 は暫定値である（ユーザー決定: 値は変えず、暫定と明記する）。本番の 1 取引あたり損益の標準偏差 σ は未確認で、σ を測って見直す予定。あいりの承認も未であり、確定値として扱わない。** 値は `experiment.MinTradesFloor`（1 か所）で、**サーバー側の下限**。`experiments_list` / `experiment_compare` の `min_trades` 引数は、これを**上げるためだけ**に使える。下限未満の指定は、黙って 64 に置き換えず `invalid_value`（422。理由に指定値と下限を書く）で拒否する（置き換えると、呼んだ値と違う前提の判定が返り、誤読のもとになるため）。応答の `min_trades_source` は `floor_provisional_unapproved`（下限の暫定値。値の名前の `unapproved` は、あいりの承認が未であることを指す）か `argument_raised`（呼び出しで下限より上げた）。ツールの説明（`experiments_list` / `experiment_compare` の description と `min_trades` の引数の説明）と Settings 画面の注記にも、同じ「暫定値（本番の 1 取引あたり損益の標準偏差 σ は未確認。σ を測って見直す予定）」を出す（`experiment.MinTradesProvisionalText`、`TestToolDescriptionsStateTheFloorIsProvisional`）。

- **暫定値: 前後それぞれ 64 件（下限）**
- 根拠（仮定つきの統計式）: 前後の 1 取引あたりの純損益の平均を比べる 2 標本の検定（両側 $\alpha=0.05$、検出力 $1-\beta=0.8$）で、差が 1 取引あたり損益の標準偏差 $\sigma$ の半分（効果量 $d=0.5$）のとき、1 期間あたり
  $$n = \frac{2\,(z_{1-\alpha/2}+z_{1-\beta})^2}{d^2} = \frac{2\,(1.960+0.842)^2}{0.5^2} = \frac{15.70}{0.25} = 62.8 \to 63$$
  に、t 分布の補正（約 $z_{1-\alpha/2}^2/4 \approx 1$）を足して **64**
- 効果量を変えたときの件数（同じ式。1 期間あたり）: $d=0.3$ なら 176、$d=0.5$ なら 64、$d=0.8$ なら 26、$d=0.2$ なら 394
- **実測の $\sigma$ は未確認**。この環境にはクローズ済み取引のある Paper の DB が無く（ローカルの開発 DB は `positions` が 0 件）、1 取引あたり損益のばらつきを測れていない。上の式は $\sigma$ を使わず「$\sigma$ の何倍の差を見分けたいか」で件数を決めているため、64 件で円でいくらの差が見えるか（$d \times \sigma$）も**未確認**である（$d=0.5$ は仮定）。**本番 DB の `realized_pnl` の標準偏差を測って、この $d$（と件数）を見直す**（測り方は §5.1、σ から見分けられる差・必要件数の式は §5.2。下の SQL は母集団の標準偏差で、`paper_performance_summary` と同じ集合の不偏標準偏差は §5.1。`SELECT COUNT(*), AVG(realized_pnl), SQRT(AVG(realized_pnl*realized_pnl) - AVG(realized_pnl)*AVG(realized_pnl)) FROM positions WHERE closed_at IS NOT NULL AND realized_pnl IS NOT NULL`）
- 限界: 取引は互いに独立でない（同じ日・同じ相場に偏る）ので、実際に必要な件数はこれより多くなりうる。件数が足りても因果の証明にはならない（§3、§4.1）

### 5.1 本番の σ の測り方（手順。σ はまだ測っていない）

issue #887。σ は本番の Paper DB で測る。**本番 Windows PC の DB の写しが要るため、この環境では測れていない（値は未測定。σ の値をここに書かない）**。測るときは、`paper_performance_summary` / `experiment_compare` が数える取引と**同じ集合・同じ値**で測る: クローズ済み（`closed_at` と `realized_pnl` がともに NULL でない）で、`closed_at` が期間 `[:from, :to)` に入る取引の `realized_pnl`。`realized_pnl` は、`paper_performance_summary` がそのまま集計するコスト控除後の 1 取引あたりの純損益で、別の控除はしない。

```sql
WITH closed AS (
  SELECT realized_pnl AS pnl FROM positions
  WHERE closed_at IS NOT NULL AND realized_pnl IS NOT NULL AND closed_at >= :from AND closed_at < :to),
m AS (SELECT COUNT(*) AS n, AVG(pnl) AS mean FROM closed)
SELECT m.n, m.mean, SQRT(SUM((closed.pnl - m.mean) * (closed.pnl - m.mean)) / (m.n - 1)) AS sd
FROM m LEFT JOIN closed ON 1
```

`:from` / `:to` は保存形式（UTC RFC3339、例 `2026-10-01T00:00:00Z`）。sqlite3 なら `.parameter set :from '2026-10-01T00:00:00Z'` で渡す。`sd` は標準偏差 $\sigma$ で、検定（`experiment.Welch`）と同じ不偏標準偏差（$n-1$）、平均からの差の 2 乗を足す 2 パス（桁落ちに強い）。取引が 0 件でも `n`=0 の 1 行を返す（`mean` と `sd` は NULL。1 件なら `sd` が NULL）。件数が 5,000 を超えても読める（`paper_performance_summary` の件数上限には掛からない）。件数は同じ期間の `closed_trade_count`、平均は `expectancy_jpy` と、`sd` は `experiment_compare` の `per_trade_pnl.*.sd_jpy` と一致する（`TestMeasurementSQLIsThePerformanceSummarysSet` が、本節の SQL をそのまま実行して固定する）。読み取り専用（`SELECT` のみ）で、DB の写しに対して実行する。

同じ日・同じ相場への偏り（自己相関）は、JST の日ごとの集計から見る:

```sql
SELECT date(closed_at, '+9 hours') AS jst_day, COUNT(*) AS n, SUM(realized_pnl) AS sum_jpy, SUM(realized_pnl * realized_pnl) AS sum_sq_jpy
FROM positions
WHERE closed_at IS NOT NULL AND realized_pnl IS NOT NULL AND closed_at >= :from AND closed_at < :to
GROUP BY jst_day ORDER BY jst_day
```

日（クラスタ）ごとの件数 $m_i$・合計・2 乗和から、一元配置の分散分析で級内相関 $\hat\rho = \dfrac{MS_B - MS_W}{MS_B + (m_0 - 1)\,MS_W}$（$m_0$ は 1 日あたりの平均件数に近い値）を求める。

### 5.2 σ から、見分けられる差と必要な件数（式。σ は未測定のまま）

件数は §5 の式を、効果量 $d$（見分けたい差を $\sigma$ で割った値）の関数として使う（Welch の 2 標本、両側 $\alpha=0.05$、検出力 0.8、前後が同じ件数 $n$。$z_{0.975}=1.960$、$z_{0.8}=0.842$）:

$$n(d)=\left\lceil \frac{2\,(z_{0.975}+z_{0.8})^2}{d^2}+\frac{z_{0.975}^2}{4} \right\rceil,\qquad d(n)=\sqrt{\frac{2\,(z_{0.975}+z_{0.8})^2}{n-z_{0.975}^2/4}},\qquad \delta = d\cdot\sigma\ [\text{円}]$$

$\delta$ が、前後それぞれ $n$ 件で見分けられる 1 取引あたりの純損益の差（円）。**測った $\sigma$ を $\delta = d\cdot\sigma$ に掛けて円にする**（表は $\sigma$ に依らない係数）:

| 前後それぞれの件数 $n$ | 64 | 100 | 150 | 200 | 300 | 500 | 1000 |
|---|---|---|---|---|---|---|---|
| 見分けられる差 $d$（$\delta=d\sigma$ の係数） | 0.499 | 0.398 | 0.325 | 0.281 | 0.229 | 0.177 | 0.125 |

| 見分けたい差 $d$ | 0.2 | 0.3 | 0.5 | 0.8 | 1.0 |
|---|---|---|---|---|---|
| 必要な件数 $n$（1 期間あたり） | 394 | 176 | 64 | 26 | 17 |

（表の値は `TestSigmaTablesFollowTheFormula` が式から再計算して固定する。）見分けたい差が $\Delta$ 円なら、$d=\Delta/\sigma$ をそのまま $n(d)$ に入れる。

自己相関（同じ日・同じ相場への偏り）があると、実質の件数が減る。1 日あたり平均 $m$ 件で級内相関が $\rho$ なら、実質の件数は $n/\mathrm{DEFF}$、$\mathrm{DEFF}=1+(m-1)\rho$ なので、**必要な件数は $n(d)$ に $\mathrm{DEFF}$ を掛けた値**になる（区間の信頼度も、独立とみなした §4.1 の区間より低い）。$\rho$ は §5.1 の日ごとの集計から測る。仮に置いた値の目安（$\rho$ は未測定。目安であって推定ではない）:

| $\mathrm{DEFF}$ | $m=3$ | $m=5$ | $m=10$ |
|---|---|---|---|
| $\rho=0.05$ | 1.10 | 1.20 | 1.45 |
| $\rho=0.10$ | 1.20 | 1.40 | 1.90 |
| $\rho=0.20$ | 1.40 | 1.80 | 2.80 |

見直しの手順（本番の DB の写しが手に入ってから）: (1) §5.1 で $n$・平均・$\sigma$・$\rho$ を測る、(2) 見分けたい差 $\Delta$ を決め（人の判断）、$d=\Delta/\sigma$ から $n(d)\times\mathrm{DEFF}$ を求める、(3) その値と 64 を比べ、足りなければ `MinTradesFloor` を上げる（下限未満の拒否とパリティテストはそのまま）、(4) 暫定値の表記（`MinTradesProvisionalText`、`MinTradesSourceFloor`、§1・§5・FR-MCP-35）を更新する。**$\sigma$ が未測定のうちは、64 が足りるかを判断できないため、暫定値の表記は変えない**（issue #887 の残り）。

## 6. 書き込み経路と GUI

- 書き込みは `experiment_create`（`tuning_experiments` への `INSERT` 1 件）と `experiment_close`（`status` と `ended_at` の `UPDATE` 1 件。`running` の行だけ）の 2 つだけで、`mcpset.Writer` の別接続（`docs/architecture/paper-results-mcp.md` §7.3）を通り、**書き込み許可のスイッチ（§7.2）に従う**（オフなら `write_disabled`）。設定・監査・取引の表には触れない（`settingstests.TestSettingsWritePathIsNarrow` が実行された文を数えて固定する）
- `experiments_list` と `experiment_compare` は読み取り接続のままで、`SELECT` だけ（名前付きの列のみ）
- GUI: Settings「外部連携」の別カード「調整の実験（前後比較）」のダイアログ（`modal-mcp-experiments`。MCP のダイアログの外。issue #1079）に実験の節がある（`GET /settings/mcp/experiments`）。実験は ふだん MCP から記録される前提なので、一覧と判定を先に見せ、作成フォームは閉じた `<details>`（「手で記録する」）に入れる。一覧（状態・期間・変数・前後の取引件数・判定）、比較（`GET /settings/mcp/experiments/:id`。前後の指標・拒否理由・交絡）、作成（`POST /settings/mcp/experiments`。変数 1 つだけのフォーム）、終了（`POST /settings/mcp/experiments/:id/close`）。画面は人が操作するので、MCP の書き込み許可のスイッチ（`settings_write_enable` / `settings_write_disable`）には**従わない**（利用者の決定、issue #884）。スイッチが止めるのは MCP 経由の書き込みだけで、GUI は人が直接操作するものだから（注文案の受け付けスイッチと同じ考え方。Settings の他の保存とも同じ）。MCP の `experiment_create` / `experiment_close` は、スイッチがオフなら従来どおり `write_disabled`。この振る舞いを固定するテストと画面の表示は #884 で別に行う。作成者は `gui`
- GUI の画面は設定キーを出さない（issue #904）。作成フォームの「名前」は、設定の実験なら Settings 画面での項目名（MCP から変更できる項目だけを候補に出す。`mcpsettings.settingNames`）を受け取り、保存のときにキーへ直す（`mcpsettings.settingKey`）。キーそのものも従来どおり受け付ける。一覧・比較の「変数」は項目名で示し、拒否理由の内訳はコードではなく日本語名で示す。保存する値と `experiment_compare` が返す中身は変わらない
- 実験の一覧・比較に出る件数の下限は暫定値（§5）。一覧は新しい順に最大 20 件
- 一覧の行は軽く作る（issue #880）。取引件数は SQL の `COUNT`（`query.CountClosed`。`paper_performance_summary` と同じ集合で、5,000 件の上限は掛からない）、交絡・切り替えの確認は `experiment_compare` と同じ検査で、判定が検定なしで決まる行（中止・交絡・切り替え未確認・件数不足。`experiment.withheld`）は取引を 1 件も読まない。取引を読んで検定するのは、これらを通った実験だけで、その判定は `experiment_compare` と同じ。判定を保留する行は取引を読まずに返すので、期間の取引が 5,000 件を超えても保留の判定が出る（そのとき `experiment_compare` 自身は `limit_exceeded` で失敗する）。以前は 1 件ごとに `Compare`（前後の `PerformanceSummary`、拒否理由、交絡）を全部呼んでいた
- **行ごとの失敗**: 1 件の集計に失敗しても一覧全体は失敗せず、その行に `error`（`limit_exceeded`＝前後どちらかの期間のクローズ済み取引が 5,000 件超で検定できない / `failed`＝その他）が付き、`verdict` は空になる（取引件数は数えられた分だけ出る）。他の行はそのまま。画面ではその行の判定欄に理由を出し、比較（`GET /settings/mcp/experiments/:id`）は 422 で理由を返す。キャンセル・タイムアウトは行の失敗にせず、呼び出し全体の失敗のまま（`min_trades` の下限違反も同じ）
- **終了済みの実験は再集計しない**: 終了（または中止）から 1 時間（`experiment.CacheSettle`）以上たった実験の行は、メモリ（`experiment.SummaryCache`。`min_trades` ごと）に残し、次の一覧では取引を読まない。終了した実験の前後の期間は過去の区間で変わらない（ただし期間内の時刻の行が少し遅れて入りうるので、終了から 1 時間は毎回集計する）。失敗した行・実行中の実験は残さない。上限 512 件で、超えたら空にする。**DB には保存しない**: 判定の保存には `tuning_experiments` の列（マイグレーション）と書き込み文の追加が要り、書き込み面を「INSERT 1 件・UPDATE 1 件」に固定している（`settingstests.TestSettingsWritePathIsNarrow`）うえ、MCP の読み取りを読み取り専用に保つ前提とも合わない。再起動後は最初の一覧で 1 回集計し直す（`experiment_compare` は常に最新を計算する）
