# API 仕様: Paper 実績 MCP の注文案ツール

`docs/api/endpoints/paper-results-mcp.md` §3.16〜§3.22 の分割章（行数上限のため別ファイル）。issue #827・#828・#829（親 #826）。共通の上限・トランスポート・成功ボディ `{"tool":"<名前>","data":{...}}` は元の文書、エラーの HTTP と `code` は `docs/architecture/paper-results-mcp-errors.md`、設計は `docs/architecture/paper-results-mcp.md` §8。

## 3. ツール

### 3.16 `order_proposal_submit`

Paper の注文案を 1 件、**受け付けて記録するだけ**（issue #827。親 #826）。発注・Risk check・約定は行わない（アプリの処理役が行う。issue #828、`architecture/order-proposal-processing.md`）。銘柄の存在・価格・Risk は MCP では見ない。Live には使えない（`mode` の引数は無く、DB の `order_proposals.mode` は `paper` だけ）。`readOnlyHint: false`。仕様は `architecture/paper-results-mcp.md` §8。

引数（未知の引数・`mode` は `invalid_argument`。文字列は制御文字・改行・タブを拒否。数値は有限の正の値だけ）:

| 項目 | 必須 | 内容 |
|------|------|------|
| `idempotency_key` | ○ | 8〜64 文字、`[A-Za-z0-9_-]` だけ。同じ案の再送を見分ける（UNIQUE） |
| `symbol` | ○ | 銘柄コード（`instruments.symbol` と同じ形。英大文字と数字、10 文字以内） |
| `bucket` | ○ | `stock` / `bond_etf`（それ以外は拒否） |
| `action` | ○ | `open_long`（新規の買い）/ `close`（保有中ポジションの決済）。売り建ては無い |
| `quantity` または `budget_jpy` | `open_long` のとき、どちらか 1 つ | 株数（1 以上の整数）か、使う金額（円）。両方・どちらも無しは拒否。`close` は指定不可（保有中ポジション全体の決済） |
| `order_type` | ○ | `MARKET` / `LIMIT`。`close` は `MARKET` だけ（成行で決済する。指値の決済は受け付けない） |
| `limit_price` | `LIMIT` のとき | 指値。`MARKET` では指定不可 |
| `stop_loss_price` | `open_long` のとき | 損切りの価格。指値があるときは指値より小さい（買いなので下。成行のときの直近値との比較はアプリが行う）。`close` は指定不可 |
| `take_profit_price` | 任意 | 利確の価格。`close` は指定不可 |
| `exit_plan` | `open_long` のとき | 手じまいの計画（500 文字以内） |
| `max_loss_jpy` | `open_long` のとき | あいりが見込む最大損失（円、0 より大きい）。`close` は指定不可 |
| `rationale` | ○ | 判断の理由（2000 文字以内） |
| `sources` | ○ | 情報源の配列（1〜10 件）。各要素は `title`（200 文字以内）・`url`（http / https）・`retrieved_at`（RFC3339） |
| `expires_at` | ○ | 有効期限（RFC3339）。受け付け時点より後で、24 時間以内 |

処理の順は、認証（失敗は SQL 0 件）→ 引数の形と `expires_at` の範囲（`invalid_argument`。理由は `error.data.detail` に載るが、送った値は引用しない）→ 受け付けスイッチ（`proposals_disabled`）→ JST の 1 日あたり 20 件の上限（`limit_exceeded`。`error.data.detail` に件数）→ 冪等の確認（同じキーで内容が違えば `idempotency_conflict`）→ `INSERT`（`order_proposals` 1 行と `order_proposal_events` 1 行を同じトランザクション）。

`data`: `proposal_id` / `status`（新規は `received`。再送のときは、その案のいまの状態）/ `received_at` / `expires_at`（UTC の RFC3339）/ `replayed`（同じ `idempotency_key` と同じ内容の再送で、新しい行を作らず最初の案を返したとき `true`）。内容が同じかは、`idempotency_key` を除いた引数を正規化（前後の空白・時刻の表記ゆれを吸収）した SHA-256 で決める。

### 3.17 `order_proposal_get`

引数: `proposal_id`（必須、1 以上の整数。`order_proposal_submit` の応答か `order_proposals_list` の値）。ない ID は `proposal_not_found`（404）。読み取り専用（読み取り接続だけ）で、受け付けスイッチがオフでも使える。

`data`: 提案から結果までを 1 件で返す（issue #829）。

