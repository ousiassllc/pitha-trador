# API 仕様: Paper 実績 MCP の注文案ツール

`docs/api/endpoints/paper-results-mcp.md` §3.16〜§3.18 の分割章（行数上限のため別ファイル）。issue #827（親 #826）。共通の上限・トランスポート・成功ボディ `{"tool":"<名前>","data":{...}}` は元の文書、エラーの HTTP と `code` は `docs/architecture/paper-results-mcp-errors.md`、設計は `docs/architecture/paper-results-mcp.md` §8。

## 3. ツール

### 3.16 `order_proposal_submit`

Paper の注文案を 1 件、**受け付けて記録するだけ**（issue #827。親 #826）。発注・Risk check・約定は行わない（アプリが issue #828 で行う）。銘柄の存在・価格・Risk は MCP では見ない。Live には使えない（`mode` の引数は無く、DB の `order_proposals.mode` は `paper` だけ）。`readOnlyHint: false`。仕様は `architecture/paper-results-mcp.md` §8。

引数（未知の引数・`mode` は `invalid_argument`。文字列は制御文字・改行・タブを拒否。数値は有限の正の値だけ）:

| 項目 | 必須 | 内容 |
|------|------|------|
| `idempotency_key` | ○ | 8〜64 文字、`[A-Za-z0-9_-]` だけ。同じ案の再送を見分ける（UNIQUE） |
| `symbol` | ○ | 銘柄コード（`instruments.symbol` と同じ形。英大文字と数字、10 文字以内） |
| `bucket` | ○ | `stock` / `bond_etf`（それ以外は拒否） |
| `action` | ○ | `open_long`（新規の買い）/ `close`（保有中ポジションの決済）。売り建ては無い |
| `quantity` または `budget_jpy` | `open_long` のとき、どちらか 1 つ | 株数（1 以上の整数）か、使う金額（円）。両方・どちらも無しは拒否。`close` は指定不可（保有中ポジション全体の決済） |
| `order_type` | ○ | `MARKET` / `LIMIT` |
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

引数: `proposal_id`（必須、1 以上の整数。`order_proposal_submit` の応答の値）。ない ID は `proposal_not_found`（404）。読み取り専用で、受け付けスイッチがオフでも使える。

`data`: `proposal_id` / `status` / `status_reason`（固定コード。最初は空）/ `received_at` / `updated_at` / `expires_at` / 受け付けた内容（`symbol`・`bucket`・`action`・`quantity`・`budget_jpy`・`order_type`・`limit_price`・`stop_loss_price`・`take_profit_price`・`exit_plan`・`max_loss_jpy`・`rationale`・`sources[]`。ないものは `null`）/ アプリが埋める紐付け `instrument_id`・`trade_signal_id`・`paper_order_id`・`position_id`（最初は `null`）/ `events[]`（古い順。`at`・`from_status`（最初の行だけ `null`）・`to_status`・`reason_code`・`actor`＝`mcp` / `app` / `gui`）。`request_hash`・接続元 IP は返さない。`status` は `received` / `processing` / `waiting_session` / `rejected` / `submitted` / `filled` / `expired` / `cancelled` / `closed` / `failed`（この Issue の範囲で MCP が書くのは `received` だけで、以降はアプリが issue #828 で更新する）。

### 3.18 `order_proposals_disable`

引数なし。注文案の受け付けをオフにする（`order_proposal_submit` が `proposals_disabled` になる）。すでに記録した案には影響しない。すでにオフでも呼べる。切り替えは監査ログ（`mcp_settings_audit`、`action` が `order_proposals_disable`、`key` が `mcp.order_proposals_enabled`）に残る。`data`: `order_proposals_enabled`（`false`）/ `audit_id` / `at`。`readOnlyHint: false`。

**オンにするツールは無い。** 受け付けスイッチ（`runtime_settings` の `mcp.order_proposals_enabled`、1 / 0。行が無い・壊れているときはオフ）をオンにできるのは Settings 画面（「外部連携」→ MCP カード）だけで、`settings_set` で書こうとしても `key_not_allowed` になる。書き込み許可（`settings_write_enable`）とは別の値で、既定が逆（こちらは既定オフ）。

## 5. 受け入れ条件（テストで確認できること）

- 受け付けスイッチがオフ（行なし・`0`・壊れた値）のとき `order_proposal_submit` は `proposals_disabled` でどの表も変わらない。正しい案は `order_proposals` 1 行と `order_proposal_events` 1 行（`received`）が同じトランザクションで入り、片方だけ残らない。同じ `idempotency_key`・同じ内容の再送は行を増やさず同じ `proposal_id` を返し、内容が違えば `idempotency_conflict` で何も書かない。必須項目の欠け・`bucket` / `action` / `order_type` の範囲外・`quantity` と `budget_jpy` の両方・損切りが指値以上・期限が過去または 24 時間より先・文字数超過・制御文字・未知の引数は `invalid_argument`。JST の 1 日 21 件目は `limit_exceeded`。認証なしの 3 ツール呼び出しは SQL 0 件・接続 0 本。実行された更新文は `order_proposals` と `order_proposal_events` の `INSERT`（案 1 件につき各 1 行）と、受け付けスイッチをオフにする `runtime_settings` の upsert と `mcp_settings_audit` の `INSERT` だけで、`paper_orders`・`positions`・`trade_signals`・`kill_switch_events` に触れる文は無い。`order_proposals.mode` に `paper` 以外を入れると DB の CHECK で失敗する。Settings 画面でオンにでき、`order_proposals_disable` でオフにでき、MCP からオンにする手段は無い（`settings_set` は `key_not_allowed`）。設定のホワイトリストは 17 キーのまま

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-10-21 | 新規作成。注文案ツール 3 つの入出力と受け入れ条件（元の文書の版 1.10 で追加） | issue #827（親 #826） |
