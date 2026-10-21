# Paper 実績 MCP: 調整の実験の記録と前後比較（issue #871）

`docs/architecture/paper-results-mcp.md` から分割した章。ボットを調整する（設定・Jev のモデル・Jev のプロンプトの版を変える）とき、「何を変えたら、結果がどう変わったか」を**同じ物差し**で比べるための記録と比較。ツールの入出力は `docs/api/endpoints/paper-results-mcp-experiments.md`、表は `docs/architecture/er/tables-system.md`（`tuning_experiments`、マイグレーション `000047`）。Paper だけで、Live の取引・設定には触れない。

## 1. 決定事項（issue #871）

- 調整は 1 つずつ**実験**として記録する。1 つの実験で変えるのは**変数 1 つだけ**。2 つ以上を同時に変える実験は作れない（`experiment_create` と Settings 画面が作成時に弾く）
- 前後の比較は、どの実験でも同じ指標: 取引件数、コスト控除後の純損益、勝率、平均の勝ち・平均の負け、最大ドローダウン、拒否理由の内訳
- **判定の基準は、コスト控除後の純損益と最大ドローダウンだけ**。勝率などは参考として並べるだけで、判定には使わない
- 最低取引件数に届くまでは判定しない（`insufficient_trades`。数値は返す）。件数は**暫定の案で、あいりの承認を待っている**（§5）
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

判定の順（`experiment.verdict`）:

1. `aborted`（中止した実験）: 判定しない
2. `confounded`（交絡あり）: 判定しない。`confounders[]` に何が変わったかを返す
3. `insufficient_trades`: 前後どちらかのクローズ済み取引が `min_trades` 件に届かない。数値は返す
4. `improved`: 純損益が前より高く、最大ドローダウンが前より大きくない（`<=`）
5. `worsened`: 純損益が前より低く、最大ドローダウンが前より小さくない（`>=`）
6. `mixed`: 上のどちらでもない（純損益とドローダウンが逆を向く、または同値）。勝ち負けを言わない

`judged` は 3 つの判定（`improved` / `worsened` / `mixed`）のときだけ true。

**交絡**は、実験期間（後の期間）に、対象以外が変わったことをいう:

- ホワイトリストの別の設定キーの `set` / `reset`（`mcp_settings_audit`。GUI の変更も同じ表に入る）。対象の設定キー自身は、実験の `audit_id` の行と、その後に**同じ値**へ保存し直した行は数えず、**別の値**へ変えたら数える
- `jev_decisions` の同じ判断種別（scout / trader）で、`model_id` が 2 種類以上（または `question_version` が 2 種類以上）。ただしモデル（プロンプト）の実験では、その軸だけは前後の 2 つまで許す
- 前の期間は見ない（issue は実験の期間中の変化を検査とした）。前に変更があっても、それは「前」の条件であり実験の対象ではない

モデルの切り替えは監査されない（`jev_decisions.model_id` にだけ残る）ので、判断が 1 件も無い期間のモデルの変更は検出できない。

## 5. 最低取引件数: 暫定の案（あいり未承認）

**これは確定値ではなく、実装者の案である。あいりの承認があるまで承認済みとして扱わない。** 値は `experiment.DefaultMinTrades`（1 か所）にあり、`experiments_list` / `experiment_compare` の `min_trades` 引数で呼び出しごとに上書きできる。応答の `min_trades_source` が `default_provisional_unapproved` のあいだは暫定の既定が効いている。Settings 画面にも「暫定の案で、まだ承認されていません」と出す。

- **案: 前後それぞれ 64 件**
- 根拠（仮定つきの統計式）: 前後の 1 取引あたりの純損益の平均を比べる 2 標本の検定（両側 $\alpha=0.05$、検出力 $1-\beta=0.8$）で、差が 1 取引あたり損益の標準偏差 $\sigma$ の半分（効果量 $d=0.5$）のとき、1 期間あたり
  $$n = \frac{2\,(z_{1-\alpha/2}+z_{1-\beta})^2}{d^2} = \frac{2\,(1.960+0.842)^2}{0.5^2} = \frac{15.70}{0.25} = 62.8 \to 63$$
  に、t 分布の補正（約 $z_{1-\alpha/2}^2/4 \approx 1$）を足して **64**
- 効果量を変えたときの件数（同じ式。1 期間あたり）: $d=0.3$ なら 176、$d=0.5$ なら 64、$d=0.8$ なら 26、$d=0.2$ なら 394
- **実測の $\sigma$ は未確認**。この環境にはクローズ済み取引のある Paper の DB が無く（ローカルの開発 DB は `positions` が 0 件）、1 取引あたり損益のばらつきを測れていない。上の式は $\sigma$ を使わず「$\sigma$ の何倍の差を見分けたいか」で件数を決めているため、実測の $\sigma$ で「円でいくらの差が見分けられるか」は $d \times \sigma$ になる。**本番 DB の `realized_pnl` の標準偏差を確認してから、この $d$（と件数）を承認する**（`SELECT COUNT(*), AVG(realized_pnl), SQRT(AVG(realized_pnl*realized_pnl) - AVG(realized_pnl)*AVG(realized_pnl)) FROM positions WHERE closed_at IS NOT NULL AND realized_pnl IS NOT NULL`）
- 限界: 取引は互いに独立でない（同じ日・同じ相場に偏る）ので、実際に必要な件数はこれより多くなりうる。件数が足りても因果の証明にはならない（§3）

## 6. 書き込み経路と GUI

- 書き込みは `experiment_create`（`tuning_experiments` への `INSERT` 1 件）と `experiment_close`（`status` と `ended_at` の `UPDATE` 1 件。`running` の行だけ）の 2 つだけで、`mcpset.Writer` の別接続（`docs/architecture/paper-results-mcp.md` §7.3）を通り、**書き込み許可のスイッチ（§7.2）に従う**（オフなら `write_disabled`）。設定・監査・取引の表には触れない（`settingstests.TestSettingsWritePathIsNarrow` が実行された文を数えて固定する）
- `experiments_list` と `experiment_compare` は読み取り接続のままで、`SELECT` だけ（名前付きの列のみ）
- GUI: Settings「外部連携」の MCP カードに実験の節がある（`GET /settings/mcp/experiments`）。一覧（状態・期間・変数・前後の取引件数・判定）、比較（`GET /settings/mcp/experiments/:id`。前後の指標・拒否理由・交絡）、作成（`POST /settings/mcp/experiments`。変数 1 つだけのフォーム）、終了（`POST /settings/mcp/experiments/:id/close`）。画面は人が操作するので、MCP の書き込み許可のスイッチには従わない（Settings の他の保存と同じ）。作成者は `gui`
- 実験の一覧・比較に出る件数の既定は暫定の案（§5）。一覧は新しい順に最大 20 件（実験 1 件ごとに前後の集計 2 回）