| 項目 | 内容 |
|------|------|
| `proposal_id` / `status` / `status_reason`（固定コード。最初は空）/ `received_at` / `updated_at` / `expires_at` | 状態と時刻（UTC の RFC3339）。`status` は §3.20 の固定コード |
| 受け付けた内容 | `symbol`・`bucket`・`action`・`quantity`・`budget_jpy`・`order_type`・`limit_price`・`stop_loss_price`・`take_profit_price`・`exit_plan`・`max_loss_jpy`・`rationale`・`sources[]`（`title`・`url`・`retrieved_at`）。ないものは `null`。自由文は受け付けたときの文字数の上限内でそのまま返す |
| 紐付け | `instrument_id`・`trade_signal_id`・`paper_order_id`・`position_id`（アプリが埋める。最初は `null`）、`cancel_requested_at`（`order_proposal_cancel` の依頼の時刻。なければ `null`） |
| アプリの判断 | `applied_quantity`（アプリが決めた株数。提案とアプリの小さいほう）・`computed_max_loss_jpy`（アプリの最大損失の見積もり）・`effective_stop_loss_price`（その見積もりに使った損切り）。発注前は `null` |
| `result.signal` | 紐づく `trade_signals` の `id`・`at`・`policy_version`・`risk_passed`・`rejected_after_risk`・`reject_code`（固定コード。理由の文は返さない。`rejected_after_risk` は、Risk Engine を通ったがアプリの後段（数量・最大損失・Execution）で拒否された案だけ `true`、Risk Engine 自身の拒否と注文まで進んだ案は `false`。`risk_passed = false` のとき: Risk Engine の拒否なら Risk の理由コード、Risk を通ったが注文にならなかった案（`proposal_rejected_after_risk: <コード>`、issue #860）なら `size_zero` などその理由コード。**`reject_code` だけでは見分けられないので `rejected_after_risk` で見る**。注文まで進んだ案は `risk_passed = true` で `null`）。なければ `null` |
| `result.order` | 紐づく `paper_orders`（`open_long` は買いの注文、`close` は決済の注文）の `id`・`side`・`order_type`・`status`・`quantity`・`submitted_at`・`filled_at`・`filled_price`・`fees_jpy`。なければ `null` |
| `result.position` | 紐づく `positions` の `id`・`status`（`open` / `closed`）・`quantity`・`entry_price`・`opened_at`・`closed_at`・`exit_price`（決済注文の約定価格）・`exit_reason`（`stop_loss` / `take_profit` / `proposal_close` など。§3.3）・`realized_pnl`（`positions.realized_pnl` をそのまま。新しい計算はしない）・`holding_minutes`（`opened_at`〜`closed_at` の分）・`fees_jpy`（エントリーと決済の手数料の和）。決済前は後ろの値が `null`。なければ `null` |
| `result.max_loss` | `proposed_jpy`（あいりの `max_loss_jpy`）・`estimated_jpy`（アプリの見積もり）・`actual_loss_jpy`（決済済みなら `max(0, -realized_pnl)`。利益・引き分けは 0。決済前・ポジションなしは `null`）・`effective_stop_loss_price`・`applied_quantity` |
| `result.stop_loss_reached` / `result.take_profit_reached` | 決済の約定価格が、案の `stop_loss_price` 以下か、`take_profit_price` 以上か。案にその価格がない・決済前は `null` |
| `events[]` | 古い順。`at`・`from_status`（最初の行だけ `null`）・`to_status`・`reason_code`・`actor`（`mcp` / `app` / `gui`）・`detail`（アプリが書いた補足。コードと数値だけの JSON。なければ `null`。例: 数量を減らしたときの `requested_quantity` / `applied_quantity`） |

`request_hash`・接続元 IP は返さない。API キー・`secrets`・板や時価の生データは返さない。

### 3.18 `order_proposals_disable`

引数なし。注文案の受け付けをオフにする（`order_proposal_submit` が `proposals_disabled` になる）。すでに記録した案には影響しない。すでにオフでも呼べる。切り替えは監査ログ（`mcp_settings_audit`、`action` が `order_proposals_disable`、`key` が `mcp.order_proposals_enabled`）に残る。`data`: `order_proposals_enabled`（`false`）/ `audit_id` / `at`。`readOnlyHint: false`。

**オンにするツールは無い。** 受け付けスイッチ（`runtime_settings` の `mcp.order_proposals_enabled`、1 / 0。行が無い・壊れているときはオフ）をオンにできるのは Settings 画面（「外部連携」→ MCP カード）だけで、`settings_set` で書こうとしても `key_not_allowed` になる。書き込み許可（`settings_write_enable`）とは別の値で、既定が逆（こちらは既定オフ）。

### 3.19 `order_proposal_cancel`

注文案の取消を**依頼する**（issue #828）。**依頼を記録するだけ**で、取消そのもの（案の `cancelled`、板待ちの指値の注文の取消）はアプリが次の周期に行う。`readOnlyHint: false`。引数: `proposal_id`（必須、1 以上の整数）。未知の引数は `invalid_argument`。ない ID は `proposal_not_found`（404）。

