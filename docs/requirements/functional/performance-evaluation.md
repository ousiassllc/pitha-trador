# 機能要件: 実績評価と合格ライン（§5.3 Performance 続き）

`functional.md` §5.3 から分割した節（issue #777）。「Paperで勝てているか」を、コスト込み・期間別・サンプル数付き・OOSと同じ土俵で判断するための集計と、その合格ライン、検証済みSQLを定める。**Live移行（#55）の可否は人が決める。ここの合否はその材料であり、取引・設定・Live切替のどこにも接続しない。**

## 1. 範囲と制約

- 集計は**読み取り専用**で、既存テーブル（`positions`・`paper_orders`・`trade_signals`・`jev_decisions`・`calibration_outcomes`・`metalabel_models`・`metalabel_predictions`）から**表示のたびに計算する**。新しい永続化・テーブル・マイグレーションはない。取引・設定テーブルへ書き込まない。
- 板・時価などの市場データの生値（`market_snapshots`、`jev_decisions.state_json`、`metalabel_predictions.features_json`）は読まず、出力しない。出力は集計値だけ（kabuのデータ取扱い規約の方針、#778と矛盾しない）。
- 既存の「実績（Paper）」節（Total/Daily PnL・Max Drawdown＝各取引の損益率の累積曲線 等、`GET /api/v1/performance`）は変更しない。本節の指標は**その下に別の節として**追加する。

## 2. 指標（FR-EVAL-1）

対象は、`closed_at`と`realized_pnl`が非NULLの`positions`（クローズ済みPaper建玉）。`realized_pnl`は両約定の手数料控除後（円）で、約定価格にスリッページ・スプレッド・呼値丸めが含まれる（`internal/service/fillmodel`）。

| 指標 | 定義 |
|------|------|
| 損益（円）・取引件数 n | `realized_pnl`の合計と件数 |
| 勝率 | `realized_pnl > 0`の割合 |
| EV（円/取引） | `realized_pnl`の平均 |
| EV（bps/取引） | `realized_pnl ÷ (entry_price × quantity) × 10,000`の平均（取引ごとのネットリターン） |
| Profit Factor | 総利益 ÷ 総損失（円）。損失が無いと定義できず「判定不能」 |
| 最大ドローダウン（円） | 累積損益（開始時を0）の山からの最大下落。最初の取引が損失ならその損失も下落に数える |
| 最大ドローダウン（資金比） | 上の円 ÷ `initial_capital` × 100（%）。**`initial_capital`と併記する。** |
| Jev方向的中率・Brier | 方向付きのtrader判断（LONG/SHORT）の`calibration_outcomes`ごとの的中（`was_direction_correct`）と、確信度との二乗誤差。**`label_version`別に分けて出し、混ぜない**（1=終値のみ、2=トリプルバリアで「コスト控除後に利益」）。リターンがちょうど0の同値は的中にも外れにも数えない |
| メタモデルOOS | `metalabel_models`のOOS AUC・OOS EV（bps）・OOS件数・OOS期間。**同じモデルの予測が付いた判断で実際に入った取引の実現EV（bps）と予測EV（`metalabel_predictions.expected_value_bps`）・その差（実現−予測）**、および**OOS期間内にクローズした全取引のPaper EV**を並べる。1判断に複数モデルの予測があるときは最新（最大id）のモデルに紐付ける |

`initial_capital`は`config/risk.yaml`の`paper.initial_capital`（既定3,000万円）。Paperでは**仮置きの値で口座残高ではない**。0以下（未設定）なら資金比ドローダウンは「判定不能」と表示する。

### 切り口

全期間に加えて、日別（JST）・週別（月曜始まりのISO週、例`2026-W42`）・方向別（`LONG`/`SHORT`）・`exit_reason`別・メタモデル版別（`metalabel_models.version`。予測の無い取引は「(なし)」）・A/Bアーム別（`jev_decisions.ab_arm`。無い取引は「(なし)」）。

### コスト内訳（FR-EVAL-2）

グロスとネットを分けて出す。`ネット = 実現損益`、`グロス（手数料前）= ネット + 手数料`、`グロス（コスト前）= グロス（手数料前）+ スリッページ`。手数料は`paper_orders.fees`（両約定の合計）。スリッページは約定価格に既に含まれており、`paper_orders.slippage_bps`（基準価格より何bp不利だったか。負は有利）から、約定価格と数量で円換算する（買い約定は`価格 = 基準×(1+b)`、売り約定は`価格 = 基準×(1−b)`。LONGは買いで入り売りで出る、SHORTは逆）。`slippage_bps`が記録されていない約定は0円として数え、記録のある約定数を併記する。

