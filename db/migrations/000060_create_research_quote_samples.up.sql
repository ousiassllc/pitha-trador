-- 立花の日中の時価を kabu と並べて比べる研究専用の記録（issue #1083）。取引・Feature Engine・Scout・Paper は読まない。
-- timestamp は記録した瞬間（UTC、market_snapshots と同じ書式）で、退避（snapshotarchive）が id と合わせて使う。
CREATE TABLE research_quote_samples (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp TEXT NOT NULL,
    source TEXT NOT NULL CHECK (source IN ('tachibana', 'kabu')),
    symbol VARCHAR(10) NOT NULL,
    price NUMERIC NOT NULL CHECK (price > 0),
    bid NUMERIC,
    ask NUMERIC,
    high NUMERIC,
    low NUMERIC,
    vwap NUMERIC,
    volume NUMERIC,
    turnover NUMERIC,
    special_quote INTEGER NOT NULL DEFAULT 0 CHECK (special_quote IN (0, 1)),
    observed_at TEXT,
    via TEXT NOT NULL CHECK (via IN ('event', 'rest')),
    session_phase TEXT NOT NULL CHECK (session_phase IN ('open_auction', 'morning', 'lunch', 'afternoon', 'close')),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE UNIQUE INDEX research_quote_samples_key_idx ON research_quote_samples (source, symbol, timestamp);
CREATE INDEX research_quote_samples_timestamp_idx ON research_quote_samples (timestamp);