取り消せる状態は `received` / `waiting_session` / `submitted` だけ。`filled` / `closed` / `rejected` / `expired` / `cancelled` / `failed` と、アプリが処理中の `processing` は `not_cancellable`（409。何も書かない）。書くのは `order_proposals.cancel_requested_at` と、`order_proposal_events` 1 行（`from_status` と `to_status` は取消の依頼を受けたときの状態のまま、`reason_code` は `cancel_requested`、`actor` は `mcp`）を同じトランザクションで。同じ案への 2 回目以降の依頼は何も書かず最初の依頼を返す（`replayed: true`）。受け付けスイッチがオフでも使える（リスクを減らす操作）。`paper_orders`・`positions`・`trade_signals`・`kill_switch_events` には書かない。

`data`: `proposal_id` / `status`（依頼を受けたときの状態。取消が済むまで変わらない。結果は `order_proposal_get`）/ `cancel_requested_at`（UTC の RFC3339）/ `replayed`。

### 3.20 `order_proposals_list`

引数（すべて任意。未知の引数は `invalid_argument`）: `from` / `to`（JST の日付。`received_at` がこの期間の案。両方省略は直近 30 日。期間の規則と上限は元の文書 §1・§2）、`status`（固定コード 10 種のどれか。範囲外は `invalid_argument`）、`bucket`（`stock` / `bond_etf`）、`limit`（既定 50、最大 200）、`cursor`（前回応答の `next_cursor`）。読み取り専用で、受け付けスイッチがオフでも使える。

`data`: `proposals[]`（`received_at` 降順、同着は `proposal_id` 降順）と `next_cursor`（次のページが無ければ `null`）。各行: `proposal_id`・`status`・`status_reason`・`received_at`・`updated_at`・`expires_at`・`symbol`・`bucket`・`action`・`order_type`・`quantity`・`budget_jpy`・`stop_loss_price`・`max_loss_jpy`（あいりの見込み）・`applied_quantity`・`computed_max_loss_jpy`（アプリの見積もり）・`position_id`・`realized_pnl_jpy`（紐づくポジションが決済済みなら `positions.realized_pnl`、それ以外は `null`）。理由・手じまいの計画・情報源・履歴などの詳細は `order_proposal_get`。`status` の固定コードは `received` / `processing` / `waiting_session` / `submitted` / `filled` / `closed` / `rejected` / `expired` / `cancelled` / `failed`（#827・#828 と同じ）。

### 3.21 `order_proposal_summary`

引数: `from` / `to`（任意。§3.20 と同じ期間）。読み取り専用。

`data`: `total_count`（期間に受け付けた案の件数）、`by_status[]`（`code` と `count`。固定コード 10 種を上の順で、0 件も含めて返す）、`by_reject_reason[]`（`status` が `rejected` の案の `status_reason` ごと。件数の多い順、同数はコード順。拒否が無ければ `[]`）、`by_bucket[]`（`stock` / `bond_etf`）、`filled`（約定した買い案＝紐づく `positions` がある `open_long` の案の結果。`proposal_count`・`open_count`＝まだ決済していない件数・`closed_count`・`win_count`・`loss_count`・`win_rate`・`total_pnl_jpy`・`expectancy_jpy`。`win_rate` と `expectancy_jpy` は決済済みが 0 件なら `null`）。損益は `positions.realized_pnl` の合計と件数だけで、新しい計算はしない。`close` の案は、決済するポジションを別の案やルールが開いているため数えない（二重計上を避ける）。

### 3.22 `origin`（`paper_trades` と `paper_performance_summary`）

元の文書の §3.3 と §3.1 に足す、取引の入り口の区別（issue #829）。ポジションのエントリー注文（`positions.entry_order_id`）の `paper_orders.trade_signal_id` から `trade_signals.policy_version` を引いて決める。決済では変わらない（メタモデルが開いたポジションを注文案の `close` で閉じても `meta_model`）。

| `origin` | 条件 |
|----------|------|
| `order_proposal` | `policy_version` が `mcp-proposal-v1`（`orderproposal.PolicyVersion`） |
| `bootstrap` | `policy_version` が `policy-vb`（`domain.PolicyVersionPaperBootstrap`。ブートストラップ、#798）、または `policy-vb+<適用済み提案>` |
| `meta_model` | 上の 2 つ以外のシグナル（`policy-v2` と `policy-v2+<適用済み提案>`） |
| `unknown` | エントリー注文にシグナルが無い（手動の注文など） |

`paper_trades` の各行に `origin` を足す。`paper_performance_summary` は `by_origin[]` を足す（4 種を上の順に常に 1 行ずつ。取引が無い入り口は 0 件。各行: `origin`・`closed_trade_count`・`win_count`・`loss_count`・`win_rate`・`total_pnl_jpy`・`expectancy_jpy`。比率は 0 件なら `null`）。既存の全体の値は変わらない。

