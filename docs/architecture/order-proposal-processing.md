# 設計: 注文案の処理（Risk check → Paper 発注）

`docs/architecture/paper-results-mcp.md` §8 の続き（行数上限のため別ファイル）。MCP が受け付けた Paper の注文案（`order_proposals`）を、アプリが Risk check に通して Paper で発注し、結果・拒否理由・期限切れ・取消を記録する。issue #828（親 #826）。実装は `internal/bootstrap/proposalexec`。

## 1. 役割の分け方

- **MCP は記録するだけ**。書くのは `order_proposals` / `order_proposal_events` の INSERT（`order_proposal_submit`）と、取消の依頼（`order_proposal_cancel`。`order_proposals.cancel_requested_at` の UPDATE 1 つとイベント 1 行。§4）だけで、`paper_orders`・`positions`・`trade_signals`・`kill_switch_events` には書かない。
- **アプリの処理役（`proposalexec.Processor`）が判断と発注を行う**。5 秒ごとに `received` / `waiting_session` / `submitted` の案（古い順）を 1 件ずつ進める。起動時に 1 回だけ、`processing` のまま止まった案を回復する（§5）。
- **Paper だけ**。Live の発注・モード・承認は無い。`order_proposals.mode` は `paper` だけ（`000042` の CHECK）。処理役は Paper の Risk Engine と Execution Engine（Policy の経路と同じ実体）にだけ繋ぐ。
- **処理役自身も Paper でしか動かない**（issue #858）。配線（`wire.go` の「Live の配線は無い」）に頼らず、処理役がモードを持つ。`proposalexec.New` は `metalabelsettings.RuntimeMode`（`internal/bootstrap/services.go`）を受け取り、`metalabelsettings.ModePaper` 以外（空のゼロ値を含む）では何もしない: 案を読まず、状態を変えず、Risk も Execution も呼ばない。守りは 2 か所で、どちらも `Processor.paperOnly` を見る。
  - `Launch`（`wire.go`）: エラーログ 1 行（`proposalexec: refusing to run outside Paper mode`、`mode=` 付き）を出して戻る。`Recover` もループ（`Run`）も始めず、ゴルーチンも起動しない。
  - `Cycle`・`Recover`・`Run`（`processor.go`・`submitted.go`）: `Launch` を通らずに直接呼んでも、DB を読む前に `ErrNotPaperMode` を返す（`Run` はログを出して戻る）。
  - 固定するテスト: `TestProcessor_RefusesToRunOutsidePaperMode`、`TestLaunch_DoesNotStartOutsidePaperMode`（`mode_test.go`。`live`・空のとき 5 表の行が変わらず、Risk の `CheckThenEnter`・Execution の `Enter` / `CloseAtMarket` / `CancelPendingEntry` の呼び出しが 0 回）。

## 2. 状態と理由コード

状態: `received`（MCP が受け付けた）→ `processing`（処理役が取った）→ 終わりは `rejected` / `submitted`（指値が板待ち）→ `filled` / `closed`（`close` が済んだ）/ `expired` / `cancelled` / `failed`。`waiting_session` は立会時間外の待ち（有効期限まで再試行）。状態の変化は `order_proposal_events` に 1 変化 1 行（`actor` は `app`。MCP の取消依頼だけ `mcp`）。`status_reason` とイベントの `reason_code` は固定コードで、自由文は入れない（エラーの詳細はログだけ）。