## 3. サンプル数と信頼区間（FR-EVAL-3）

各指標は**サンプル数n**と**95%信頼区間**を持つ。

- 平均（勝率・EV円・EVbps・Jev的中率・Brier・予測EV・EV差）: **日単位のクラスターに頑健な区間**。同じ日の取引は市場の方向を共有して独立でないため、日ごとの偏差の合計を1単位として標準誤差を求め、`t(日数−1)`の両側95%臨界値で区間にする。勝率・的中率は[0,1]に切る。日数が1日だけのときは区間が作れない。
- Profit Factor・最大ドローダウン（円・資金比）: **日ブロックのブートストラップ**（日を単位に復元抽出、1,000回、固定シードで再現可能、パーセンタイル95%区間）。ドローダウンは経路依存なので日の並びを保つ。

**「判定不能」**: 次のいずれかのとき、値は参考表示（ツールチップ）にとどめ、画面・JSONとも判定不能として扱う。

| 条件 | 既定 |
|------|------|
| サンプルnが基準未満 | n < **30** |
| 観測が基準日数未満の日にまたがる | 取引のあった日 < **5** |
| 値が定義できない／区間が作れない | 例: 損失なしのProfit Factor、1日だけ |

境界はちょうど基準値で「判定可」（n=29は判定不能、n=30は判定可、4日は不能、5日は可）。メタモデルのOOS指標は`oos_samples`が30未満なら判定不能。

## 4. 合格ライン（FR-EVAL-4）

「勝てているか」を次の4項目で判定する。全項目を満たすと**合格**。

| 項目 | 既定の基準 | 判定 |
|------|-----------|------|
| 期間 | 取引のあった日数 ≥ **20日**（約1か月） | 満たさなければ判定不能 |
| 取引件数 | クローズ済み ≥ **100件** | 満たさなければ判定不能 |
| EVの下側信頼限界 | EV（bps/取引）の95%区間の下限 > **0 bps** | 判定可能（上2項目を満たし、EVが判定可）なときだけ。下限が0以下なら不合格、満たさなければ判定不能 |
| 最大ドローダウン上限 | 資金比 ≤ **5%**（Paperの`max_daily_loss_pct`1%の5日分） | 基準超過は**件数によらず不合格**（ドローダウンは取引が増えるほど悪化するだけ）。基準内は、期間・件数を満たすときだけ合格、満たさなければ判定不能。`initial_capital`未設定は判定不能 |

全体の判定は、1項目でも不合格なら**不合格**、そうでなく1項目でも判定不能なら**判定不能**、全項目合格で**合格**。

基準値は`internal/service/actuals.DefaultCriteria()`の既定値で、実行時設定ではない（Settingsからは変更できず、自己改善ループの対象外）。変更はコードとこの表の同時改訂で行う。根拠の目安: 30件・5日は平均の正規近似と日クラスターの自由度の最低ライン、100件・20日は数bpsのEVを標準偏差数十bpsの1取引リターンから区別するための最低量、ドローダウン5%は日次損失上限の5日連続分。**実データで妥当性を見て人が見直す前提の暫定値**であり、Liveの許可条件を示すものではない。

## 5. 画面とAPI

- Performance画面（`GET /performance`）の「実績（Paper）」節の下に「実績評価（勝てているか）」節を表示する: 合格ラインの判定と各項目の実績／基準、`initial_capital`（仮置きの注記つき）、コスト内訳、全期間と各切り口の表（n・損益・勝率・EV円・EVbps・PF・最大DD円・最大DD資金比。各指標は`値 [95%区間]`または「判定不能」）、Jevの的中率・Brier（ラベル版別）、メタモデルのOOSと実績EVの並列表。取得失敗時は節内に固定文言を出して500（原因は表示しない）。
- `GET /api/v1/performance/evaluation`（`api/endpoints/huma-api-insights.md`）が同じ集計をJSONで返す（`from`/`to`で`closed_at`の期間を絞れる）。上記の指標はすべて`n`・`lo`/`hi`・`reliable`を持つ。

## 6. 検証済みSQL（FR-EVAL-5）

`tools/verify-queries.sql`は、画面と同じ指標を`sqlite3`で独立に再計算するクエリ集（Q0〜Q10、`-- name: Qn`で区切り）。実スキーマ（`db/migrations` 000004・6・7・8・11・33・34・35・36）に対して検証済みで、次を満たす。

