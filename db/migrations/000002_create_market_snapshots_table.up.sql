-- market_snapshots: Feature Engineが算出した1分足スナップショット
-- （docs/architecture/er.md §market_snapshots）
CREATE TABLE market_snapshots (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    instrument_id INTEGER NOT NULL REFERENCES instruments (id),
    symbol VARCHAR(10) NOT NULL,
    timestamp TEXT NOT NULL,
    price NUMERIC(12, 2) NOT NULL,
    bid NUMERIC(12, 2),
    ask NUMERIC(12, 2),
    spread_bps NUMERIC(8, 2),
    volume INTEGER NOT NULL,
    turnover NUMERIC(18, 2) NOT NULL,
    return_1m NUMERIC(8, 4),
    return_5m NUMERIC(8, 4),
    return_15m NUMERIC(8, 4),
    vwap NUMERIC(12, 2) NOT NULL,
    price_vs_vwap_bps NUMERIC(8, 2) NOT NULL,
    volume_ratio_5m NUMERIC(8, 4),
    orderbook_imbalance NUMERIC(6, 4),
    realized_vol_5m NUMERIC(8, 4),
    market_return_5m NUMERIC(8, 4),
    sector_return_5m NUMERIC(8, 4),
    raw_data_json TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE UNIQUE INDEX market_snapshots_instrument_timestamp_idx
    ON market_snapshots (instrument_id, timestamp);
CREATE INDEX market_snapshots_symbol_timestamp_idx
    ON market_snapshots (symbol, timestamp DESC);
