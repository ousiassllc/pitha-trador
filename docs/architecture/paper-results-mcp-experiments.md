# Paper 実績 MCP: 調整の実験の記録と前後比較（issue #871）

`docs/architecture/paper-results-mcp.md` から分割した章。ボットを調整する（設定・Jev のモデル・Jev のプロンプトの版を変える）とき、「何を変えたら、結果がどう変わったか」を**同じ物差し**で比べるための記録と比較。ツールの入出力は `docs/api/endpoints/paper-results-mcp-experiments.md`、表は `docs/architecture/er/tables-system.md`（`tuning_experiments`、マイグレーション `000046`）。Paper だけで、Live の取引・設定には触れない。

## 1. 決定事項（issue #871）

- 調整は 1 つずつ**実験**として記録する。1 つの実験で変えるのは**変数 1 つだけ**。2 つ以上を同時に変える実験は作れない（`experiment_create` と Settings 画面が作成時に弾く）
- 前後の比較は、どの実験でも同じ指標: 取引件数、コスト控除後の純損益、勝率、平均の勝ち・平均の負け、最大ドローダウン、拒否理由の内訳
- **判定の基準は、1 取引あたりの純損益（コスト控除後）の平均の前後差と、最大ドローダウン**。前後の平均を Welch の t 検定で比べ、差（後−前）の 95% 信頼区間を返す。「勝ち（`improved`）」は区間の下限が 0 より上で、かつ最大ドローダウンが前より悪化していないときだけ。合計の損益の大小では判定しない。勝率などは参考として並べるだけ（§4）
- 最低取引件数（前後それぞれ 64 件。サーバー側の下限）に届くまでは判定しない（`insufficient_trades`。数値は返す）。呼ぶ側は上げられるが下げられない。件数は**暫定の案で、あいりの承認を待っている**（§5）
- 実験は変更そのものをしない。設定は `settings_set`（または Settings 画面）、モデルの切り替えとプロンプトの版の切り替えは本体側（コード・設定）で行い、実験はそれが**効き始めた時刻**から比べる

## 2. 変数 1 つ

| `kind` | 変数 | `name` | 交絡の見方 |
|--------|------|--------|-----------|
| `setting` | `mcpwritesettings` のホワイトリストの設定キー | 設定キー（ホワイトリスト外は `key_not_allowed`。交絡を確かめられないため） | `mcp_settings_audit` |
| `model` | Jev のモデル | モデルの呼び名（自由。1〜200 文字） | `jev_decisions.model_id` |
| `prompt` | Jev のプロンプトの版 | 版の呼び名（例 `trader-v3`） | `jev_decisions.question_version` |

検査（`experiment.Args.Validate`）は、変数の数（0 件は `invalid_argument`、2 件以上は `multiple_variables`。同じ変数の 2 回も 2 件と数える）、仮説（1〜500 文字）、`before` ≠ `after`、`audit_id`（`setting` のときだけ。その設定キーの `set` / `reset` の行）、`started_at`（RFC3339。未来は不可）の順。DB 側にも CHECK がある（種類・状態・文字数・`before <> after`・`ended_at` は `running` のときだけ NULL）。

## 3. 前後の取り方（実装者が決めたこと）

- **後** = 実験の開始時刻から終了時刻まで（実行中は現在まで）。半開区間 `[開始, 終了)`
- **前** = 開始時刻の直前の、**後と同じ長さ**の期間 `[開始 − 長さ, 開始)`
- 理由: 長さを揃えれば取引件数・ドローダウン（期間が長いほど大きくなりやすい）が公平に比べられ、「開始前」という定義が曖昧にならない。直前を使うので、相場・銘柄選別などの条件が最も近い。固定の日数にすると、短い実験では前が長すぎ、長い実験では短すぎる
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
4. `not_computable`: 件数は足りるが区間が計算できない（上の縮退）。判定保留
5. 勝ち負け（区間と最大ドローダウンの前後差 `diff.max_drawdown_jpy` で決める。保守的に、勝ちは 1 通りだけ）:
   - `improved`（勝ち）: 区間の下限が 0 より上、**かつ**最大ドローダウンが前以下（`<= 0`）
   - `mixed`: 区間の下限は 0 より上だが、最大ドローダウンが前より大きい（悪化）。勝ちと言わない
   - `worsened`: 区間の上限が 0 より下（1 取引あたりの純損益が下がった）。ドローダウンは問わない
   - `inconclusive`: 区間が 0 をまたぐ（差があるとも無いとも言えない。「効果なし」の証明でもない）

`judged` は 4 つの判定（`improved` / `mixed` / `worsened` / `inconclusive`）のときだけ true。合計の損益の大小は判定に使わない（取引が増えただけで合計は増えるため）。

**交絡**は、実験期間（後の期間）に、対象以外が変わったことをいう:

- ホワイトリストの別の設定キーの `set` / `reset`（`mcp_settings_audit`。GUI の変更も同じ表に入る）。対象の設定キー自身は、実験の `audit_id` の行と、その後に**同じ値**へ保存し直した行は数えず、**別の値**へ変えたら数える
- `jev_decisions` の同じ判断種別（scout / trader）で、`model_id` が 2 種類以上（または `question_version` が 2 種類以上）。ただしモデル（プロンプト）の実験では、その軸だけは前後の 2 つまで許す
- 前の期間は見ない（issue は実験の期間中の変化を検査とした）。前に変更があっても、それは「前」の条件であり実験の対象ではない

モデルの切り替えは監査されない（`jev_decisions.model_id` にだけ残る）ので、判断が 1 件も無い期間のモデルの変更は検出できない。

## 5. 最低取引件数: 暫定の案（あいり未承認）

**これは確定値ではなく、実装者の案である。あいりの承認があるまで承認済みとして扱わない。** 値は `experiment.MinTradesFloor`（1 か所）で、**サーバー側の下限**。`experiments_list` / `experiment_compare` の `min_trades` 引数は、これを**上げるためだけ**に使える。下限未満の指定は、黙って 64 に置き換えず `invalid_value`（422。理由に指定値と下限を書く）で拒否する（置き換えると、呼んだ値と違う前提の判定が返り、誤読のもとになるため）。応答の `min_trades_source` は `floor_provisional_unapproved`（下限の暫定値。まだ承認されていない）か `argument_raised`（呼び出しで下限より上げた）。Settings 画面にも「暫定の案で、まだ承認されていません」と出す。

- **案: 前後それぞれ 64 件（下限）**
- 根拠（仮定つきの統計式）: 前後の 1 取引あたりの純損益の平均を比べる 2 標本の検定（両側 $\alpha=0.05$、検出力 $1-\beta=0.8$）で、差が 1 取引あたり損益の標準偏差 $\sigma$ の半分（効果量 $d=0.5$）のとき、1 期間あたり
  $$n = \frac{2\,(z_{1-\alpha/2}+z_{1-\beta})^2}{d^2} = \frac{2\,(1.960+0.842)^2}{0.5^2} = \frac{15.70}{0.25} = 62.8 \to 63$$
  に、t 分布の補正（約 $z_{1-\alpha/2}^2/4 \approx 1$）を足して **64**
- 効果量を変えたときの件数（同じ式。1 期間あたり）: $d=0.3$ なら 176、$d=0.5$ なら 64、$d=0.8$ なら 26、$d=0.2$ なら 394
- **実測の $\sigma$ は未確認**。この環境にはクローズ済み取引のある Paper の DB が無く（ローカルの開発 DB は `positions` が 0 件）、1 取引あたり損益のばらつきを測れていない。上の式は $\sigma$ を使わず「$\sigma$ の何倍の差を見分けたいか」で件数を決めているため、実測の $\sigma$ で「円でいくらの差が見分けられるか」は $d \times \sigma$ になる。**本番 DB の `realized_pnl` の標準偏差を確認してから、この $d$（と件数）を承認する**（`SELECT COUNT(*), AVG(realized_pnl), SQRT(AVG(realized_pnl*realized_pnl) - AVG(realized_pnl)*AVG(realized_pnl)) FROM positions WHERE closed_at IS NOT NULL AND realized_pnl IS NOT NULL`）
- 限界: 取引は互いに独立でない（同じ日・同じ相場に偏る）ので、実際に必要な件数はこれより多くなりうる。件数が足りても因果の証明にはならない（§3、§4.1）

## 6. 書き込み経路と GUI

- 書き込みは `experiment_create`（`tuning_experiments` への `INSERT` 1 件）と `experiment_close`（`status` と `ended_at` の `UPDATE` 1 件。`running` の行だけ）の 2 つだけで、`mcpset.Writer` の別接続（`docs/architecture/paper-results-mcp.md` §7.3）を通り、**書き込み許可のスイッチ（§7.2）に従う**（オフなら `write_disabled`）。設定・監査・取引の表には触れない（`settingstests.TestSettingsWritePathIsNarrow` が実行された文を数えて固定する）
- `experiments_list` と `experiment_compare` は読み取り接続のままで、`SELECT` だけ（名前付きの列のみ）
- GUI: Settings「外部連携」の MCP カードに実験の節がある（`GET /settings/mcp/experiments`）。一覧（状態・期間・変数・前後の取引件数・判定）、比較（`GET /settings/mcp/experiments/:id`。前後の指標・拒否理由・交絡）、作成（`POST /settings/mcp/experiments`。変数 1 つだけのフォーム）、終了（`POST /settings/mcp/experiments/:id/close`）。画面は人が操作するので、MCP の書き込み許可のスイッチには従わない（Settings の他の保存と同じ）。作成者は `gui`
- 実験の一覧・比較に出る件数の既定は暫定の案（§5）。一覧は新しい順に最大 20 件（実験 1 件ごとに前後の集計 2 回）