- `calibration_outcomes`は`label_version`で絞る／分ける（Q5〜Q7）。固定の「コスト0.1%」仮定ではなく、実際の約定コスト（`paper_orders.fees`・`slippage_bps`、Q4）とコスト控除後ラベル（`net_return_bps`、Q6）を使う。
- 実取引と予測の結合: `positions.entry_order_id → paper_orders.trade_signal_id → trade_signals.jev_decision_id → jev_decisions / metalabel_predictions`（Q2・Q8）。入力の古さは`jev_decisions.data_age_ms`（Q7）。
- 約定スリッページの分布（Q10、コスト想定の見直し基準の入力。`components-pipeline.md` FR-COST-1「見直し」）: `paper_orders.slippage_bps`を、`filled_at`のJST時間帯（`open`＝前場・後場の寄りの最初の1分（9:00台・12:30台の最初の1分）、`close`＝15:25以降のクロージング・オークション、`regular`＝それ以外のザラ場。境界は`marketcalendar.Calendar.PhaseAt`と同じ）×建玉の売買方向（`positions.side`）に分け、約定件数`fills`・平均`mean_slippage_bps`・95パーセンタイル`p95_slippage_bps`（最近順位法: 昇順で`ceil(0.95×n)`番目）を出す。対象は`FILLED`で`filled_at`と`slippage_bps`が記録された約定（NULLは数えない）。想定値（2bps／5bps）や1.5倍・3倍のしきい値はクエリに埋め込まず、生の分布だけを出す（判定は運用者が出力と`cost.*`の設定値を突き合わせて行う）。合成DBで時間帯の境界・件数・平均・p95を手計算と照合する（`internal/repository/actuals/verifysql_test.go`）
- 円÷円の比はSQLiteでは整数除算になる（NUMERIC列は整数値だと整数で保存される）ため、bpsは先に`10000.0`を掛けてから割る（合成DBでの検証で実際に見つかった不具合の修正）。
- `state_json`の特徴量（`return_5m`等）は市場データ由来のため読まない。

実行（アプリ停止中のDBまたはそのコピーに、読み取り専用で）:

```sh
sqlite3 -readonly -header -column pitha.db ".read tools/verify-queries.sql"
```

期間は、`params`CTE（`from_ts`/`to_ts`、UTC、既定は全期間）を持つQ1〜Q5・Q10の先頭の値を書き換える（Q1〜Q4は`positions.closed_at`、Q5は`jev_decisions.timestamp`、Q10は`paper_orders.filled_at`で絞る）。`params`を持たないQ0（データの在庫）・Q6・Q7・Q8・Q9は全期間が対象で、期間を絞るときは`WHERE`に次を足す: Q6・Q7は`d.timestamp >= '…' AND d.timestamp < '…'`（Q5と同じ列）、Q8の`linked`は`p.closed_at >= '…' AND p.closed_at < '…'`（Q9はモデルごとのOOS窓`oos_start`〜`oos_end`で決まり、期間の指定は受け付けない）。`initial_capital`はQ3の`params`を書き換える。`go test ./internal/repository/actuals/`が、空DBと合成DB（`internal/repository/actuals/actualstest`）で全クエリを実行し（構文・列名の検証、読み取り専用接続でも実行）、合成DBでは手計算の期待値（件数・勝率・EV・円建て最大DD・コスト・Brier・メタモデル比較）と照合する。画面側の集計（`internal/service/actuals`）も同じ合成DBの同じ期待値でテストしている。

## 7. 実データの実行記録

リポジトリには実績の数値を置かない（アプリが動くPCのSQLiteにのみ溜まる）。運用者はPaper稼働分のDBバックアップに上のSQLを実行し、**期間・稼働日数・クローズ済み取引件数**を明記して、次をissue #777のコメントに記録する。

- 稼働日数・取引件数（Q0）、損益合計・勝率・EV・最大DD（円・資金比。Q1・Q3）、コスト内訳（Q4）
- Jevの方向的中率とBrier（Q5）、メタモデルの有効モデル数・OOS AUC・OOS EVと、同期間のPaper実績EV（Q8・Q9）
- 合格ラインの判定（画面または`GET /api/v1/performance/evaluation`）。サンプルが基準未満なら「判定不能」のまま記録する。

生の板・時価・`raw_data_json`・DBダンプは貼らない。
