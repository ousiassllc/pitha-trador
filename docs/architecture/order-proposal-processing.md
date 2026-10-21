# 設計: 注文案の処理（Risk check → Paper 発注）

`docs/architecture/paper-results-mcp.md` §8 の続き（行数上限のため別ファイル）。MCP が受け付けた Paper の注文案（`order_proposals`）を、アプリが Risk check に通して Paper で発注し、結果・拒否理由・期限切れ・取消を記録する。issue #828（親 #826）。実装は `internal/bootstrap/proposalexec`。

## 1. 役割の分け方

- **MCP は記録するだけ**。書くのは `order_proposals` / `order_proposal_events` の INSERT（`order_proposal_submit`）と、取消の依頼（`order_proposal_cancel`。`order_proposals.cancel_requested_at` の UPDATE 1 つとイベント 1 行。§4）だけで、`paper_orders`・`positions`・`trade_signals`・`kill_switch_events` には書かない。
- **アプリの処理役（`proposalexec.Processor`）が判断と発注を行う**。5 秒ごとに `received` / `waiting_session` / `submitted` の案（古い順）を 1 件ずつ進める。起動時に 1 回だけ、`processing` のまま止まった案を回復する（§5）。
- **Paper だけ**。Live の発注・モード・承認は無い。`order_proposals.mode` は `paper` だけ（`000042` の CHECK）。処理役は Paper の Risk Engine と Execution Engine（Policy の経路と同じ実体）にだけ繋ぐ。

## 2. 状態と理由コード

状態: `received`（MCP が受け付けた）→ `processing`（処理役が取った）→ 終わりは `rejected` / `submitted`（指値が板待ち）→ `filled` / `closed`（`close` が済んだ）/ `expired` / `cancelled` / `failed`。`waiting_session` は立会時間外の待ち（有効期限まで再試行）。状態の変化は `order_proposal_events` に 1 変化 1 行（`actor` は `app`。MCP の取消依頼だけ `mcp`）。`status_reason` とイベントの `reason_code` は固定コードで、自由文は入れない（エラーの詳細はログだけ）。

| コード | どこで | 意味 |
|--------|--------|------|
| `expired` | `expired` | `expires_at` を過ぎた（発注前の案も、板待ちの指値も） |
| `cancel_requested` | `cancelled` | `order_proposal_cancel` の依頼どおりに取り消した |
| `unknown_symbol` / `inactive_symbol` | `rejected` | `instruments` に無い（株以外の指数を含む）／無効な銘柄（`open_long` だけ。`close` は無効な銘柄でも決済できる） |
| `bucket_mismatch` | `rejected` | 案の `bucket` が銘柄の `asset_class` の枠（#824 の `stock` / `bond_etf`）と違う |
| `outside_trading_session` | `waiting_session` | 立会時間外（`open_long` は新規エントリーの時間帯、`close` は取引所が閉まっていない時間帯だけ）。期限内に立会が始まれば進む |
| `stale_quote` | `rejected` | 銘柄の最新の足が無い／古い（Policy・Jev と同じ `SnapshotMaxAge`） |
| Risk の理由（`kill_switch_active` / `system_paused` / `bucket_disabled` / `bucket_budget_exceeded` / `daily_loss_limit_jpy` / `profit_lock` / `max_daily_trades` ほか） | `rejected` | `risk.Engine` の判断そのまま。`trade_signals` にも残す（§3） |
| `size_zero` | `rejected` | 案の数量とアプリが決めた数量（`risk.Engine.PositionSize`）の小さいほうが 0 |
| `max_trade_loss` | `rejected` | 最大損失の見積もりが `max_trade_loss_pct` を超える（§3 手順 9） |
| `quantity_reduced` | イベント（状態は変わらない） | 案の数量よりアプリの数量が小さく、数量を減らして出した。`detail_json` に `requested_quantity` / `applied_quantity` / `limited_by` |
| `position_already_open` / `pending_order_exists` / `symbol_in_cooldown` | `rejected` | Execution の銘柄ごとの拒否（同じ銘柄に保有中・板待ちの注文・損失後のクールダウン） |
| `no_open_position` | `rejected` | `close` なのに保有中のポジションが無い |
| `order_rejected` / `order_cancelled` | `rejected` / `cancelled` | 板待ちの注文が Execution 側で拒否・取消になった |
| `processing_error` | `failed` | 想定外のエラー（原因はログ） |
| `recovered_after_restart` | イベントの `detail_json` | 再起動で `processing` から回復した（§5） |

## 3. 処理の流れ（`open_long`）

