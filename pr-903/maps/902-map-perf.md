# #902 対応表: perf（成績・予測の精度）

画面: A=`/performance`（実績タブ）、B=`/performance?tab=backtest`、C=`/calibration`。ヘルプのID/ラベルは「ラベル=○○の説明」。

## A 実績（`organisms/performance_actuals_panel.templ`・`evaluation/panel.templ`・`handler/performance/evaluation_view.go`）

| 画面 | 元の文 | 区分 | 変更後 | 理由 |
|---|---|---|---|---|
| A | 「クローズ済みポジションの実績。シャープレシオ・ソルティノレシオは1取引あたりの参考値（年率換算なし）。」（performance_actuals_panel.templ:43、見出し下の常時表示文） | ヘルプに移した | 見出し「実績（Paper）」の横 `help-performance-actuals`: クローズ済みポジションの実績です。シャープレシオ・ソルティノレシオは1取引あたりの参考値で、年率換算はしていません。 | 項目の説明 |
| A | 各指標名の `title="Total PnL"` 等（11項目: Total PnL / Daily PnL / Trades / Win Rate / Profit Factor / Expectancy / Max Drawdown / Average Hold Time / Sharpe ratio / Sortino ratio / Signal count） | ヘルプに移した | 各ラベルの横に `help-performance-actuals-<key>`。本文は「英語名。日本語の説明」（例: 勝率=「Win Rate。利益が出た取引の割合です。」、プロフィットファクター=「…総利益÷総損失（円ベース）…負けた取引がないときは「—」」、シャープ/ソルティノ=年率換算しない参考値と「—」になる条件 など） | `title=` の説明はヘルプへ（英語名は本文の先頭に残す） |
| A | 実績の取得失敗 `実績の取得に失敗しました。`（handler/performance/performance.go:110、`data-testid="actuals-error"`） | 残した | 変更なし | エラー |
| A | 実績評価の見出し下: 「損益は手数料控除後の実現損益（円）。[ ] は95%信頼区間（同じ日の取引は相関するため日単位でまとめて評価）。サンプルが基準未満の指標は「判定不能」。」（panel.templ:16） | ヘルプに移した | 見出し「実績評価（勝てているか）」の横 `help-evaluation`: 同内容を丁寧語にし、表頭の略語（EV=1取引あたりの平均損益、PF=プロフィットファクター、最大DD）の意味を追記 | 項目の説明 |
| A | 初期資金の行: 「初期資金 ¥500,000（Paper では仮置きの値で、口座残高ではない（config/risk.yaml の initial_capital））」（evaluation_view.go:24） | 残した＋ヘルプに移した | 画面: 「初期資金 ¥500,000（Paper では仮置きの値）」。横の `help-evaluation-capital`: 「Paper の初期資金は仮置きの値で、口座残高ではありません。資金比の最大ドローダウンの基準になります。設定キー: initial_capital」。yamlパス `config/risk.yaml` は消した | 仮置きの警告は短くして残す／yamlパスは画面から消し、キー名はヘルプ内へ |
| A | 初期資金未設定: 「初期資金 未設定（資金比の最大ドローダウンは判定不能）」（evaluation_view.go:30） | 残した | 変更なし（注記の括弧は出さない） | 設定の警告 |
| A | 合格ライン本文: 「合格ライン: 取引のあった日が20日以上・取引100件以上・EV下側信頼限界が0.00 bps超・最大ドローダウンが初期資金の5.00%以下。指標はn≥… かつ …日以上にまたがるときだけ判定する。」（criteriaText、evaluation_view.go:133、verdict下の常時表示文） | ヘルプに移した | 「合格ライン: 判定不能」の横 `help-evaluation-verdict` の本文（先頭の「合格ライン: 」を除いた同文） | 判定基準の説明。各項目の 実績/基準/判定 は画面に残した（`evaluation-check-*`） |
| A | 「Live前チェックの材料であり、判定結果で Live が許可・拒否されることはない（#55 は人が判断する）。」（panel.templ:49） | ヘルプに移した（Issue番号は消した） | `help-evaluation-verdict` 内: 「Live前チェックの材料であり、判定結果で Live が許可・拒否されることはありません。」 | 開発メモ（#55）を消し、制約の説明はヘルプへ |
| A | 合格ラインの判定（合格/不合格/判定不能）と各チェック行 | 残した | 変更なし | 判定結果・警告 |
| A | 取得失敗 `実績評価の取得に失敗しました。`（performance.go:117、`evaluation-error`） | 残した | 変更なし | エラー |
| A | コスト内訳のラベル「グロス（手数料前）」「スリッページ（約定価格に含まれる）」「グロス（コスト前）」（evaluation_view.go:40-42） | ヘルプに移した | ラベルは「グロス（手数料前）」「スリッページ」「グロス（コスト前）」。括弧の説明は各ラベル横のヘルプへ（`help-evaluation-cost-gross-before-fees` / `-slippage` / `-gross-before-costs`: 「約定価格の時点での損益（純損益＋手数料）…」「スプレッドや呼値の丸めを含む、約定価格に含まれたコスト（円）です…」「手数料とスリッページの両方を取り戻した損益…」） | 項目の説明 |
| A | 表の見出し `title="n"`（件数）（panel.templ:80,120） | 消した | — | 略号だけのツールチップ。説明は `help-evaluation` に集約 |
| A | 評価表の判定不能セルの `title="参考値 50.0%（n=6、区間 [10.0%–90.0%]）: サンプルまたは取引日数が基準未満のため判定不能"`／`n=…`（evaluation_view.go:163-171）、メタモデルの `title="参考値…（OOS n=…）"`・`OOS n=…`・`OOSでは定義できない（NULL）`、見出し `title="OOS n"` | ヘルプに移した（一部消した） | 判定不能セルは「判定不能」の横に `help-evaluation-<表>-<行>-<指標>`（参考値と基準未満の理由）。信頼できる値のセルの `title="n=…"`と見出しの `title="OOS n"` は消した（件数は列で見える）。「NULL」の語は消した | `title=` の説明はヘルプへ／内部語を消す |
| A | 表の見出し・区分名「exit_reason別」「exit_reason」「メタモデル版別（metalabel_models.version）」（evaluation_view.go:51-52） | 消した（日本語化） | 「決済理由別」「決済理由」「メタモデル版別」 | 内部のカラム名・変数名の表示 |
| A | 「label_version 1（終値のみ）」「label_version 2（トリプルバリア・コスト控除後）」「label_version N」（labelVersionText） | 消した（日本語化） | 「ラベル版 1（終値のみ）」「ラベル版 2（トリプルバリア・コスト控除後）」「ラベル版 N」 | 内部名の表示 |
| A | Jev見出し「Jevの方向的中率とBrier（ラベル版別・同値除く）」と空表示「ラベル付き判断がまだありません。」 | ヘルプに移した／残した | 見出し「Jevの方向的中率とBrier」＋ `help-evaluation-jev`（ラベル版ごとの集計。同値は除く。Brierは小さいほど良い）。空は「ラベル付き判断はありません。」（開発メモ的な「まだ」を除いた） | 項目の説明／状態文 |
| A | メタモデル見出し「メタモデル: OOS指標と同じ取引・同じ期間のPaper実績EV」と空表示「学習済みのメタモデルがありません。」 | ヘルプに移した／残した | 見出し「メタモデルのOOS指標とPaper実績EV」＋ `help-evaluation-meta`（OOSと実績EVを並べて比べる表。差＝実現EV−予測EV）。空は「学習済みのメタモデルはありません。」 | 項目の説明／状態文 |

