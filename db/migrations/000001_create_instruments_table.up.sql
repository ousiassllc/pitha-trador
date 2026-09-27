-- instruments: 対象銘柄マスタ（docs/architecture/er.md §instruments）
CREATE TABLE instruments (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    symbol VARCHAR(10) NOT NULL,
    name VARCHAR(255) NOT NULL,
    market VARCHAR(50) NOT NULL,
    sector VARCHAR(100),
    is_active BOOLEAN NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE UNIQUE INDEX instruments_symbol_idx ON instruments (symbol);
CREATE INDEX instruments_is_active_idx ON instruments (is_active);