## 5. 受け入れ条件（テストで確認できること）

- 受け付けスイッチがオフ（行なし・`0`・壊れた値）のとき `order_proposal_submit` は `proposals_disabled` でどの表も変わらない。正しい案は `order_proposals` 1 行と `order_proposal_events` 1 行（`received`）が同じトランザクションで入り、片方だけ残らない。同じ `idempotency_key`・同じ内容の再送は行を増やさず同じ `proposal_id` を返し、内容が違えば `idempotency_conflict` で何も書かない。必須項目の欠け・`bucket` / `action` / `order_type` の範囲外・`quantity` と `budget_jpy` の両方・損切りが指値以上・期限が過去または 24 時間より先・文字数超過・制御文字・未知の引数は `invalid_argument`。JST の 1 日 21 件目は `limit_exceeded`。認証なしの 3 ツール呼び出しは SQL 0 件・接続 0 本。実行された更新文は `order_proposals` と `order_proposal_events` の `INSERT`（案 1 件につき各 1 行）と、受け付けスイッチをオフにする `runtime_settings` の upsert と `mcp_settings_audit` の `INSERT` だけで、`paper_orders`・`positions`・`trade_signals`・`kill_switch_events` に触れる文は無い。`order_proposals.mode` に `paper` 以外を入れると DB の CHECK で失敗する。Settings 画面でオンにでき、`order_proposals_disable` でオフにでき、MCP からオンにする手段は無い（`settings_set` は `key_not_allowed`）。設定のホワイトリストは 17 キーのまま

- `order_proposal_cancel`（issue #828）は、`received` / `waiting_session` / `submitted` の案に対し `cancel_requested_at` の UPDATE 1 つとイベント 1 行だけを書き、状態は変えず、2 回目の依頼は何も書かない。`filled` などの終わった案と `processing` は `not_cancellable`、ない ID は `proposal_not_found`。受け付けスイッチがオフでも呼べる。実行された更新文は、案の `INSERT`・取消依頼の `UPDATE`（`order_proposals`）とイベントの `INSERT`・スイッチの upsert と監査の `INSERT` だけで、`paper_orders`・`positions`・`trade_signals`・`kill_switch_events` には何も書かない（`proposaltests.TestOrderProposalWritePathIsNarrow`）。

- 読み取りの 3 ツール（issue #829）は、拒否・約定して決済・期限切れ・取消の案を 1 件ずつ入れた DB で、`order_proposals_list`（新しい順・`status`・`bucket`・`limit` / `cursor`）・`order_proposal_get`（提案 → `trade_signals` → `paper_orders` → `positions` の実現損益、最大損失の 3 つの値、損切り・利確に届いたか、イベントの時系列）・`order_proposal_summary`（状態別・拒否理由別・枠別と、約定した案の勝率・期待値・合計損益）が期待どおりの値を返す。`paper_trades` の `origin` は 3 種（と `unknown`）を見分け、`paper_performance_summary` の `by_origin` が入り口ごとに分ける。3 ツールは書き込み接続を開かず、実行した文は `SELECT` だけ（`proposaltests.TestOrderProposalReadToolsUseOnlyReadStatements`）。`health_today` の要対応コード `order_proposal_stalled` は `received` / `processing` が 10 分を超えて止まった案があるときだけ出る（`healthtests`）。

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-10-21 | 新規作成。注文案ツール 3 つの入出力と受け入れ条件（元の文書の版 1.10 で追加） | issue #827（親 #826） |
| 1.1 | 2026-10-21 | §3.19 `order_proposal_cancel`（取消の依頼を記録するだけ）と受け入れ条件を追加。§3.16・§3.17 の「アプリが #828 で」を処理役の仕様への参照に改めた | issue #828（親 #826） |
| 1.2 | 2026-10-21 | §3.17 `order_proposal_get` に結果（signal・order・position・最大損失・損切りと利確に届いたか）とイベントの補足を追加。§3.20 `order_proposals_list`・§3.21 `order_proposal_summary`・§3.22 `origin` を追加。読み取りの 3 ツールの受け入れ条件を追加（ツールは合計 21） | issue #829（親 #826） |
| 1.3 | 2026-10-21 | §3.16 `order_type`: `close` は `MARKET` だけ（処理役は常に成行で決済するので、指値を受け付けて黙って成行にしない） | S-2-0 研磨 |
| 1.4 | 2026-10-21 | §3.17 `result.signal` に `rejected_after_risk`（Risk を通ったが後段で拒否された案だけ true）を追加し、`reject_code` に後段拒否の理由コード（`proposal_rejected_after_risk`）も入ること、`risk_passed` は注文まで進んだ案だけ true であることを明記 | issue #860（親 #826） |