## B バックテスト（`organisms/backtest_form.templ`・`performance_summary_panel.templ`・`backtest_fold_table.templ`・`handler/performance/performance.go`）

| 画面 | 元の文 | 区分 | 変更後 | 理由 |
|---|---|---|---|---|
| B | フォーム項目「学習日数」「検証日数」「Forward 日数」に説明なし | ヘルプに移した（新設） | 各項目名の横（`<label>` の外・兄弟）に `help-backtest-training-days` / `-validation-days` / `-forward-days`: 学習期間／検証期間／Forward期間（学習に使っていない、成績として評価する期間。この日数ずつずらしてフォールドを繰り返す）の長さ | 項目の説明。ラベルは `<label for>` に変更（入力は `id=backtest-*`） |
| B | 合算表の見出し「Walk Forward（Forward 期間を合算）」 | ヘルプに移した（新設） | `help-backtest-summary`: 全フォールドのForward期間の成績を合算した値。損益はリターン（%）の合計で金額ではない | 項目の説明 |
| B | 合算表の各指標の `title="Trades"`…`title="Net PnL"`（11項目） | ヘルプに移した | 各ラベル横に `help-backtest-summary-<項目>`（英語名＋説明。Profit Factor は負けなしのとき「∞」） | `title=` の説明はヘルプへ |
| B | フォールド表の見出し `title="Trades"` 等5つ | ヘルプに移した | 見出し「フォールド別」の横に `help-backtest-folds`（意味は合算表と同じ）。各列の `title` は消した | 説明の重複を1か所に |
| B | 入力エラー: 「forward_days は 1〜366 の整数で指定してください。」（training_days/validation_days も同形）、「from は YYYY-MM-DD 形式の日付で指定してください。」「to は …」「to は from 以降の日付で…」「from〜to の期間は最大 N 日までです」「Training/Validation/Forward を 5+2+1 日で1フォールド…短すぎます」「forward_days を長くするか…」（performance.go:173-213） | 残した（内部名を日本語ラベルへ） | 「Forward 日数は 1〜366 の整数で指定してください。」「学習日数は…」「検証日数は…」「開始日は YYYY-MM-DD 形式の日付で指定してください。」「終了日は…」「終了日は開始日以降の日付で指定してください。」「開始日〜終了日の期間は最大 N 日までです」「学習・検証・Forward を 5+2+1 日で…」「Forward 日数を長くするか…」 | 入力値のエラー。画面専用（JSON API は別経路）。変数名/クエリ名の露出を消した |
| B | バックテスト失敗・タイムアウト（`バックテストの実行に失敗しました。`、`…秒でタイムアウトしました。期間を短くしてください。`） | 残した | 変更なし | エラー |