| コード | どこで | 意味 |
|--------|--------|------|
| `expired` | `expired` | `expires_at` を過ぎた（発注前の案も、板待ちの指値も） |
| `cancel_requested` | `cancelled` | `order_proposal_cancel` の依頼どおりに取り消した |
| `unknown_symbol` / `inactive_symbol` | `rejected` | `instruments` に無い（株以外の指数を含む）／無効な銘柄（`open_long` だけ。`close` は無効な銘柄でも決済できる） |
| `bucket_mismatch` | `rejected` | 案の `bucket` が銘柄の `asset_class` の枠（#824 の `stock` / `bond_etf`）と違う |
| `outside_trading_session` | `waiting_session` | 立会時間外（`open_long` は新規エントリーの時間帯、`close` は取引所が閉まっていない時間帯だけ）。期限内に立会が始まれば進む。発注の直前に Execution が時間外と判断した場合（時刻の境目）も `rejected` にせずこの状態で待つ |
| `stale_quote` | `rejected` | 銘柄の最新の足が無い／古い（Policy・Jev と同じ `SnapshotMaxAge`） |
| Risk の理由（`kill_switch_active` / `system_paused` / `bucket_disabled` / `bucket_budget_exceeded` / `daily_loss_limit_jpy` / `profit_lock` / `max_daily_trades` ほか） | `rejected` | `risk.Engine` の判断そのまま。`trade_signals` にも残す（§3） |
| `size_zero` | `rejected` | 案の数量とアプリが決めた数量（`risk.Engine.PositionSize`）の小さいほうが 0 |
| `max_trade_loss` | `rejected` | 最大損失の見積もりが `max_trade_loss_pct` を超える（§3 手順 9） |
| `quantity_reduced` | イベント（状態は変わらない） | 案の数量よりアプリの数量が小さく、数量を減らして出した。`detail_json` に `requested_quantity` / `applied_quantity` / `limited_by` |
| `position_already_open` / `pending_order_exists` / `symbol_in_cooldown` | `rejected` | Execution の銘柄ごとの拒否（同じ銘柄に保有中・板待ちの注文・損失後のクールダウン） |
| `no_open_position` | `rejected` | `close` なのに保有中のポジションが無い |
| `position_not_from_proposal` | `rejected` | `close` で、保有中のポジションが注文案で建ったものでない（Policy Engine・ブートストラップが建てたもの、エントリー注文にシグナルが無いもの）。`close` は注文案（`open_long`）で建てたポジションだけ決済する（#859。§3） |
| `stop_loss_not_below_price` | `rejected` | `open_long` の `stop_loss_price` が予想約定価格以上（買いの損切りとして意味がない）。アプリの損切りに置き換えず拒否する（§3 手順 5。#857）。`detail_json` に `latest_price`・`expected_entry_price`・`stop_loss_price` |
| `order_rejected` / `order_cancelled` | `rejected` / `cancelled` | 板待ちの注文が Execution 側で拒否・取消になった |
| `processing_error` | `failed` | 想定外のエラー（原因はログ） |
| `recovered_after_restart` | イベントの `detail_json` | 再起動で `processing` から回復した（§5） |

## 3. 処理の流れ（`open_long`）