1. **期限**: `expires_at` を過ぎていれば `expired`。以降の手順に進まない（Risk も発注もしない）。
2. **取消依頼**: `cancel_requested_at` があれば `cancelled`（`cancel_requested`）。発注前なら何も発注しない。
3. 状態を `processing` にして処理役が取る（`received`／`waiting_session` からの更新は、読んだときの状態を条件にする。別の書き手に負けたら何も書かない）。
4. 銘柄・枠の確認（`unknown_symbol` / `inactive_symbol` / `bucket_mismatch`）。
5. 立会時間外なら `waiting_session`。最新の足が古ければ `stale_quote`。
6. **Risk check**: `risk.Engine.CheckThenEnter`（Policy 経路の Paper エントリーと同じ、ロックの中で判断と発注を一続きにする入口）に `LONG` で通す。通った案の `trade_signals` 行を 1 行書く（`policy_version = 'mcp-proposal-v1'`、`jev_decision_id` は NULL、`risk_passed = 1`）。拒否のときも `risk_passed = 0`・`reject_reason = 'risk_engine_rejected: <Risk の理由>'` で 1 行書き、案を `rejected` にして `trade_signal_id` を結ぶ。Kill Switch・一時停止・日次ガード・枠 0・枠超過はここで拒否され、**Execution は呼ばれない**（`Enter` の呼び出し 0 回）。MCP の `risk_rejections` は Policy の拒否と同じ表を数え、案の分は `proposal_risk_passed_count` / `proposal_risk_rejected_count`（`policy_version` で見分ける）で分かる。
7. Risk が通したときの数量の上限は `risk.Engine.PositionSize`（`CheckThenEnter` が判断した数量。Enter はこの数量を作り直さない）。
8. **数量**: 案の `quantity`（`budget_jpy` のときは予想約定価格で割って単元に切り捨て）と、アプリが決めた数量の**小さいほう**を注文する。減らしたときは `quantity_reduced` のイベントを残す。小さいほうが 0 なら `size_zero`。
9. **最大損失**: 注文数量 ×（予想約定価格 − 実際に効く損切り価格）を `computed_max_loss_jpy` に残す（あいりの `max_loss_jpy` とは別に並べる）。実際に効く損切りは、案の `stop_loss_price` とアプリの損切り（エントリーの `StopLossPct` 下）の**エントリーから遠いほう**で、`effective_stop_loss_price` に残す。`運用資金 × max_trade_loss_pct` を超えれば `max_trade_loss`。
10. **発注**: Execution の `Enter`（成行／指値は案の `order_type`）。銘柄ごとの拒否は上表のコードで `rejected`。
11. 約定したら `filled`（`paper_order_id`・`position_id` を結ぶ）。指値で板待ちなら `submitted`。`submitted` の案は、期限・取消の依頼・注文の最終状態を毎周期見て、期限か取消なら Execution の `CancelPendingEntry`（約定処理と同じロックの中で、`PENDING` のときだけ `CANCELLED` にする）で注文を取り消し、案を `expired` / `cancelled` にする。先に約定していれば取消はせず `filled`（約定が勝つ）。

**`close`**: 手順 4 のあと、Risk check を**通さず**（`trade_signals` も書かない）、保有中ポジションを Execution の `CloseAtMarket` で成行決済する（`exit_reason = 'proposal_close'`。`ExitReasonProposalClose`）。Kill Switch・一時停止・日次ガード中でも決済は実行できる（リスクを減らす操作。手動決済・Kill Switch の強制決済と同じ扱い）。保有が無ければ `no_open_position`。成功で `closed`。

## 4. 取消（`order_proposal_cancel`）

MCP は取消の**依頼を記録するだけ**: `cancel_requested_at` の UPDATE 1 つと、`order_proposal_events` 1 行（状態は変えず、`reason_code = 'cancel_requested'`、`actor = 'mcp'`）を 1 トランザクションで書く。取り消せるのは `received` / `waiting_session` / `submitted` だけで、それ以外（`processing` を含む）は `not_cancellable`（409）。2 回目の依頼は何も書かず最初の依頼を返す（`replayed`）。受け付けスイッチがオフでも使える（リスクを減らす操作）。実際の取消（案の `cancelled`、板待ちの指値の `CANCELLED`）はアプリが次の周期に行い、結果は `order_proposal_get` の `status` で分かる。

## 5. 再起動

`processing` のまま止まった案は、起動時に 1 回だけ回復する。発注の有無は DB から読み、推測しない: `open_long` は、案の `trade_signal_id` を持つ注文が既にあればその結果（約定・取消・拒否・板待ち）を取り、無ければ `received` に戻して最初からやり直す（同じ案で注文が 2 本出ることはない）。`close` は、保有が既に無く `proposal_close` で決済済みなら `closed`、まだ保有中なら `received` に戻す。回復はイベントの `detail_json` に `{"recovered_after_restart":true}` を残す。

## 6. 記録する表（5 表）

1 件の案が、`order_proposals`（状態・理由・紐付け）、`order_proposal_events`（変化の履歴）、`trade_signals`（Risk の判断。`open_long` のみ）、`paper_orders`（注文）、`positions`（約定したポジション）に残る。拒否は前の 3 表まで、期限切れ・取消は前の 2 表（板待ちだった指値は注文も `CANCELLED`）、約定は 5 表すべて。`order_proposals` の追加列は `000043`（ER 図 `er/tables-trading.md`）。

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-10-21 | 新規作成。注文案の処理（Risk check・数量・最大損失・発注・期限・取消・`close`・再起動の回復）と理由コード | issue #828（親 #826） |