## C 予測の精度（`pages/calibration_page.templ`・`static/.../calibration-heatmap/**`・`exit-alignment/**`）

| 画面 | 元の文 | 区分 | 変更後 | 理由 |
|---|---|---|---|---|
| C | 集計ホライズンの行末「（5/10/15分の合算。旧ホライズンのラベルは除外）」「実現PnL（trades / JPY）はホライズンに依存しません」（horizon-selector.ts） | ヘルプに移した | 「集計ホライズン: 全体」の横 `help-calibration-horizon`: 全体は5・10・15分の合算（旧ホライズンのラベルは除外）。実現PnLはホライズンに依存しません | 注記・制約の説明 |
| C | 「サンプルなし（ラベル付き判断がまだありません）」 | 残した（短縮） | 「サンプルなし（ラベル付き判断がありません）」 | サンプル不足の表示。「まだ」の開発メモ調を除いた |
| C | 「トリプルバリアのラベルなし（EV・到達率は未集計）」 | 残した（短縮） | 「トリプルバリアのラベルなし」 | サンプル不足の表示 |
| C | トリプルバリア集計の項目名「EV（net, 1判断あたり）」 | ヘルプに移した | 「EV（コスト控除後）」＋ `help-calibration-barrier-ev`: 1判断あたりの平均リターン（net: 取引コスト控除後、bps） | 項目の説明 |
| C | 指標 Brier Score / Log Loss / Expected Calibration Error / PnL Brier Score / PnL Log Loss（説明なし） | ヘルプに移した（新設） | 各項目名の横に `help-calibration-brier` / `-log-loss` / `-ece` / `-pnl-brier` / `-pnl-log-loss`（意味と「小さいほど良い」） | 指標の説明 |
| C | メタモデル: 「メタラベリングモデルは未学習です（ラベルが貯まると夜間に学習されます）」 | 残した＋ヘルプに移した | 「メタラベリングモデルは未学習です」＋ `help-calibration-metamodel-none`: ラベルが貯まると夜間に学習されます | 状態は残し、補足はヘルプ |
| C | 「Jev 評価期間（OOS）」（説明なし）／「OOS EV（net, エントリーした判断あたり）」 | ヘルプに移した | 「Jev 評価期間（OOS）」＋ `help-calibration-metamodel-period`（学習に使っていない期間。LLM学習データ終了日より前は含まない）／「OOS EV（net）」＋ `help-calibration-metamodel-ev`（エントリーした判断1件あたりの平均リターン、コスト控除後） | 項目の説明 |
| C | OOS EV の判定の末尾「足切りであり、安全の証明ではありません。数字は仮定つきで、リターンの保証ではありません。」（meta-model-adoption-view.ts） | ヘルプに移した | 判定は「正のエッジ（信頼区間の下限が 0 を超過）」等の1文だけ画面に残し、注意書きは `help-calibration-metamodel-ev-verdict` へ | 長い注意書き。判定結果（判定不能・データ無し 等）は残した |
| C | 採用基準の末尾「（クラスタ・ブートストラップ N 回）。足切りであり、安全の証明ではありません」 | ヘルプに移した | 基準本体（OOS件数・take件数・営業日数・EV下限・AUC下限）は残し、ブートストラップ回数と注意書きは `help-calibration-metamodel-criteria` へ | 長い注意書き |
| C | 「前向き評価（採用判断に不使用）」 | ヘルプに移した | 「前向き評価」＋ `help-calibration-metamodel-forward`: 採用した後（学習後）の判断で測った成績。採用判断には使いません | 制約の説明。「— 損失が続いています」の警告は残した |
| C | LLM cutoff の警告 `calibration-metamodel-cutoff-warning`（API の `warning`）、不採用の理由ラベル（「LLM 学習データの終わりが仮置きのまま」等）、出所の表示 | 残した | 変更なし | 設定の警告（仮置き等）。画面で隠さない |
| C | 「ラベル基準と実出口の整合（#774）」見出し | 消した（番号）＋ヘルプに移した | 「ラベル基準と実出口の整合」＋ `help-exit-alignment`: 学習ラベルが前提にする出口（利確・損切り・タイムアウト・コスト）と実際の取引の出口が合っているかを確かめる表。合っていないとモデルの勝ち負けの幅が実際の取引とずれる | 開発メモ（#774）を消し、説明をヘルプへ |
| C | 整合の警告 `exit-alignment-warnings`（`role=alert`）と「ラベル基準と実出口は許容範囲内で一致しています。」 | 残した | 変更なし（質問参照） | 警告・状態 |
| C | 取得エラー（`pitha-calibration-heatmap` / `pitha-exit-alignment` の `role=alert`） | 残した | 変更なし | エラー |