1. **期限**: `expires_at` を過ぎていれば `expired`。以降の手順に進まない（Risk も発注もしない）。
2. **取消依頼**: `cancel_requested_at` があれば `cancelled`（`cancel_requested`）。発注前なら何も発注しない。
3. 状態を `processing` にして処理役が取る（`received`／`waiting_session` からの更新は、読んだときの状態を条件にする。別の書き手に負けたら何も書かない）。取ったあとにもう一度案を読み、読んでから取るまでの間に `cancel_requested_at` が入っていれば `cancelled`（`cancel_requested`）にする（`processing` になった案への取消依頼は `not_cancellable` なので、ここで読んだ値が最終）。
4. 銘柄・枠の確認（`unknown_symbol` / `inactive_symbol` / `bucket_mismatch`）。
5. 立会時間外なら `waiting_session`。最新の足が古ければ `stale_quote`。続けて**損切りの向き**を見る: 案の `stop_loss_price` が予想約定価格（`expectedEntryPrice`。成行は最新の足の価格、指値は指値と最新の足の価格の小さいほう）**以上**なら `stop_loss_not_below_price` で `rejected`。この拒否は Risk check の前なので、`trade_signals` を書かず、`CheckThenEnter`・`Enter` を呼ばず、`paper_orders`・`positions` も増えない（`detail_json` に判定に使った `latest_price`・`expected_entry_price`・`stop_loss_price` を残す）。成行買いが対象で、指値買いも同じ式で判定する: 指値が最新の足の価格より**上**なら予想約定価格は最新の足の価格なので、損切りがその価格以上のとき（受け付け時の「損切り ＜ 指値」は通っている）に**同じ拒否がかかる**。指値が最新の足の価格**以下**なら予想約定価格は指値で、受け付け時に損切り ＜ 指値を確かめてあるので**かからない**。
6. **Risk check**: `risk.Engine.CheckThenEnter`（Policy 経路の Paper エントリーと同じ、ロックの中で判断と発注を一続きにする入口）に `LONG` で通す。通った案の `trade_signals` 行を 1 行書く（`policy_version = 'mcp-proposal-v1'`、`jev_decision_id` は NULL、`risk_passed = 1`。この時点では「Risk を通った」だけで、注文になるとは限らない）。拒否のときも `risk_passed = 0`・`reject_reason = 'risk_engine_rejected: <Risk の理由>'` で 1 行書き、案を `rejected` にして `trade_signal_id` を結ぶ。Kill Switch・一時停止・日次ガード・枠 0・枠超過はここで拒否され、**Execution は呼ばれない**（`Enter` の呼び出し 0 回）。MCP の `risk_rejections` は Policy の拒否と同じ表を数え、案の分は `proposal_risk_passed_count` / `proposal_risk_rejected_count`（`policy_version` で見分ける）で分かる。
   **Risk を通ったが注文にならなかった案**（手順 8〜10 の `size_zero` / `max_trade_loss` / `position_already_open` / `pending_order_exists` / `symbol_in_cooldown`、予期しない失敗の `failed`、発注直前の時間外で `waiting_session` に戻った場合、期限切れ・取消、再起動で `received` に戻した場合）は、案の状態を変えるのと**同じトランザクション**で、その `trade_signals` 行を `risk_passed = 0`・`reject_reason = 'proposal_rejected_after_risk: <理由コード>'` に直す（`proposalstore.Repository.Transition`。その行から出た `paper_orders` が無いときだけ。注文が出ていれば、後で取消・拒否になっても `risk_passed = 1` のまま）。これで `risk_passed = 1` は「Risk を通って注文まで進んだ案」だけになる。MCP の `risk_rejections` は、Policy の拒否と同じ表を数え、案の分は `proposal_risk_passed_count` / `proposal_risk_rejected_count`（`policy_version` で見分ける）、後段で拒否された分は `proposal_rejected_after_risk_count` と理由コード別の `proposal_rejected_after_risk_breakdown` に分ける（`risk_passed_count` にも `other_reject_count` にも入れない）。`000045` が、この修正の前に残った行（#851 以降）を同じ形に直し、直す前の値を `trade_signal_rewrites_000045`（`signal_id` / `proposal_id` / `link` / `old_risk_passed` / `old_reject_reason` / `new_reject_reason` / `rewritten_at`）に残す（down はそこから戻して表を消す）。直す行は、案の `trade_signal_id` が指す行（`link = trade_signal_id`）と、**再試行で案から外れて孤立した古い行**（`link = retry_history`。再試行は新しい signal を書いて付け替えるので、古い行は `risk_passed = 1` のまま取り残される。この移行より前のイベントには signal id が無いため、行の時刻の前後 60 秒以内に同じ銘柄の案が `processing`→`waiting_session`（`outside_trading_session`）になった記録が**ちょうど 1 件**のときだけ、その案の行とみなす。0 件・2 件以上の孤立行は推測せず書き換えない）。適用した起動で `migration 000045: rewrote N trade_signals (退避テーブル trade_signal_rewrites_000045)、案に辿れず書き換えなかった孤立 signal M 件` を 1 行ログに出し、次の起動では出さない。実行時は、案が注文にならない状態へ動くたびに `Transition` がその時点で案が指している signal を書き換え、イベントの `detail_json` に `trade_signal_id` を残す（`waiting_session` のあと別の signal で再試行しても、古い signal はそのとき既に `risk_passed = 0` で、どの signal の試行だったか履歴で辿れる。`TestRetry_OldSignalIsNotLeftRiskPassedWithoutAnOrder`）。実行時の書き換えは元の値を退避しない（案の状態と理由コードから元の値は分かるため。退避の対象は過去行だけ）。実行時の書き換え（`Transition`）は退避しない（案の状態と理由コードから元の値は分かるため。過去行だけが退避の対象）。`order_proposal_get` の `result.signal.rejected_after_risk` と注文案の詳細画面（「Risk check: 通過」「後段で拒否: <コード>」）で、後段の拒否を Risk Engine 自身の拒否と見分けられる。
