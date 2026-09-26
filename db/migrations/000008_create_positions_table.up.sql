-- positions: 保有ポジション（Entry/Exit紐付き）
-- （docs/architecture/er.md §positions）
CREATE TABLE positions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    instrument_id INTEGER NOT NULL REFERENCES instruments (id),
    entry_order_id INTEGER NOT NULL REFERENCES paper_orders (id),
    exit_order_id INTEGER REFERENCES paper_orders (id),
    symbol VARCHAR(10) NOT NULL,
    side VARCHAR(10) NOT NULL
        CHECK (side IN ('LONG', 'SHORT')),
    quantity INTEGER NOT NULL,
    entry_price NUMERIC(12, 2) NOT NULL,
    current_price NUMERIC(12, 2) NOT NULL,
    unrealized_pnl NUMERIC(14, 2) NOT NULL DEFAULT 0,
    realized_pnl NUMERIC(14, 2),
    opened_at TEXT NOT NULL,
    closed_at TEXT,
    exit_reason VARCHAR(50),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX positions_instrument_idx ON positions (instrument_id);
-- 同一銘柄の同時保有は1ポジションに制限する部分UNIQUEインデックス
-- （SQLite 3.8+対応、状態管理§4.9の`position`フィールドと整合）
CREATE UNIQUE INDEX positions_open_instrument_uq
    ON positions (instrument_id) WHERE closed_at IS NULL;