## 質問

1. **整合の警告文に内部キー名が出る（変更せず）**: `internal/service/exitalign/table.go:115` の `WarnCost` の文に「barrier の基底コスト（calibration.barrier_cost_bps）が既定から外れています。…」が入っています。この文字列は画面（`pitha-exit-alignment`）と `GET /api/v1/calibration/exit-alignment` の `warnings[].message`（API/MCP が返す中身）の両方で使われているので、API 側は変えない方針に従い触っていません。画面だけキー名を除く（画面専用の文に分ける／キー名を後ろに回す）設計にするか、そのままにするか、決めてください。
2. **LLM cutoff の警告文 `warning`・メタモデルの `gate.detail`・`comparison.note` も同様**: API が返す日本語文をそのまま画面に出しています（`internal/web/handler/calibration` の出力）。キー名や設定名が含まれる場合があるか未確認のため触っていません。含める場合は上と同じ判断が必要です。
3. **決済理由の生コード**: `exit-alignment-view.ts` の「再生の決済理由」「Paper の決済理由」は API の `reason` をそのまま表示（`take_profit` など）しています。日本語ラベルにする対応表が TS にありません。足してよいか、API の値のまま見せてよいか決めてください。
4. **ヒートマップ・方向別表の英語見出し**（Direction / Samples / Accuracy / Avg return、セル内の `conf 0.xx` `trades` `win` `EV … TP SL TO`）は説明文ではなく見出し・値の書式なので変えていません。日本語化まで含めるなら別途指示をください。
5. **performance 担当外の既存仕様書**: `docs/requirements/functional/performance-evaluation.md:71` は節名など中身が変わっていない記述なので触っていません（画面の文言を引用しているのは `docs/components/lit.md` の3か所のみ直しました）。