7. Risk が通したときの数量の上限は `risk.Engine.PositionSize`（`CheckThenEnter` が判断した数量。Enter はこの数量を作り直さない）。
8. **数量**: 案の `quantity`（`budget_jpy` のときは予想約定価格で割って単元に切り捨て）と、アプリが決めた数量の**小さいほう**を注文する。減らしたときは `quantity_reduced` のイベントを残す。小さいほうが 0 なら `size_zero`。
9. **最大損失**: 注文数量 ×（予想約定価格 − 実際に効く損切り価格）を `computed_max_loss_jpy` に残す（あいりの `max_loss_jpy` とは別に並べる）。ここまで来た案の `stop_loss_price` は手順 5 で予想約定価格より下と分かっている。実際に効く損切りは、案の `stop_loss_price` とアプリの損切り（エントリーの `StopLossPct` 下）の**エントリーから遠いほう**で、`effective_stop_loss_price` に残す。`運用資金 × max_trade_loss_pct` を超えれば `max_trade_loss`。
10. **発注**: Execution の `Enter`（成行／指値は案の `order_type`）。銘柄ごとの拒否は上表のコードで `rejected`（手順 6 のとおり `trade_signals` 行は `risk_passed = 0` に直る）。
11. 約定したら `filled`（`paper_order_id`・`position_id` を結ぶ）。指値で板待ちなら `submitted`。`submitted` の案は、期限・取消の依頼・注文の最終状態を毎周期見て、期限か取消なら Execution の `CancelPendingEntry`（約定処理と同じロックの中で、`PENDING` のときだけ `CANCELLED` にする）で注文を取り消し、案を `expired` / `cancelled` にする。先に約定していれば取消はせず `filled`（約定が勝つ）。

**`close`**: 手順 4 のあと、Risk check を**通さず**（`trade_signals` も書かない）、保有中ポジションを Execution の `CloseAtMarket` で成行決済する（`exit_reason = 'proposal_close'`。`ExitReasonProposalClose`）。Kill Switch・一時停止・日次ガード中でも決済は実行できる（リスクを減らす操作。手動決済・Kill Switch の強制決済と同じ扱い）。保有が無ければ `no_open_position`。**決済するのは注文案で建てたポジションだけ**（#859）: 手順 4 のあと、保有中ポジションの入り口を DB から確かめ、注文案で建ったものでなければ `position_not_from_proposal` で `rejected`（`CloseAtMarket` は呼ばず、ポジションは `open` のまま、`paper_orders` に決済注文は増えない。Kill Switch・一時停止・日次ガード中でも同じ）。「注文案で建った」は、そのポジションの `entry_order_id` の注文の `trade_signal_id` が `policy_version = 'mcp-proposal-v1'` のシグナル（`paper_trades` の `origin = order_proposal` と同じ定義。`origin.Of` を共有する）、またはそのポジションを `position_id` に持つ `open_long` の案がある、のどちらか。推測はしない。受け付け時（`order_proposal_submit`）には判定しない（処理時の保有で決まるため）。成功で `closed`。

## 4. 取消（`order_proposal_cancel`）

MCP は取消の**依頼を記録するだけ**: `cancel_requested_at` の UPDATE 1 つと、`order_proposal_events` 1 行（状態は変えず、`reason_code = 'cancel_requested'`、`actor = 'mcp'`）を 1 トランザクションで書く。取り消せるのは `received` / `waiting_session` / `submitted` だけで、それ以外（`processing` を含む）は `not_cancellable`（409）。2 回目の依頼は何も書かず最初の依頼を返す（`replayed`）。受け付けスイッチがオフでも使える（リスクを減らす操作）。実際の取消（案の `cancelled`、板待ちの指値の `CANCELLED`）はアプリが次の周期に行い、結果は `order_proposal_get` の `status` で分かる。

## 5. 再起動

