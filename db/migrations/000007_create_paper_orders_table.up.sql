-- paper_orders: Paper Trading（将来は実発注）の注文・約定
-- （docs/architecture/er.md §paper_orders）
CREATE TABLE paper_orders (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    instrument_id INTEGER NOT NULL REFERENCES instruments (id),
    trade_signal_id INTEGER REFERENCES trade_signals (id),
    symbol VARCHAR(10) NOT NULL,
    side VARCHAR(10) NOT NULL
        CHECK (side IN ('BUY', 'SELL')),
    order_type VARCHAR(10) NOT NULL
        CHECK (order_type IN ('MARKET', 'LIMIT')),
    quantity INTEGER NOT NULL,
    limit_price NUMERIC(12, 2),
    status VARCHAR(20) NOT NULL
        CHECK (status IN ('PENDING', 'FILLED', 'CANCELLED', 'REJECTED')),
    submitted_at TEXT NOT NULL,
    filled_at TEXT,
    filled_price NUMERIC(12, 2),
    fees NUMERIC(10, 2) NOT NULL DEFAULT 0,
    slippage_bps NUMERIC(8, 2),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX paper_orders_instrument_submitted_idx
    ON paper_orders (instrument_id, submitted_at DESC);
CREATE INDEX paper_orders_status_idx ON paper_orders (status);
