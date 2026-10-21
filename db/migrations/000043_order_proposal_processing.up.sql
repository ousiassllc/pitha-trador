-- 注文案の処理（issue #828、親 #826）: アプリが Risk check → Paper 発注まで進める間に記録するものを足す。
-- 状態の列・紐付け列（instrument_id / trade_signal_id / paper_order_id / position_id）は 000042 にある。ここで足すのは次の 5 つ。
--
-- order_proposals
--   cancel_requested_at         order_proposal_cancel（MCP）が取消を依頼した時刻（UTC）。NULL は依頼なし。MCP がこの列と
--                               イベント 1 行だけを書き、実際の取消（注文の CANCELLED と状態の cancelled）はアプリが行う。
--   applied_quantity            アプリが実際に注文した（open_long）／決済した（close）株数。案の数量とアプリが決めた数量
--                               （risk.Engine の PositionSize）の小さいほう。まだ決めていなければ NULL。
--   computed_max_loss_jpy       アプリが計算した最大損失（円）＝ 数量 ×（約定見込み価格 − 実際に効く損切り価格）。
--                               あいりの max_loss_jpy と並べて残す。まだ計算していなければ NULL。
--   effective_stop_loss_price   上の計算に使った損切り価格＝案の損切りとアプリの損切りのうちエントリーから遠い方。
--
-- order_proposal_events
--   detail_json                 イベントの補足（固定キーの JSON オブジェクト。無いときは空文字）。数量を減らしたときの
--                               {"requested_quantity":…,"applied_quantity":…} など。数値・コードだけで自由文は入れない。
ALTER TABLE order_proposals ADD COLUMN cancel_requested_at TEXT;
ALTER TABLE order_proposals ADD COLUMN applied_quantity INTEGER
    CHECK (applied_quantity IS NULL OR applied_quantity > 0);
ALTER TABLE order_proposals ADD COLUMN computed_max_loss_jpy NUMERIC
    CHECK (computed_max_loss_jpy IS NULL OR computed_max_loss_jpy >= 0);
ALTER TABLE order_proposals ADD COLUMN effective_stop_loss_price NUMERIC
    CHECK (effective_stop_loss_price IS NULL OR effective_stop_loss_price > 0);

ALTER TABLE order_proposal_events ADD COLUMN detail_json TEXT NOT NULL DEFAULT '';

-- 再起動時の回復（processing のまま止まった案が出した注文を探す）と、注文が約定したあとの position の紐付けで引く。
CREATE INDEX paper_orders_trade_signal_idx ON paper_orders (trade_signal_id);
CREATE INDEX positions_entry_order_idx ON positions (entry_order_id);