`processing` のまま止まった案は、起動時（`Launch`）に 1 回だけ片づけ、**起動直後に `processing` は 0 件**になる。発注の有無は DB から読み、推測しない: `open_long` は、案の `trade_signal_id` を持つ注文が既にあればその結果（約定・取消・拒否・板待ち。期限を過ぎていても注文の結果が優先）を取り、注文が無ければ、`expires_at` を過ぎていれば**直接 `expired`**（`received` に戻さない）、期限内なら `received` に戻して最初からやり直す（同じ案で注文が 2 本出ることはない）。`close` は、保有が既に無く、注文案で建ったポジション（§3 の `close` と同じ判定）が `proposal_close` で決済済みなら `closed`、まだ保有中なら `received` に戻す。Policy Engine やブートストラップのポジションが別の経路で `proposal_close` 決済済みでも、この案は `closed` にしない（`received` に戻り、保有が無いので `no_open_position` で終わる）。回復はイベントの `detail_json` に `{"recovered_after_restart":true}` を残す。1 件の回復の失敗（注文は出ているのに約定のポジションが見つからない等）で他は止まらない: 原因をエラーログ（`proposalexec: proposal failed`）に残し、その案は `failed`（`status_reason = processing_error`）にして `processing` のまま放置しない。回復の結果は `proposalexec: recovery settled proposals left in processing count=… expired=… received=… failed=… still_processing=…` と `bootstrap: recovered order proposals left in processing count=…` のログに出る。テスト: `TestRestart_ExpiredProcessingProposalBecomesExpired`、`TestRestart_RecoveryFailureDoesNotLeaveProcessing`、`TestRestart_LaunchLeavesNothingInProcessing`。

## 6. 記録する表（5 表）

1 件の案が、`order_proposals`（状態・理由・紐付け）、`order_proposal_events`（変化の履歴）、`trade_signals`（Risk の判断。`open_long` のみ。注文にならなかった案の行は `risk_passed = 0`、手順 6）、`paper_orders`（注文）、`positions`（約定したポジション）に残る。拒否は前の 3 表まで、期限切れ・取消は前の 2 表（板待ちだった指値は注文も `CANCELLED`）、約定は 5 表すべて。`order_proposals` の追加列は `000043`（ER 図 `er/tables-trading.md`）。

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-10-21 | 新規作成。注文案の処理（Risk check・数量・最大損失・発注・期限・取消・`close`・再起動の回復）と理由コード | issue #828（親 #826） |
| 1.1 | 2026-10-21 | 取る直前に入った取消依頼を取った直後の再読で拾う（§3 手順 3）。発注直前の時間外は `waiting_session`（§2） | S-2-0 研磨 |
| 1.2 | 2026-10-21 | `close` は注文案で建てたポジションだけ決済し、それ以外は `position_not_from_proposal`（§2 理由コード表・§3 `close`・§5 再起動）。入り口の判定は `paper_trades` の `origin` と同じ定義 | issue #859 |
| 1.3 | 2026-10-21 | `open_long` の損切りが予想約定価格以上なら `stop_loss_not_below_price` で拒否し、アプリの損切りに黙って置き換えない（§2 理由コード表・§3 手順 5・9）。指値買いへのかかり方も書いた | issue #857 |
| 1.4 | 2026-10-21 | 処理役が Paper 以外では動かない守り（§1）。`New` が `Mode` を受け取り、`Launch` は何も起動せず、`Cycle`・`Recover` は DB を読む前にエラーを返す | issue #858 |
| 1.5 | 2026-10-21 | Risk を通ったが注文にならなかった案の `trade_signals` を `risk_passed = 0`・`proposal_rejected_after_risk: <コード>` に直す（§3 手順 6、`000045` で過去行も）。再起動の回復は期限切れの案を直接 `expired` にし、1 件の失敗で他を止めず `failed` にする（§5） | issue #860 |
| 1.6 | 2026-10-22 | 過去行の移行を 000045 にし、直す前の値を `trade_signal_rewrites_000045` に退避、適用時に件数をログに出す（§3 手順 6）。再試行で案から外れた古い signal も `waiting_session` へ動くときに直し、履歴の `detail_json` に `trade_signal_id` を残す。移行は履歴から一意に辿れた孤立行だけ直す。`result.signal.rejected_after_risk` と画面表示で後段の拒否を見分ける | issue #860 |
