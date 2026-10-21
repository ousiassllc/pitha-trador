DROP INDEX positions_entry_order_idx;
DROP INDEX paper_orders_trade_signal_idx;
ALTER TABLE order_proposal_events DROP COLUMN detail_json;
ALTER TABLE order_proposals DROP COLUMN effective_stop_loss_price;
ALTER TABLE order_proposals DROP COLUMN computed_max_loss_jpy;
ALTER TABLE order_proposals DROP COLUMN applied_quantity;
ALTER TABLE order_proposals DROP COLUMN cancel_requested_at;
